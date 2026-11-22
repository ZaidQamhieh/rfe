package api

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"net/http"
	"strconv"

	"github.com/ZaidQamhieh/rfe/agent/internal/next/rootfs"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/thumbs"
)

// Query parameters, response headers and fixed texts of GET /thumb.
const (
	queryThumbSize = "size"
	headerETag     = "ETag"

	// thumbCacheControl lets only the phone's own cache keep a thumbnail: it comes
	// from authenticated, per-device content, so a shared cache must not.
	thumbCacheControl = "private, max-age=86400"
	// thumbRetryAfterSeconds is the Retry-After of a 429 THUMB_BUSY.
	thumbRetryAfterSeconds = "1"

	msgThumbPathRequired = "path required"
	msgThumbBadSize      = "size must be a positive integer"
	msgThumbBusy         = "too many thumbnail requests; retry shortly"
	msgThumbNone         = "no thumbnail available for this file"
	msgOutsideFolders    = "path is outside the folders this device may use"
	msgInvalidPath       = "path is not valid"
)

// ThumbService renders thumbnails. *thumbs.Service satisfies it.
type ThumbService interface {
	Get(ctx context.Context, set *rootfs.Set, path string, size int) (*thumbs.Result, error)
}

// ThumbSetFor returns the folders a device with the given jail may reach (the
// whole set when jail is empty) and a function that releases that view. It must
// be safe for concurrent use.
type ThumbSetFor func(jail string) (set *rootfs.Set, release func(), err error)

// thumb answers GET /thumb: the JPEG thumbnail of one file, rounded to a size
// bucket, with an ETag so an unchanged file answers 304 Not Modified. A busy
// renderer is 429 THUMB_BUSY with Retry-After; a file with no thumbnail is 404.
func (s *Server) thumb(w http.ResponseWriter, r *http.Request) error {
	svc, setFor := s.cfg.Misc.Thumbs, s.cfg.Misc.ThumbSets
	if svc == nil || setFor == nil {
		return notYet
	}
	c, err := callOf(r)
	if err != nil {
		return err
	}
	q := r.URL.Query()
	path := q.Get(queryPath)
	if path == "" {
		return badRequest(msgThumbPathRequired)
	}
	size := 0 // the service picks its default bucket
	if raw := q.Get(queryThumbSize); raw != "" {
		n, convErr := strconv.Atoi(raw)
		if convErr != nil || n <= 0 {
			return badRequest(msgThumbBadSize)
		}
		size = n // a size above the largest bucket is clamped by the service
	}
	set, release, err := setFor(c.principal.JailRoot)
	if err != nil {
		return thumbError(w, err)
	}
	defer release()
	res, err := svc.Get(r.Context(), set, path, size)
	if err != nil {
		if clientGone(r, err) {
			return nil
		}
		return thumbError(w, err)
	}
	h := w.Header()
	h.Set(headerContentType, res.ContentType)
	h.Set(headerCacheControl, thumbCacheControl)
	h.Set(headerETag, res.ETag)
	// ServeContent answers If-None-Match (weak comparison) and If-Modified-Since
	// with 304, and sets Last-Modified from the source's modification time.
	http.ServeContent(w, r, "", res.ModTime, bytes.NewReader(res.Data))
	return nil
}

// thumbError turns an error of the thumbnail service or of opening the file into
// the error to send. Texts are fixed because rootfs errors name host paths. A
// busy answer also sets Retry-After on w.
func thumbError(w http.ResponseWriter, err error) error {
	switch {
	case errors.Is(err, thumbs.ErrBusy), errors.Is(err, thumbs.ErrInterrupted):
		w.Header().Set(headerRetryAfter, thumbRetryAfterSeconds)
		return &apiError{kind: ErrThumbBusy, msg: msgThumbBusy}
	case errors.Is(err, thumbs.ErrBadSize):
		return badRequest(msgThumbBadSize)
	case errors.Is(err, thumbs.ErrNotAvailable), errors.Is(err, thumbs.ErrUnsupported),
		errors.Is(err, thumbs.ErrRenderFailed), errors.Is(err, thumbs.ErrDown),
		errors.Is(err, thumbs.ErrRendererUnavailable), errors.Is(err, fs.ErrNotExist):
		return &apiError{kind: ErrNotAvailable, msg: msgThumbNone}
	case errors.Is(err, rootfs.ErrOutsideRoots), errors.Is(err, rootfs.ErrProtected):
		return &apiError{kind: ErrForbidden, msg: msgOutsideFolders}
	case errors.Is(err, rootfs.ErrInvalidPath):
		return badRequest(msgInvalidPath)
	}
	return err
}
