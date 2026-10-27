// Package server — transfer (upload) + content (download) handlers.
package server

import (
	"compress/gzip"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/ZaidQamhieh/rfe/agent/internal/fsops"
	"github.com/ZaidQamhieh/rfe/agent/internal/settings"
	"github.com/ZaidQamhieh/rfe/agent/internal/store"
	"github.com/ZaidQamhieh/rfe/agent/internal/transfer"
)

// compressibleExtensions are the (lowercase, dot-free) file extensions
// eligible for opt-in gzip-on-download (S3). Deliberately a small text/code
// allowlist — anything already compressed (zip/jpg/mp4/...) or not in this
// list is served as-is. Not the search package's categoryExtensions: that
// table is image/video/audio/document/archive *display* categories (and
// includes already-compressed formats like pdf/docx), not a
// compresses-well-with-gzip allowlist.
var compressibleExtensions = map[string]bool{
	"txt": true, "log": true, "md": true, "json": true, "yaml": true,
	"yml": true, "csv": true, "xml": true, "html": true, "css": true,
	"js": true, "go": true, "dart": true, "py": true, "java": true,
	"c": true, "cpp": true, "h": true, "sql": true,
}

// compressMinBytes is the floor below which gzip overhead isn't worth it.
const compressMinBytes = 1024

// --------- /content GET ---------

func downloadHandler(ops *fsops.Ops, st ...*settings.Store) http.HandlerFunc {
	var ss *settings.Store
	if len(st) > 0 {
		ss = st[0]
	}
	return func(w http.ResponseWriter, r *http.Request) {
		ops := opsFromContext(r.Context(), ops)
		path := r.URL.Query().Get("path")
		if path == "" {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "path required")
			return
		}
		f, err := ops.Open(path)
		if err != nil {
			if errors.Is(err, fsops.ErrForbidden) || errors.Is(err, fsops.ErrNotRegular) {
				handleFsError(w, err)
			} else if os.IsNotExist(err) {
				writeError(w, http.StatusNotFound, "PATH_NOT_FOUND", "file not found")
			} else {
				writeInternal(w, "download", err)
			}
			return
		}
		defer f.Close()

		info, err := f.Stat()
		if err != nil {
			writeInternal(w, "download", err)
			return
		}
		if info.IsDir() {
			writeError(w, http.StatusUnprocessableEntity, "NOT_A_FILE", "path is a directory")
			return
		}
		setUntrustedFileResponseHeaders(w, info.Name())
		w = countingWriter{w}

		// Apply download bandwidth throttle if configured.
		var content io.ReadSeeker = f
		if ss != nil {
			if limit := ss.MaxDownloadBytesPerSec(); limit > 0 {
				trs := transfer.NewThrottledReadSeeker(f, limit)
				trs.WithContext(r.Context())
				content = trs
			}
		}

		// Gzip-on-download (S3): only for a fresh, non-ranged request whose
		// client opted in via Accept-Encoding, on a compressible extension
		// above the size floor. Gzip and Range don't compose — a gzip'd
		// stream's offsets don't correspond to the original file's byte
		// offsets — so any Range header unconditionally falls through to the
		// plain http.ServeContent path below, which is what makes resumable
		// download (agent_client.dart's downloadFile) safe.
		if r.Header.Get("Range") == "" &&
			acceptsGzip(r) &&
			compressibleExtensions[extOfName(info.Name())] &&
			info.Size() >= compressMinBytes {
			w.Header().Set("Content-Type", contentTypeForName(info.Name()))
			w.Header().Set("Content-Encoding", "gzip")
			w.Header().Set("Vary", "Accept-Encoding")
			gz := gzip.NewWriter(w)
			defer gz.Close()
			_, _ = io.Copy(gz, content) // best-effort stream; client sees a truncated body on error
			return
		}

		// http.ServeContent handles Range, 206, 416, Content-Type, ETag, etc.
		http.ServeContent(w, r, info.Name(), info.ModTime(), content)
	}
}

