// Package server — chmod handler.
package server

import (
	"errors"
	"io/fs"
	"net/http"
	"os"
	"strconv"

	"github.com/ZaidQamhieh/rfe/agent/internal/fsops"
)

func chmodHandler(ops *fsops.Ops) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ops := opsFromContext(r.Context(), ops)
		if ops.IsReadOnly() {
			writeError(w, http.StatusForbidden, "READ_ONLY", fsops.ErrReadOnly.Error())
			return
		}
		var req struct {
			Path string `json:"path"`
			Mode string `json:"mode"`
		}
		if !decodeJSONBody(w, r, &req) {
			return
		}
		if req.Path == "" || req.Mode == "" {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "path and mode required")
			return
		}

		parsed, err := strconv.ParseUint(req.Mode, 8, 32)
		if err != nil {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid octal mode: "+req.Mode)
			return
		}

		// Only the nine permission bits are accepted. Larger values would reach
		// Go's own FileMode type bits (setuid is 1<<23, not the digits' 04000),
		// and setuid/setgid/sticky are not something a remote client may set.
		if parsed > 0o777 {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "mode must be at most 777 (octal); setuid, setgid and sticky are not supported")
			return
		}
		mode := fs.FileMode(parsed)

		if err := ops.Chmod(req.Path, mode); err != nil {
			if errors.Is(err, fsops.ErrForbidden) {
				handleFsError(w, err)
				return
			}
			if os.IsNotExist(err) {
				handleFsError(w, fsops.ErrNotFound)
				return
			}
			if os.IsPermission(err) {
				writeError(w, http.StatusForbidden, "FORBIDDEN", "permission denied")
				return
			}
			writeInternal(w, "chmod", err)
			return
		}

		entry, err := ops.Meta(req.Path)
		if err != nil {
			handleFsError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, entry)
	}
}
