// Package server — filesystem route handlers.
package server

import (
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"net/http"
	"strconv"

	"github.com/ZaidQamhieh/rfe/agent/internal/fsops"
)

// --------- /system/drives ---------

func drivesHandler(base *fsops.Ops) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ops := opsFromContext(r.Context(), base)
		drives, err := ops.Drives()
		if err != nil {
			writeInternal(w, "drives", err)
			return
		}
		writeJSON(w, http.StatusOK, drives)
	}
}

// --------- /fs GET (list dir) ---------

func listDirHandler(ops *fsops.Ops) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ops := opsFromContext(r.Context(), ops)
		path := r.URL.Query().Get("path")
		if path == "" {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "path query param required")
			return
		}
		cursor := r.URL.Query().Get("cursor")
		limit := 200
		if l := r.URL.Query().Get("limit"); l != "" {
			if n, err := strconv.Atoi(l); err == nil && n > 0 {
				limit = n
			}
		}
		listing, err := ops.ListDir(path, cursor, limit)
		if err != nil {
			handleFsError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, listing)
	}
}

// --------- /fs DELETE (batch delete) ---------

func deleteHandler(ops *fsops.Ops, trashDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ops := opsFromContext(r.Context(), ops)
		var paths []string

		// Accept either ?path= query param or JSON body with paths array.
		if p := r.URL.Query().Get("path"); p != "" {
			paths = append(paths, p)
		}

		// Parse optional body.
		// A chunked body has ContentLength -1, so test for a body rather than a length.
		if r.Body != nil && r.Body != http.NoBody {
			var body struct {
				Paths []string `json:"paths"`
			}
			err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxJSONBody)).Decode(&body)
			switch {
			case err == nil:
				paths = append(paths, body.Paths...)
			case !errors.Is(err, io.EOF):
				writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid JSON body")
				return
			}
		}

		if len(paths) == 0 {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "no paths specified")
			return
		}
		// Default is reversible (move to trash); ?permanent=true hard-deletes.
		var results []fsops.BatchResult
		if r.URL.Query().Get("permanent") == "true" {
			results = ops.Delete(paths)
		} else {
			results = ops.MoveToTrash(paths, trashDir)
		}
		writeJSON(w, http.StatusOK, map[string]any{"results": results})
	}
}

// --------- /trash ---------

func listTrashHandler(base *fsops.Ops, trashDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ops := opsFromContext(r.Context(), base)
		items, err := ops.ListTrash(trashDir)
		if err != nil {
			writeInternal(w, "list trash", err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": items})
	}
}

func restoreTrashHandler(ops *fsops.Ops, trashDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ops := opsFromContext(r.Context(), ops)
		var body struct {
			IDs []string `json:"ids"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxJSONBody)).Decode(&body); err != nil || len(body.IDs) == 0 {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "ids required")
			return
		}
		results := ops.RestoreFromTrash(body.IDs, trashDir)
		writeJSON(w, http.StatusOK, map[string]any{"results": results})
	}
}

func emptyTrashHandler(ops *fsops.Ops, trashDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		reqOps := opsFromContext(r.Context(), ops)
		if reqOps.IsReadOnly() {
			writeError(w, http.StatusForbidden, "READ_ONLY", fsops.ErrReadOnly.Error())
			return
		}
		// Optional body {ids:[...]} deletes specific items; empty body empties all.
		var body struct {
			IDs []string `json:"ids"`
		}
		// Only a body with no content at all means "empty all"; a body that
		// fails to decode (including an unsized one) is rejected so a bad
		// request can never wipe the whole trash.
		err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxJSONBody)).Decode(&body)
		if err != nil && !errors.Is(err, io.EOF) {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid body")
			return
		}
		// A body that is present must name ids ({}, null and {"ids":[]} would otherwise wipe everything,
		// for example when a client has nothing selected).
		if err == nil && len(body.IDs) == 0 {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "ids must not be empty; send no body to empty all")
			return
		}
		if err := reqOps.EmptyTrash(trashDir, body.IDs); err != nil {
			if errors.Is(err, fsops.ErrBadTrashID) {
				writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid trash id")
				return
			}
			writeError(w, http.StatusInternalServerError, "INTERNAL", "empty trash failed")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

// --------- /fs/folder POST ---------

func createFolderHandler(ops *fsops.Ops) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ops := opsFromContext(r.Context(), ops)
		var req struct {
			Path string `json:"path"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxJSONBody)).Decode(&req); err != nil || req.Path == "" {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "path required")
			return
		}
		entry, err := ops.CreateFolder(req.Path)
		if err != nil {
			handleFsError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, entry)
	}
}

// --------- /fs/file POST ---------

func createFileHandler(ops *fsops.Ops) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ops := opsFromContext(r.Context(), ops)
		var req struct {
			Path string `json:"path"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxJSONBody)).Decode(&req); err != nil || req.Path == "" {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "path required")
			return
		}
		entry, err := ops.CreateFile(req.Path)
		if err != nil {
			handleFsError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, entry)
	}
}

// --------- /fs/rename PATCH ---------

func renameHandler(ops *fsops.Ops) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ops := opsFromContext(r.Context(), ops)
		var req struct {
			Src string `json:"src"`
			Dst string `json:"dst"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxJSONBody)).Decode(&req); err != nil || req.Src == "" || req.Dst == "" {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "src and dst required")
			return
		}
		entry, err := ops.Rename(req.Src, req.Dst)
		if err != nil {
			handleFsError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, entry)
	}
}

// --------- /fs/copy POST ---------

