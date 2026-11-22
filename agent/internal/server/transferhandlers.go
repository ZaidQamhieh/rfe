// Package server — transfer (upload) + content (download) handlers.
package server

import (
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/ZaidQamhieh/rfe/agent/internal/fsops"
	"github.com/ZaidQamhieh/rfe/agent/internal/settings"
	"github.com/ZaidQamhieh/rfe/agent/internal/store"
	"github.com/ZaidQamhieh/rfe/agent/internal/transfer"
)

const (
	// transferNotFoundMsg is the 404 message for an unknown transfer id.
	transferNotFoundMsg = "transfer not found"
	// headerChunkSHA256 is the request header carrying a chunk's SHA-256.
	headerChunkSHA256 = "X-Chunk-Sha256"
	// codeTransferActive is the 409 error code for a transfer that is busy.
	codeTransferActive = "TRANSFER_ACTIVE"
	// codeBadRequest is the 400 error code for a malformed or incomplete request.
	codeBadRequest = "BAD_REQUEST"
	// codeInsufficientStorage is the 507 error code for an upload the host's disk cannot hold right now.
	codeInsufficientStorage = "INSUFFICIENT_STORAGE"
	// storageRetryAfter is the Retry-After (seconds) of a 507: freeing disk space takes the owner longer than a
	// busy-slot retry, so the client should back off further than for a 429.
	storageRetryAfter = "30"
	// pathRequiredMsg is the 400 message for a request that names no path.
	pathRequiredMsg = "path required"
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

// gzipPool reuses compressor state (about a megabyte each) across downloads.
var gzipPool = sync.Pool{New: func() any { return gzip.NewWriter(io.Discard) }}

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
			writeError(w, http.StatusBadRequest, codeBadRequest, pathRequiredMsg)
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
		// A strong validator for If-Range / If-Match, set before the gzip branch so both answers name the same version.
		w.Header().Set("ETag", downloadETag(info))
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
			gz, _ := gzipPool.Get().(*gzip.Writer)
			gz.Reset(w)
			if _, err := io.Copy(gz, content); err != nil {
				// No gzip trailer on a failed copy, so the client sees a corrupt stream, not a short "complete" file.
				gz.Reset(io.Discard)
				gzipPool.Put(gz)
				return
			}
			_ = gz.Close()
			gz.Reset(io.Discard) // do not keep the response writer alive in the pool
			gzipPool.Put(gz)
			return
		}

		// http.ServeContent handles Range, 206, 416, Content-Type, If-Range / If-Match against the ETag above, etc.
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
			writeError(w, http.StatusBadRequest, codeBadRequest, pathRequiredMsg)
			return
		}

		var baseModified *time.Time
		if bm := r.URL.Query().Get("baseModified"); bm != "" {
			t, err := time.Parse(time.RFC3339Nano, bm)
			if err != nil {
				writeError(w, http.StatusBadRequest, codeBadRequest, "baseModified must be RFC3339")
				return
			}
			baseModified = &t
		}

		r.Body = http.MaxBytesReader(w, r.Body, MaxContentBytes)
		data, err := io.ReadAll(r.Body)
		if err != nil {
			var maxErr *http.MaxBytesError
			if errors.As(err, &maxErr) {
				writeError(w, http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE", fmt.Sprintf("content exceeds maximum size of %dMiB", MaxContentBytes>>20))
				return
			}
			writeError(w, http.StatusBadRequest, codeBadRequest, "failed to read request body")
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

// Bounds and alphabet of the optional Idempotency-Key header of POST /transfers.
const (
	idempotencyKeyHeader = "Idempotency-Key"
	idempotencyKeyMin    = 16
	idempotencyKeyMax    = 64
)

// validIdempotencyKey reports whether key is 16 to 64 characters of [A-Za-z0-9_-].
func validIdempotencyKey(key string) bool {
	if len(key) < idempotencyKeyMin || len(key) > idempotencyKeyMax {
		return false
	}
	for i := 0; i < len(key); i++ {
		c := key[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

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
			writeError(w, http.StatusBadRequest, codeBadRequest, "invalid request body")
			return
		}
		if req.Path == "" || req.SHA256 == "" || req.ChunkSize <= 0 {
			writeError(w, http.StatusBadRequest, codeBadRequest, "path, sha256, and chunkSize required")
			return
		}
		if req.Size < 0 {
			writeError(w, http.StatusBadRequest, codeBadRequest, "size must not be negative")
			return
		}
		if req.ChunkSize > maxChunkSize {
			writeError(w, http.StatusBadRequest, codeBadRequest, fmt.Sprintf("chunkSize exceeds maximum of %dMiB", maxChunkSize>>20))
			return
		}
		if req.Overwrite && !requireDeviceFileCapabilities(w, r, capModify) {
			return
		}
		key := r.Header.Get(idempotencyKeyHeader)
		if key != "" && !validIdempotencyKey(key) {
			writeError(w, http.StatusBadRequest, codeBadRequest, fmt.Sprintf("%s must be %d-%d characters of A-Z, a-z, 0-9, _ or -", idempotencyKeyHeader, idempotencyKeyMin, idempotencyKeyMax))
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
		t, existed, err := tm.OpenSessionWithKey(id, resolved, req.Size, req.ChunkSize, req.SHA256, req.Overwrite, deviceID, key, ops.Stat)
		if err != nil {
			if errors.Is(err, transfer.ErrIdempotencyKeyUsed) {
				writeError(w, http.StatusConflict, "IDEMPOTENCY_KEY_USED", err.Error())
				return
			}
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
			if errors.Is(err, transfer.ErrInsufficientStorage) {
				writeStorageFull(w)
				return
			}
			writeInternal(w, "open transfer", err)
			return
		}
		if existed {
			// A repeat of an open whose answer the client lost: the same session, not a new one.
			writeJSON(w, http.StatusOK, transferSession(t))
			return
		}
		writeJSON(w, http.StatusCreated, transferSession(t))
	}
}

// writeStorageFull answers 507 INSUFFICIENT_STORAGE with Retry-After: the host's disk cannot hold the upload
// right now. The session stays open and keeps every chunk already received, so the client waits and retries.
func writeStorageFull(w http.ResponseWriter) {
	w.Header().Set("Retry-After", storageRetryAfter)
	writeError(w, http.StatusInsufficientStorage, codeInsufficientStorage, "the host has no free disk space for this upload; retry later")
}

// callerOwnsDevice reports whether the authenticated device is ownerID (the device
// that opened a transfer or minted a share token), or is an admin device. Non-owners
// are treated as if the record does not exist so foreign IDs are neither confirmed
// nor usable. Records with no recorded owner (ownerID "") are admin-only.
func callerOwnsDevice(r *http.Request, ownerID string) bool {
	d := deviceFromContext(r)
	if d == nil {
		return false
	}
	if ownerID != "" && ownerID == d.ID {
		return true
	}
	return isAdminDevice(d)
}

// writeTransferCompletionBusy is the 409 for a transfer whose completion is still running.
func writeTransferCompletionBusy(w http.ResponseWriter) {
	writeError(w, http.StatusConflict, codeTransferActive, "transfer completion is in progress; retry after it finishes")
}

// --------- GET /transfers/{id} ---------

func transferStatusHandler(tm *transfer.Manager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		t, err := tm.Lookup(id)
		if err != nil {
			if errors.Is(err, transfer.ErrNotFound) {
				writeError(w, http.StatusNotFound, "NOT_FOUND", transferNotFoundMsg)
			} else {
				writeInternal(w, "transfer status", err)
			}
			return
		}
		if !callerOwnsDevice(r, t.DeviceID) {
			writeError(w, http.StatusNotFound, "NOT_FOUND", transferNotFoundMsg)
			return
		}
		// Only an authorized owner refreshes the retention clock.
		t, err = tm.Status(id)
		if err != nil {
			if errors.Is(err, transfer.ErrNotFound) {
				writeError(w, http.StatusNotFound, "NOT_FOUND", transferNotFoundMsg)
			} else if errors.Is(err, transfer.ErrSessionActive) {
				writeTransferCompletionBusy(w)
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
			writeError(w, http.StatusBadRequest, codeBadRequest, "invalid chunk index")
			return
		}
		chunkSHA256 := r.Header.Get(headerChunkSHA256)
		if chunkSHA256 == "" {
			writeError(w, http.StatusBadRequest, codeBadRequest, headerChunkSHA256+" header required")
			return
		}

		t, err := tm.Lookup(id)
		if err != nil {
			if errors.Is(err, transfer.ErrNotFound) {
				writeError(w, http.StatusNotFound, "NOT_FOUND", transferNotFoundMsg)
				return
			}
			writeInternal(w, "upload chunk", err)
			return
		}
		if !callerOwnsDevice(r, t.DeviceID) {
			writeError(w, http.StatusNotFound, "NOT_FOUND", transferNotFoundMsg)
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
				writeError(w, http.StatusNotFound, "NOT_FOUND", transferNotFoundMsg)
				return
			}
			if errors.Is(err, transfer.ErrSessionActive) {
				writeTransferCompletionBusy(w)
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
				writeError(w, http.StatusNotFound, "NOT_FOUND", transferNotFoundMsg)
				return
			}
			if errors.Is(err, transfer.ErrChunkMismatch) {
				writeError(w, http.StatusConflict, "CHUNK_HASH_MISMATCH", err.Error())
				return
			}
			// Client-side geometry errors, not server faults.
			if errors.Is(err, transfer.ErrChunkOutOfRange) || errors.Is(err, transfer.ErrChunkWrongSize) {
				writeError(w, http.StatusBadRequest, codeBadRequest, err.Error())
				return
			}
			if errors.Is(err, transfer.ErrNotOpen) {
				writeError(w, http.StatusConflict, "TRANSFER_NOT_OPEN", "transfer is no longer open")
				return
			}
			if errors.Is(err, transfer.ErrSessionActive) {
				writeTransferCompletionBusy(w)
				return
			}
			if errors.Is(err, transfer.ErrInsufficientStorage) {
				writeStorageFull(w)
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
		t, err := tm.Lookup(id)
		if err != nil {
			if errors.Is(err, transfer.ErrNotFound) {
				writeError(w, http.StatusNotFound, "NOT_FOUND", transferNotFoundMsg)
			} else {
				writeInternal(w, "complete transfer status", err)
			}
			return
		}
		if t == nil {
			writeError(w, http.StatusNotFound, "NOT_FOUND", transferNotFoundMsg)
			return
		}
		if !callerOwnsDevice(r, t.DeviceID) {
			writeError(w, http.StatusNotFound, "NOT_FOUND", transferNotFoundMsg)
			return
		}
		if t.Overwrite && !requireDeviceFileCapabilities(w, r, capModify) {
			return
		}
		verifiedSHA256 := t.SHA256
		// A repeated complete (the first answer was lost) is idempotent: the session is already
		// published, so answer with the same success body and publish nothing. Only the owning
		// device (checked above) reaches this; failed sessions keep their TRANSFER_NOT_OPEN error.
		if t.Status == store.TransferCompleted {
			tm.NoteCompleteOnTerminalSession(id)
			writeCompleteSuccess(w, ops, t.TargetPath, verifiedSHA256)
			return
		}
		_, targetPath, err := tm.CompleteWithPublisher(id, func(path string, source io.Reader, overwrite bool) (os.FileInfo, error) {
			info, publishErr := ops.PublishReaderWith(path, source, overwrite, tm.PublishOptions(id))
			if errors.Is(publishErr, fsops.ErrConflict) {
				return nil, transfer.ErrDestinationExists
			}
			// An overwrite cannot replace a folder: the rename fails with an OS-specific
			// error, so report the occupied destination as a conflict, not a 500.
			if publishErr != nil && overwrite {
				if fi, statErr := ops.Stat(path); statErr == nil && fi.IsDir() {
					return nil, transfer.ErrDestinationExists
				}
			}
			return info, publishErr
		})
		if err != nil {
			if errors.Is(err, fsops.ErrForbidden) || errors.Is(err, fsops.ErrReadOnly) {
				handleFsError(w, err)
				return
			}
			if errors.Is(err, transfer.ErrNotFound) {
				writeError(w, http.StatusNotFound, "NOT_FOUND", transferNotFoundMsg)
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
			// session was opened with overwrite=false.
			if errors.Is(err, transfer.ErrDestinationExists) {
				writeError(w, http.StatusConflict, "CONFLICT", "destination already exists")
				return
			}
			if errors.Is(err, transfer.ErrNotOpen) {
				writeError(w, http.StatusConflict, "TRANSFER_NOT_OPEN", "transfer is no longer open")
				return
			}
			if errors.Is(err, transfer.ErrSessionActive) {
				writeError(w, http.StatusConflict, codeTransferActive, "transfer has another active operation; retry after it finishes")
				return
			}
			writeInternal(w, "complete transfer", err)
			return
		}
		// Complete() only returns successfully once the whole-file SHA-256 has
		// been verified against t.SHA256 (captured above), so it's safe to
		// report that hash back to the client as the verified checksum.
		writeCompleteSuccess(w, ops, targetPath, verifiedSHA256)
	}
}

// writeCompleteSuccess writes the 200 body of a finished upload: the published file's Entry fields
// plus its verified sha256. A first complete and a repeated one share it, so both answer alike.
func writeCompleteSuccess(w http.ResponseWriter, ops *fsops.Ops, targetPath, verifiedSHA256 string) {
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
