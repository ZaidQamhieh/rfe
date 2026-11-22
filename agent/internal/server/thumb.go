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

// thumbCacheMaxAge is how long, in seconds, a client may cache a thumbnail.
const thumbCacheMaxAge = 24 * 60 * 60

// thumbCacheControl is the Cache-Control value for thumbnails, built from thumbCacheMaxAge.
var thumbCacheControl = "private, max-age=" + strconv.Itoa(thumbCacheMaxAge)

// thumbNotAvailableMsg is the 404 message for a file that has no thumbnail.
const thumbNotAvailableMsg = "no thumbnail available for this file"

// --------- /thumb GET ---------

// writeThumbBusy answers 429 THUMB_BUSY with Retry-After: thumbnail work is at capacity, try again shortly.
func writeThumbBusy(w http.ResponseWriter) {
	w.Header().Set("Retry-After", busyRetryAfter)
	writeError(w, http.StatusTooManyRequests, "THUMB_BUSY", "too many thumbnail requests; retry shortly")
}

func thumbHandler(ops *fsops.Ops, rn *thumbs.Renderer) http.HandlerFunc {
	endpointSlots := make(chan struct{}, maxConcurrentThumbRequests)
	return func(w http.ResponseWriter, r *http.Request) {
		ops := opsFromContext(r.Context(), ops)
		path := r.URL.Query().Get("path")
		if path == "" {
			writeError(w, http.StatusBadRequest, codeBadRequest, pathRequiredMsg)
			return
		}

		size := defaultThumbSize
		if raw := r.URL.Query().Get("size"); raw != "" {
			n, err := strconv.Atoi(raw)
			if err != nil || n <= 0 {
				writeError(w, http.StatusBadRequest, codeBadRequest, "size must be a positive integer")
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
			writeThumbBusy(w)
			return
		}

		source, err := ops.Open(path)
		if err != nil {
			if errors.Is(err, fsops.ErrForbidden) {
				handleFsError(w, err)
			} else if os.IsNotExist(err) {
				writeError(w, http.StatusNotFound, "NOT_AVAILABLE", thumbNotAvailableMsg)
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
			if errors.Is(err, thumbs.ErrBusy) {
				// The sandboxed renderer is full; the file is never decoded in-process, so the client retries.
				writeThumbBusy(w)
				return
			}
			if errors.Is(err, thumbs.ErrNotSupported) || os.IsNotExist(err) {
				writeError(w, http.StatusNotFound, "NOT_AVAILABLE", thumbNotAvailableMsg)
				return
			}
			writeInternal(w, "thumbnail", err)
			return
		}

		w.Header().Set("Content-Type", "image/jpeg")
		// Thumbnails derive from authenticated, per-device content — a shared
		// (public) cache must never retain them.
		w.Header().Set("Cache-Control", thumbCacheControl)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
	}
}