// acceptsGzip reports whether the request's Accept-Encoding header lists gzip.
func acceptsGzip(r *http.Request) bool {
	for _, enc := range strings.Split(r.Header.Get("Accept-Encoding"), ",") {
		if strings.EqualFold(strings.TrimSpace(enc), "gzip") {
			return true
		}
	}
	return false
}

// contentTypeForName returns the same MIME type http.ServeContent would have
// detected from the file extension, falling back to application/octet-stream.
func contentTypeForName(name string) string {
	if m := mime.TypeByExtension(filepath.Ext(name)); m != "" {
		return m
	}
	return "application/octet-stream"
}

// setUntrustedFileResponseHeaders prevents content fetched from arbitrary
// host files from becoming executable same-origin web content. Native clients
// still receive the original bytes and media type; Content-Disposition only
// changes browser handling.
func setUntrustedFileResponseHeaders(w http.ResponseWriter, name string) {
	filename := mime.FormatMediaType("attachment", map[string]string{"filename": filepath.Base(name)})
	if filename == "" {
		filename = "attachment"
	}
	w.Header().Set("Content-Disposition", filename)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
	w.Header().Set("Cache-Control", "no-store")
}

// MaxContentBytes caps the size of a PUT /v1/content request body.
const MaxContentBytes int64 = 5 << 20 // 5 MiB

// --------- /content PUT ---------

func writeContentHandler(ops *fsops.Ops) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ops := opsFromContext(r.Context(), ops)
		path := r.URL.Query().Get("path")
		if path == "" {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "path required")
			return
		}

		var baseModified *time.Time
		if bm := r.URL.Query().Get("baseModified"); bm != "" {
			t, err := time.Parse(time.RFC3339Nano, bm)
			if err != nil {
				writeError(w, http.StatusBadRequest, "BAD_REQUEST", "baseModified must be RFC3339")
				return
			}
			baseModified = &t
		}

		r.Body = http.MaxBytesReader(w, r.Body, MaxContentBytes)
		data, err := io.ReadAll(r.Body)
		if err != nil {
			var maxErr *http.MaxBytesError
			if errors.As(err, &maxErr) {
				writeError(w, http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE", "content exceeds maximum size of 5MiB")
				return
			}
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "failed to read request body")
			return
		}

		entry, err := ops.WriteContent(path, data, baseModified)
		if err != nil {
			handleFsError(w, err)
			return
		}
		rxBytesTotal.Add(int64(len(data)))
		writeJSON(w, http.StatusOK, entry)
	}
}

// maxChunkSize caps the client-chosen chunkSize for an upload session.
// Chunks are buffered fully in memory (see uploadChunkHandler), so an
// unbounded chunkSize would let a client force large allocations.
const maxChunkSize = 32 * 1024 * 1024 // 32 MiB

// --------- POST /transfers ---------