func copyHandler(ops *fsops.Ops) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ops := opsFromContext(r.Context(), ops)
		var req struct {
			Sources   []string `json:"sources"`
			DestDir   string   `json:"destDir"`
			Dup       bool     `json:"duplicate"`
			Overwrite bool     `json:"overwrite"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxJSONBody)).Decode(&req); err != nil || len(req.Sources) == 0 || req.DestDir == "" {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "sources and destDir required")
			return
		}
		// Overwrite replaces (deletes) whatever is at the destination, so it
		// needs the delete permission too. Duplicate wins over overwrite.
		if req.Overwrite && !req.Dup && !requireDeviceFileCapabilities(w, r, capDelete) {
			return
		}
		results := ops.Copy(req.Sources, req.DestDir, req.Dup, req.Overwrite)
		writeJSON(w, http.StatusAccepted, map[string]any{"results": results})
	}
}

// --------- /fs/move POST ---------

func moveHandler(ops *fsops.Ops) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ops := opsFromContext(r.Context(), ops)
		var req struct {
			Sources   []string `json:"sources"`
			DestDir   string   `json:"destDir"`
			Dup       bool     `json:"duplicate"`
			Overwrite bool     `json:"overwrite"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxJSONBody)).Decode(&req); err != nil || len(req.Sources) == 0 || req.DestDir == "" {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "sources and destDir required")
			return
		}
		results := ops.Move(req.Sources, req.DestDir, req.Dup, req.Overwrite)
		writeJSON(w, http.StatusAccepted, map[string]any{"results": results})
	}
}

// --------- /fs/compress POST ---------

func compressHandler(ops *fsops.Ops) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ops := opsFromContext(r.Context(), ops)
		var req struct {
			Sources []string `json:"sources"`
			Dest    string   `json:"dest"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxJSONBody)).Decode(&req); err != nil || len(req.Sources) == 0 || req.Dest == "" {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "sources and dest required")
			return
		}
		entry, err := ops.Compress(req.Sources, req.Dest)
		if err != nil {
			handleFsError(w, err)
			return
		}
		writeJSON(w, http.StatusCreated, entry)
	}
}

// --------- /fs/extract POST ---------

func extractHandler(ops *fsops.Ops) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ops := opsFromContext(r.Context(), ops)
		var req struct {
			Archive string `json:"archive"`
			DestDir string `json:"destDir"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxJSONBody)).Decode(&req); err != nil || req.Archive == "" || req.DestDir == "" {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "archive and destDir required")
			return
		}
		entry, err := ops.Extract(req.Archive, req.DestDir)
		if err != nil {
			handleFsError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, entry)
	}
}

// --------- /fs/meta GET ---------

func metaHandler(ops *fsops.Ops) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ops := opsFromContext(r.Context(), ops)
		path := r.URL.Query().Get("path")
		if path == "" {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "path required")
			return
		}
		entry, err := ops.Meta(path)
		if err != nil {
			handleFsError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, entry)
	}
}

// --------- /fs/checksum GET ---------

func checksumHandler(ops *fsops.Ops) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ops := opsFromContext(r.Context(), ops)
		path := r.URL.Query().Get("path")
		if path == "" {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "path required")
			return
		}
		algo := r.URL.Query().Get("algo")
		if algo == "" {
			algo = "sha256"
		}
		switch algo {
		case "sha256", "sha1", "md5":
		default:
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "unsupported algorithm (use sha256, sha1, or md5)")
			return
		}
		sum, err := ops.Checksum(path, algo)
		if err != nil {
			handleFsError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{
			"path":      path,
			"algorithm": algo,
			"checksum":  sum,
		})
	}
}

// --------- error mapping ---------

func handleFsError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, fsops.ErrForbidden):
		writeError(w, http.StatusForbidden, "FORBIDDEN", err.Error())
	case errors.Is(err, fsops.ErrNotFound):
		writeError(w, http.StatusNotFound, "PATH_NOT_FOUND", err.Error())
	case errors.Is(err, fsops.ErrReadOnly):
		writeError(w, http.StatusForbidden, "READ_ONLY", err.Error())
	case errors.Is(err, fsops.ErrNotRegular):
		writeError(w, http.StatusUnprocessableEntity, "NOT_A_FILE", err.Error())
	case errors.Is(err, fsops.ErrNotDir), errors.Is(err, fsops.ErrIsDir):
		writeError(w, http.StatusBadRequest, "BAD_REQUEST", err.Error())
	case errors.Is(err, fsops.ErrUnsupported):
		writeError(w, http.StatusBadRequest, "UNSUPPORTED_FORMAT", err.Error())
	case errors.Is(err, fsops.ErrNoAtomicRename):
		// The file system has no atomic no-replace rename and no safe
		// substitute: fsops refused and changed nothing. Only this sentinel:
		// a raw ENOTSUP/ENOSYS also matches errors.ErrUnsupported but comes
		// from some other operation. Fixed text, never err.Error(): it carries
		// OS detail (PR-53).
		writeError(w, http.StatusNotImplemented, "NOT_IMPLEMENTED", fsops.NoAtomicRenameMessage)
	case errors.Is(err, fsops.ErrConflict):
		writeError(w, http.StatusConflict, "CONFLICT", err.Error())
	case errors.Is(err, fs.ErrExist):
		// A raw "exists" from an exclusive create that lost a race (the
		// early check saw nothing). Fixed text: the OS error carries paths.
		writeError(w, http.StatusConflict, "CONFLICT", "destination already exists")
	case errors.Is(err, fsops.ErrStale):
		writeError(w, http.StatusConflict, "STALE_WRITE", err.Error())
	default:
		// Unmapped errors are raw OS errors carrying absolute paths — don't
		// echo them to the client (PR-53).
		writeInternal(w, "fs operation", err)
	}
}
