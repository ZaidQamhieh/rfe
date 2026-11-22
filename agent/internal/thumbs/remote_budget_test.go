package thumbs

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ZaidQamhieh/rfe/agent/internal/sidecar"
)

// peekRemote records how much of the shared decode budget was held while Render ran.
type peekRemote struct {
	usedDuring int64
	down       error
	calls      int
}

func (p *peekRemote) Available() error { return p.down }

func (p *peekRemote) Render(ctx context.Context, source []byte, maxSize int) ([]byte, error) {
	p.calls++
	fullDecodeBudget.mu.Lock()
	p.usedDuring = fullDecodeBudget.used
	fullDecodeBudget.mu.Unlock()
	return nil, errRemoteUnsupported
}

func TestRenderWithRemoteChargesSourceSizeToDecodeBudget(t *testing.T) {
	path := filepath.Join(fixtureDir, "photo.jpg")
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	rn, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	p := &peekRemote{}
	rn.UseRemote(p)
	if _, err := rn.Get(path, 96); err != nil {
		t.Fatal(err)
	}
	if p.calls != 1 || p.usedDuring < st.Size() {
		t.Fatalf("calls %d, budget held during render %d, want >= %d", p.calls, p.usedDuring, st.Size())
	}
	fullDecodeBudget.mu.Lock()
	left := fullDecodeBudget.used
	fullDecodeBudget.mu.Unlock()
	if left != 0 {
		t.Fatalf("budget leaked %d bytes", left)
	}
}

func TestRenderWithRemoteSkipsReadWhenSidecarDown(t *testing.T) {
	path := filepath.Join(fixtureDir, "photo.jpg")
	rn, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	p := &peekRemote{down: sidecar.ErrUnavailable}
	rn.UseRemote(p)
	if data, err := rn.Get(path, 96); err != nil || len(data) == 0 {
		t.Fatalf("fallback render: %v", err)
	}
	if p.calls != 0 {
		t.Fatal("Render called although the sidecar was unavailable")
	}
	breaker := &peekRemote{down: errors.Join(sidecar.ErrUnavailable, sidecar.ErrBreakerOpen)}
	rn2, _ := New(t.TempDir())
	rn2.UseRemote(breaker)
	if _, err := rn2.Get(path, 96); err == nil {
		t.Fatal("open breaker must not fall back in-process")
	}
}
