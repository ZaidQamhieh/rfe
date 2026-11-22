package thumbs

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"github.com/ZaidQamhieh/rfe/agent/internal/sidecar"
)

// sidecarOp is the rfe-thumbd operation that renders one thumbnail.
const sidecarOp = "thumb.render"

// jpegMagic is the start of every JPEG file; the sidecar's answer must have it.
var jpegMagic = []byte{0xff, 0xd8}

// minJPEGBytes is the shortest answer that can be a JPEG (marker plus one segment byte).
const minJPEGBytes = 4

// BodyCaller is the part of *sidecar.Supervisor the renderer uses.
type BodyCaller interface {
	CallBody(ctx context.Context, op string, req any, body []byte, resp any) ([]byte, error)
}

// SidecarRenderer is a Renderer backed by the Rust rfe-thumbd sidecar (a
// supervised process that decodes in a sandbox, see protocol/sidecar.md).
type SidecarRenderer struct{ Caller BodyCaller }

// Render sends the source bytes to the sidecar and returns its JPEG. Failures
// come back as the package's sentinels, see mapSidecarError.
func (r SidecarRenderer) Render(ctx context.Context, source []byte, maxSize int) ([]byte, error) {
	out, err := r.Caller.CallBody(ctx, sidecarOp, map[string]int{"maxSize": maxSize}, source, nil)
	if err != nil {
		return nil, mapSidecarError(err)
	}
	if len(out) < minJPEGBytes || !bytes.HasPrefix(out, jpegMagic) {
		return nil, fmt.Errorf("%w: the sidecar answered something that is not a JPEG", ErrRenderFailed)
	}
	return out, nil
}

// mapSidecarError turns a sidecar failure into a sentinel that tells the
// Service whether the built-in decoder may take over. Only "cannot decode this
// format" (NOT_SUPPORTED) and "not running" fall back. BUSY and BAD_REQUEST
// answers, like any code not listed, are final: the sandboxed renderer was
// reached and refused, so the untrusted bytes must not be decoded unsandboxed
// (and a busy sidecar must not let a file skip the two-strike crash rule).
func mapSidecarError(err error) error {
	var re *sidecar.RemoteError
	if errors.As(err, &re) {
		switch re.Code {
		case "NOT_SUPPORTED":
			return fmt.Errorf("%w: %s", ErrUnsupported, re.Message)
		case "TOO_LARGE":
			return fmt.Errorf("%w: %s", ErrNotAvailable, re.Message)
		case "BUSY":
			return fmt.Errorf("%w: %s", ErrBusy, re.Message)
		default: // INTERNAL, BAD_REQUEST and codes this build does not know
			return fmt.Errorf("%w: %s: %s", ErrRenderFailed, re.Code, re.Message)
		}
	}
	switch {
	case errors.Is(err, sidecar.ErrBreakerOpen):
		return fmt.Errorf("%w: %w", ErrRendererUnavailable, err)
	case errors.Is(err, sidecar.ErrInterrupted):
		return fmt.Errorf("%w: %w", ErrInterrupted, err)
	case errors.Is(err, sidecar.ErrUnavailable):
		return fmt.Errorf("%w: %w", ErrDown, err)
	}
	return err
}
