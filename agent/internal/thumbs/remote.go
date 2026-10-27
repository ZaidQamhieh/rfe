package thumbs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/ZaidQamhieh/rfe/agent/internal/sidecar"
)

// Remote renders a thumbnail somewhere else (the rfe-thumbd sidecar). The returned errors tell the renderer what
// to do next: errRemoteUnsupported means "this process cannot decode it, try the built-in decoder", ErrNotSupported
// means "nobody can", and a sidecar that is merely not running also falls back to the built-in decoder. A render failure (INTERNAL) or an
// open breaker is a final error: it never falls back.
type Remote interface {
	Render(ctx context.Context, source []byte, maxSize int) ([]byte, error)
}

// errRemoteFailed means the sidecar's decoder failed or panicked on this input. The built-in decoder would run the
// same untrusted bytes unsandboxed, so the render fails instead.
var errRemoteFailed = errors.New("thumbnail sidecar failed to render this file")

var errRemoteUnsupported = errors.New("remote renderer cannot decode this source")

// UseRemote makes the renderer try r before its own decoder. It must be called before the renderer is shared.
func (rn *Renderer) UseRemote(r Remote) { rn.remote = r }

// SidecarRemote adapts a supervised rfe-thumbd to Remote.
type SidecarRemote struct{ Sup *sidecar.Supervisor }

type renderResp struct {
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Format string `json:"format"`
}

func (r SidecarRemote) Render(ctx context.Context, source []byte, maxSize int) ([]byte, error) {
	var resp renderResp
	jpeg, err := r.Sup.CallBody(ctx, "thumb.render", map[string]int{"maxSize": maxSize}, source, &resp)
	if err != nil {
		return nil, mapRemoteError(err)
	}
	if len(jpeg) < 4 || jpeg[0] != 0xff || jpeg[1] != 0xd8 {
		return nil, errors.New("sidecar returned something that is not a JPEG")
	}
	return jpeg, nil
}

func mapRemoteError(err error) error {
	var re *sidecar.RemoteError
	if errors.As(err, &re) {
		switch re.Code {
		case "NOT_SUPPORTED":
			return errRemoteUnsupported
		case "TOO_LARGE":
			return fmt.Errorf("%w: %s", ErrNotSupported, re.Message)
		case "INTERNAL":
			return fmt.Errorf("%w: %s", errRemoteFailed, re.Message)
		}
	}
	return err
}

// renderWithRemote tries the remote renderer and reports whether it produced the result (or a final error). When
// it did not (unsupported format, sidecar down or misbehaving), the caller uses the built-in decoder.
func (rn *Renderer) renderWithRemote(ctx context.Context, f *os.File, size int64, maxSize int) (data []byte, final bool, err error) {
	if rn.remote == nil || size <= 0 || size > maxThumbSourceBytes {
		return nil, false, nil
	}
	release, err := acquireDecodeSlot(ctx, fullDecodeSem)
	if err != nil {
		return nil, true, err
	}
	defer release()
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, true, err
	}
	src, err := io.ReadAll(io.LimitReader(f, maxThumbSourceBytes+1))
	if err != nil {
		return nil, true, err
	}
	if int64(len(src)) > maxThumbSourceBytes {
		return nil, false, nil // grew while reading; the built-in path applies its own cap
	}
	key, haveKey := crashKeyOf(f)
	if haveKey && rn.crashes.poisoned(key) {
		return nil, true, fmt.Errorf("%w: the thumbnail sidecar crashed on this file before", ErrNotSupported)
	}
	data, err = rn.remote.Render(ctx, src, maxSize)
	switch {
	case err == nil:
		return data, true, nil
	case ctx.Err() != nil:
		return nil, true, ctx.Err()
	case errors.Is(err, ErrNotSupported):
		return nil, true, err
	case errors.Is(err, errRemoteUnsupported):
		sidecar.Note("thumbd.unsupported-format") // expected: the built-in decoder handles it
		return nil, false, nil
	case errors.Is(err, sidecar.ErrInterrupted):
		sidecar.Note("thumbd.interrupted")
		// The sidecar died while it held this file. Decoding it here would run the very input that may have just
		// killed the sandboxed decoder inside the unsandboxed agent, so it is not retried in-process. The first
		// strike is a transient error (the phone retries on the restarted sidecar); a second on the same file
		// marks it as one the sidecar cannot survive.
		if haveKey && rn.crashes.strike(key) {
			sidecar.Note("thumbd.poisoned-file")
			return nil, true, fmt.Errorf("%w: the thumbnail sidecar crashed on this file twice", ErrNotSupported)
		}
		return nil, true, fmt.Errorf("thumbnail sidecar was interrupted: %w", err)
	case errors.Is(err, errRemoteFailed):
		sidecar.Note("thumbd.render-failed")
		return nil, true, err
	case errors.Is(err, sidecar.ErrBreakerOpen):
		// Repeated start failures: the sidecar is untrustworthy right now, not merely absent.
		sidecar.Note("thumbd.breaker-open-refused")
		return nil, true, fmt.Errorf("thumbnail sidecar is unavailable: %w", err)
	default:
		// A file the sidecar already died on must not take the in-process path while the sidecar restarts:
		// that would run the suspect input unsandboxed and skip the two-strike rule (audit F-53).
		if haveKey && rn.crashes.struck(key) {
			sidecar.Note("thumbd.restarting-refused")
			return nil, true, fmt.Errorf("thumbnail sidecar is restarting: %w", err)
		}
		// Not running, refused or misbehaving: this render ran in-process instead.
		sidecar.Note("thumbd.fallback")
		return nil, false, nil
	}
}

// crashKey identifies a file version: a changed file gets a fresh chance.
type crashKey struct {
	path string
	size int64
	mod  int64
}

func crashKeyOf(f *os.File) (crashKey, bool) {
	st, err := f.Stat()
	if err != nil {
		return crashKey{}, false
	}
	return crashKey{path: f.Name(), size: st.Size(), mod: st.ModTime().UnixNano()}, true
}

const crashLogCap = 256

// crashLog remembers (bounded, oldest out first) the files a sidecar render was interrupted on. Other renders in
// flight when the sidecar dies are interrupted too, which is why one strike does not condemn a file.
type crashLog struct {
	mu      sync.Mutex
	strikes map[crashKey]int
	order   []crashKey
}

// struck reports whether the sidecar was interrupted on this file at least once.
func (l *crashLog) struck(k crashKey) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.strikes[k] >= 1
}

func (l *crashLog) poisoned(k crashKey) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.strikes[k] >= 2
}

// strike records one interruption and reports whether the file is now poisoned.
func (l *crashLog) strike(k crashKey) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.strikes == nil {
		l.strikes = make(map[crashKey]int)
	}
	if _, seen := l.strikes[k]; !seen {
		l.order = append(l.order, k)
		if len(l.order) > crashLogCap {
			delete(l.strikes, l.order[0])
			l.order = l.order[1:]
		}
	}
	l.strikes[k]++
	return l.strikes[k] >= 2
}
