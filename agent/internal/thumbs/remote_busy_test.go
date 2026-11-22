package thumbs

import (
	"context"
	"errors"
	"image/color"
	"path/filepath"
	"testing"

	"github.com/ZaidQamhieh/rfe/agent/internal/sidecar"
)

// codeRemote is a Remote that fails the way SidecarRemote does for a given sidecar status code.
type codeRemote struct {
	code  string
	calls int
}

func (c *codeRemote) Render(context.Context, []byte, int) ([]byte, error) {
	c.calls++
	return nil, mapRemoteError(&sidecar.RemoteError{Code: c.code, Message: "render queue is full"})
}

// A BUSY sidecar is healthy but full. Falling back to the in-process decoder would run the untrusted bytes
// unsandboxed exactly when the sandbox is loaded, so the render fails with a retryable ErrBusy instead.
func TestSidecarBusyNeverFallsBackToInProcessDecode(t *testing.T) {
	src := filepath.Join(t.TempDir(), "p.png") // the built-in decoder could render this
	writeTestPNG(t, src, 40, 20, color.NRGBA{R: 10, G: 200, B: 10, A: 255})
	remote := &codeRemote{code: "BUSY"}
	rn, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	rn.UseRemote(remote)

	data, err := rn.Get(src, 16)
	if err == nil {
		t.Fatalf("a BUSY sidecar produced a thumbnail (%d bytes): the in-process decoder ran", len(data))
	}
	if !errors.Is(err, ErrBusy) {
		t.Errorf("err = %v, want ErrBusy", err)
	}
	if errors.Is(err, ErrNotSupported) {
		t.Errorf("err = %v: a busy sidecar must not read as 'no thumbnail for this file'", err)
	}
	// Nothing may be cached from a failure: the next try asks the sidecar again.
	if _, err := rn.Get(src, 16); !errors.Is(err, ErrBusy) || remote.calls != 2 {
		t.Errorf("second Get: err = %v, sidecar calls = %d, want ErrBusy and 2", err, remote.calls)
	}
}

// NOT_SUPPORTED still falls back to the built-in decoder.
func TestSidecarNotSupportedStillFallsBackWhenOthersAreMapped(t *testing.T) {
	src := filepath.Join(t.TempDir(), "p.png")
	writeTestPNG(t, src, 40, 20, color.NRGBA{R: 10, G: 200, B: 10, A: 255})
	rn, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	rn.UseRemote(&codeRemote{code: "NOT_SUPPORTED"})
	if _, err := rn.Get(src, 16); err != nil {
		t.Fatalf("NOT_SUPPORTED did not fall back: %v", err)
	}
}
