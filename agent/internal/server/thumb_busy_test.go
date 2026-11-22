package server

import (
	"context"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZaidQamhieh/rfe/agent/internal/fsops"
	"github.com/ZaidQamhieh/rfe/agent/internal/thumbs"
)

// busyRemote is a sidecar renderer that is always full.
type busyRemote struct{ calls int }

func (b *busyRemote) Render(context.Context, []byte, int) ([]byte, error) {
	b.calls++
	return nil, fmt.Errorf("%w: render queue is full", thumbs.ErrBusy)
}

// A full sandboxed renderer is a retryable 429 THUMB_BUSY with Retry-After, not a 500 and not a "no thumbnail"
// 404, and the file is not decoded in-process (the built-in decoder could render this PNG).
func TestThumbHandlerAnswers429WhenTheSidecarIsBusy(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "p.png")
	f, err := os.Create(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, image.NewNRGBA(image.Rect(0, 0, 8, 8))); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	renderer, err := thumbs.New(filepath.Join(t.TempDir(), "thumb-cache"))
	if err != nil {
		t.Fatal(err)
	}
	remote := &busyRemote{}
	renderer.UseRemote(remote)

	req := httptest.NewRequest(http.MethodGet, "/v1/thumb?path="+url.QueryEscape(src), nil)
	rr := httptest.NewRecorder()
	thumbHandler(fsops.New([]string{root}, false), renderer)(rr, req)

	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429; body: %s", rr.Code, rr.Body.String())
	}
	if got := rr.Header().Get("Retry-After"); got != busyRetryAfter {
		t.Errorf("Retry-After = %q, want %q", got, busyRetryAfter)
	}
	if body := rr.Body.String(); !strings.Contains(body, "THUMB_BUSY") {
		t.Errorf("body %q does not carry code THUMB_BUSY", body)
	}
	if remote.calls != 1 {
		t.Errorf("sidecar calls = %d, want 1", remote.calls)
	}
}
