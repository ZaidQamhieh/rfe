// Package server — thumbnail handler.
package server

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strconv"

	"github.com/ZaidQamhieh/rfe/agent/internal/fsops"
	"github.com/ZaidQamhieh/rfe/agent/internal/thumbs"
)

// defaultThumbSize matches the default declared in protocol/openapi.yaml.
const defaultThumbSize = 256

// maxThumbSize caps the requested size to keep rendering cost bounded.
const maxThumbSize = 1024

// maxConcurrentThumbRequests bounds endpoint work, including cache reads and
// callers waiting on a coalesced render. Requests above this limit fail fast.
const maxConcurrentThumbRequests = 8

// --------- /thumb GET ---------

func thumbHandler(ops *fsops.Ops, rn *thumbs.Renderer) http.HandlerFunc {
	endpointSlots := make(chan struct{}, maxConcurrentThumbRequests)
	return func(w http.ResponseWriter, r *http.Request) {
		ops := opsFromContext(r.Context(), ops)
		path := r.URL.Query().Get("path")
		if path == "" {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "path required")
			return
		}

		size := defaultThumbSize
		if raw := r.URL.Query().Get("size"); raw != "" {
			n, err := strconv.Atoi(raw)
			if err != nil || n <= 0 {
				writeError(w, http.StatusBadRequest, "BAD_REQUEST", "size must be a positive integer")
				return
			}
			size = n
		}
		if size > maxThumbSize {
			size = maxThumbSize
		}

		select {
		case endpointSlots <- struct{}{}:
			defer func() { <-endpointSlots }()
		default:
			w.Header().Set("Retry-After", "1")
			writeError(w, http.StatusTooManyRequests, "THUMB_BUSY", "too many thumbnail requests; retry shortly")
			return
		}

		source, err := ops.Open(path)
		if err != nil {
			if errors.Is(err, fsops.ErrForbidden) {
				handleFsError(w, err)
			} else if os.IsNotExist(err) {
				writeError(w, http.StatusNotFound, "NOT_AVAILABLE", "no thumbnail available for this file")
			} else {
				writeInternal(w, "thumbnail", err)
			}
			return
		}

		data, err := rn.GetFileContext(r.Context(), source, path, size)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return
			}
			if errors.Is(err, thumbs.ErrNotSupported) || os.IsNotExist(err) {
				writeError(w, http.StatusNotFound, "NOT_AVAILABLE", "no thumbnail available for this file")
				return
			}
			writeInternal(w, "thumbnail", err)
			return
		}

		w.Header().Set("Content-Type", "image/jpeg")
		// Thumbnails derive from authenticated, per-device content — a shared
		// (public) cache must never retain them (PR-60).
		w.Header().Set("Cache-Control", "private, max-age=86400")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
	}
}