func openTransferHandler(tm *transfer.Manager, ops *fsops.Ops) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ops := opsFromContext(r.Context(), ops)
		var req struct {
			Path      string `json:"path"`
			Size      int64  `json:"size"`
			SHA256    string `json:"sha256"`
			ChunkSize int    `json:"chunkSize"`
			Overwrite bool   `json:"overwrite"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxJSONBody)).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid request body")
			return
		}
		if req.Path == "" || req.SHA256 == "" || req.ChunkSize <= 0 {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "path, sha256, and chunkSize required")
			return
		}
		if req.Size < 0 {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "size must not be negative")
			return
		}
		if req.ChunkSize > maxChunkSize {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "chunkSize exceeds maximum of 32MiB")
			return
		}
		if req.Overwrite && !requireDeviceFileCapabilities(w, r, capModify) {
			return
		}
		// Validate path is in jail.
		resolved, err := ops.Resolve(req.Path)
		if err != nil {
			handleFsError(w, err)
			return
		}

		var deviceID string
		if d := deviceFromContext(r); d != nil {
			deviceID = d.ID
		}

		id := uuid.New().String()
		t, err := tm.OpenSessionWithStat(id, resolved, req.Size, req.ChunkSize, req.SHA256, req.Overwrite, deviceID, ops.Stat)
		if err != nil {
			if errors.Is(err, fsops.ErrForbidden) || errors.Is(err, fsops.ErrReadOnly) {
				handleFsError(w, err)
				return
			}
			if errors.Is(err, transfer.ErrDestinationExists) {
				writeError(w, http.StatusConflict, "CONFLICT", "destination already exists")
				return
			}
			if errors.Is(err, transfer.ErrTooLarge) {
				writeError(w, http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE", err.Error())
				return
			}
			if errors.Is(err, transfer.ErrQuotaExceeded) {
				writeError(w, http.StatusTooManyRequests, "RESOURCE_LIMIT", "upload capacity is full; finish or remove an existing session before opening another")
				return
			}
			writeInternal(w, "open transfer", err)
			return
		}
		writeJSON(w, http.StatusCreated, transferSession(t))
	}
}

// callerOwnsTransfer reports whether the authenticated device opened this
// transfer, or is an admin device. Non-owners are treated as if the transfer
// does not exist so foreign session IDs are neither confirmed nor usable
// (PR-11). Legacy sessions with no recorded owner are admin-only.
func callerOwnsTransfer(r *http.Request, t *store.Transfer) bool {
	d := deviceFromContext(r)
	if d == nil {
		return false
	}
	if t.DeviceID != "" && t.DeviceID == d.ID {
		return true
	}
	return isAdminDevice(d)
}

// --------- GET /transfers/{id} ---------

func transferStatusHandler(tm *transfer.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		t, err := tm.Lookup(id)
		if err != nil {
			if errors.Is(err, transfer.ErrNotFound) {
				writeError(w, http.StatusNotFound, "NOT_FOUND", "transfer not found")
			} else {
				writeInternal(w, "transfer status", err)
			}
			return
		}
		if !callerOwnsTransfer(r, t) {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "transfer not found")
			return
		}
		// Only an authorized owner refreshes the retention clock.
		t, err = tm.Status(id)
		if err != nil {
			if errors.Is(err, transfer.ErrNotFound) {
				writeError(w, http.StatusNotFound, "NOT_FOUND", "transfer not found")
			} else if errors.Is(err, transfer.ErrSessionActive) {
				writeError(w, http.StatusConflict, "TRANSFER_ACTIVE", "transfer completion is in progress; retry after it finishes")
			} else {
				writeInternal(w, "transfer status", err)
			}
			return
		}
		writeJSON(w, http.StatusOK, transferSession(t))
	}
}

// --------- PUT /transfers/{id}/chunks/{n} ---------

func uploadChunkHandler(tm *transfer.Manager, st ...*settings.Store) http.HandlerFunc {
	var ss *settings.Store
	if len(st) > 0 {
		ss = st[0]
	}
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		nStr := chi.URLParam(r, "n")
		n, err := strconv.Atoi(nStr)
		if err != nil {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid chunk index")
			return
		}
		chunkSHA256 := r.Header.Get("X-Chunk-Sha256")
		if chunkSHA256 == "" {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "X-Chunk-Sha256 header required")
			return
		}

		t, err := tm.Lookup(id)
		if err != nil {
			if errors.Is(err, transfer.ErrNotFound) {
				writeError(w, http.StatusNotFound, "NOT_FOUND", "transfer not found")
				return
			}
			writeInternal(w, "upload chunk", err)
			return
		}
		if !callerOwnsTransfer(r, t) {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "transfer not found")
			return
		}
		if t.Overwrite && !requireDeviceFileCapabilities(w, r, capModify) {
			return
		}
		// Hold an activity lease before consuming or throttling the request body.
		// This closes the gap where a slow upload had not yet reached WriteChunk
		// and could otherwise be mistaken for an abandoned session.
		release, err := tm.BeginActivity(id)
		if err != nil {
			if errors.Is(err, transfer.ErrNotFound) {
				writeError(w, http.StatusNotFound, "NOT_FOUND", "transfer not found")
				return
			}
			if errors.Is(err, transfer.ErrSessionActive) {
				writeError(w, http.StatusConflict, "TRANSFER_ACTIVE", "transfer completion is in progress; retry after it finishes")
				return
			}
			if errors.Is(err, transfer.ErrNotOpen) {
				writeError(w, http.StatusConflict, "TRANSFER_NOT_OPEN", "transfer is no longer open")
				return
			}
			writeInternal(w, "begin upload activity", err)
			return
		}
		defer release()

		// Cap the request body to the session's chunk size. The final chunk
		// may be smaller, which is fine since this is just an upper bound.
		r.Body = http.MaxBytesReader(w, r.Body, int64(t.ChunkSize))

		// Apply upload bandwidth throttle if configured.
		var bodyReader io.Reader = r.Body
		if ss != nil {
			if limit := ss.MaxUploadBytesPerSec(); limit > 0 {
				bodyReader = transfer.NewThrottledReader(bodyReader, limit).WithContext(r.Context())
			}
		}

		// Reserve the memory this chunk may need before buffering it.
		need := int64(t.ChunkSize)
		if !chunkBudget.acquire(need, chunkWait, r.Context().Done()) {
			w.Header().Set("Retry-After", "2")
			writeError(w, http.StatusTooManyRequests, "CHUNK_BUSY", "too many chunks are being received; retry shortly")
			return
		}
		defer chunkBudget.release(need)

		// Read straight into one buffer of exactly the reserved size, so the peak stays inside the budget (a
		// growing buffer would double). One extra byte probes for a body longer than the chunk size.
		data := make([]byte, need)
		got, err := io.ReadFull(bodyReader, data)
		switch {
		case err == nil:
			var one [1]byte
			m, perr := bodyReader.Read(one[:])
			var tooBig *http.MaxBytesError
			if m > 0 || errors.As(perr, &tooBig) {
				writeError(w, http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE", "chunk exceeds session chunk size")
				return
			}
		case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
			data, err = data[:got], nil
		}
		if err != nil {
			var maxErr *http.MaxBytesError
			if errors.As(err, &maxErr) {
				writeError(w, http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE", "chunk exceeds session chunk size")
				return
			}
			writeError(w, http.StatusInternalServerError, "INTERNAL", "failed to read body")
			return
		}

		if err := tm.WriteChunk(id, n, data, chunkSHA256); err != nil {
			if errors.Is(err, transfer.ErrNotFound) {
				writeError(w, http.StatusNotFound, "NOT_FOUND", "transfer not found")
				return
			}
			if errors.Is(err, transfer.ErrChunkMismatch) {
				writeError(w, http.StatusConflict, "CHUNK_HASH_MISMATCH", err.Error())
				return
			}
			// Client-side geometry errors, not server faults (PR-12).
			if errors.Is(err, transfer.ErrChunkOutOfRange) || errors.Is(err, transfer.ErrChunkWrongSize) {
				writeError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
				return
			}
			if errors.Is(err, transfer.ErrNotOpen) {
				writeError(w, http.StatusConflict, "TRANSFER_NOT_OPEN", "transfer is no longer open")
				return
			}
			if errors.Is(err, transfer.ErrSessionActive) {
				writeError(w, http.StatusConflict, "TRANSFER_ACTIVE", "transfer completion is in progress; retry after it finishes")
				return
			}
			writeInternal(w, "upload chunk", err)
			return
		}
		rxBytesTotal.Add(int64(len(data)))
		w.WriteHeader(http.StatusNoContent)
	}
}

// --------- POST /transfers/{id}/complete ---------

func completeTransferHandler(tm *transfer.Manager, ops *fsops.Ops) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ops := opsFromContext(r.Context(), ops)
		id := chi.URLParam(r, "id")

		// The publisher callback below re-authorizes the target through this
		// request's effective Ops and performs the final placement using the
		// same rooted filesystem boundary. A prior Resolve check is not enough
		// because the path can change between authorization and rename.
		var verifiedSHA256 string
		t, err := tm.Lookup(id)
		if err != nil {
			if errors.Is(err, transfer.ErrNotFound) {
				writeError(w, http.StatusNotFound, "NOT_FOUND", "transfer not found")
			} else {
				writeInternal(w, "complete transfer status", err)
			}
			return
		}
		if t == nil {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "transfer not found")
			return
		}
		if !callerOwnsTransfer(r, t) {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "transfer not found")
			return
		}
		if t.Overwrite && !requireDeviceFileCapabilities(w, r, capModify) {
			return
		}
		verifiedSHA256 = t.SHA256
		_, targetPath, err := tm.CompleteWithPublisher(id, func(path string, source io.Reader, overwrite bool) (os.FileInfo, error) {
			info, publishErr := ops.PublishReader(path, source, overwrite)
			if errors.Is(publishErr, fsops.ErrConflict) {
				return nil, transfer.ErrDestinationExists
			}
			return info, publishErr
		})
		if err != nil {
			if errors.Is(err, fsops.ErrForbidden) || errors.Is(err, fsops.ErrReadOnly) {
				handleFsError(w, err)
				return
			}
			if errors.Is(err, transfer.ErrNotFound) {
				writeError(w, http.StatusNotFound, "NOT_FOUND", "transfer not found")
				return
			}
			if errors.Is(err, transfer.ErrIncomplete) {
				writeError(w, http.StatusConflict, "UPLOAD_INCOMPLETE", err.Error())
				return
			}
			if errors.Is(err, transfer.ErrFileMismatch) {
				writeError(w, http.StatusUnprocessableEntity, "HASH_MISMATCH", err.Error())
				return
			}
			// Something occupied the target while the upload ran, and this
			// session was opened with overwrite=false (PR-50).
			if errors.Is(err, transfer.ErrDestinationExists) {
				writeError(w, http.StatusConflict, "CONFLICT", "destination already exists")
				return
			}
			if errors.Is(err, transfer.ErrNotOpen) {
				writeError(w, http.StatusConflict, "TRANSFER_NOT_OPEN", "transfer is no longer open")
				return
			}
			if errors.Is(err, transfer.ErrSessionActive) {
				writeError(w, http.StatusConflict, "TRANSFER_ACTIVE", "transfer has another active operation; retry after it finishes")
				return
			}
			writeInternal(w, "complete transfer", err)
			return
		}
		// Complete() only returns successfully once the whole-file SHA-256 has
		// been verified against t.SHA256 (captured above), so it's safe to
		// report that hash back to the client as the verified checksum.
		entry, err := ops.Meta(targetPath)
		if err != nil {
			// Transfer succeeded but meta failed — return a minimal entry.
			writeJSON(w, http.StatusOK, map[string]any{
				"path":     targetPath,
				"modified": time.Now(),
				"sha256":   verifiedSHA256,
				"verified": true,
			})
			return
		}
		// Marshal the Entry and merge in the verification fields, so the
		// response keeps all Entry fields (including omitempty behavior)
		// plus sha256/verified.
		resp := map[string]any{}
		if b, marshalErr := json.Marshal(entry); marshalErr == nil {
			_ = json.Unmarshal(b, &resp)
		}
		resp["sha256"] = verifiedSHA256
		resp["verified"] = true
		writeJSON(w, http.StatusOK, resp)
	}
}

// transferSession converts a store.Transfer to the UploadSession JSON shape.
func transferSession(t *store.Transfer) map[string]any {
	chunks := t.ReceivedChunks
	if chunks == nil {
		chunks = []int{}
	}
	return map[string]any{
		"id":             t.ID,
		"path":           t.TargetPath,
		"size":           t.TotalSize,
		"chunkSize":      t.ChunkSize,
		"totalChunks":    t.TotalChunks,
		"receivedChunks": chunks,
		"status":         t.Status,
	}
}
