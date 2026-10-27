package thumbs

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/sidecar"
)

type crashingRemote struct {
	err   error
	calls int
}

func (c *crashingRemote) Render(context.Context, []byte, int) ([]byte, error) {
	c.calls++
	return nil, c.err
}

// A sidecar that dies while it holds a file must not push that file into the unsandboxed in-process decoder, even
// though the built-in decoder could handle it: the first interruption is transient, the second poisons the file.
func TestInterruptedSidecarNeverFallsBackToInProcessDecode(t *testing.T) {
	src, err := os.ReadFile(filepath.Join(fixtureDir, "photo.jpg"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "p.jpg")
	if err := os.WriteFile(path, src, 0o644); err != nil {
		t.Fatal(err)
	}
	remote := &crashingRemote{err: fmt.Errorf("%w: %w: process exited", sidecar.ErrUnavailable, sidecar.ErrInterrupted)}
	rn, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	rn.UseRemote(remote)

	_, err = rn.Get(path, 96)
	if err == nil || errors.Is(err, ErrNotSupported) {
		t.Fatalf("first interruption: err = %v, want a transient error that is not ErrNotSupported", err)
	}
	_, err = rn.Get(path, 96)
	if !errors.Is(err, ErrNotSupported) {
		t.Fatalf("second interruption: err = %v, want ErrNotSupported", err)
	}
	calls := remote.calls
	if _, err = rn.Get(path, 96); !errors.Is(err, ErrNotSupported) || remote.calls != calls {
		t.Fatalf("poisoned file: err = %v, remote calls %d -> %d (must not call the sidecar again)", err, calls, remote.calls)
	}

	// A different version of the same file gets a fresh chance (and here still cannot be decoded in-process).
	time.Sleep(10 * time.Millisecond)
	if err := os.WriteFile(path, append(src, 0), 0o644); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(time.Hour)
	_ = os.Chtimes(path, future, future)
	if _, err = rn.Get(path, 96); err == nil || errors.Is(err, ErrNotSupported) {
		t.Fatalf("changed file: err = %v, want a transient error again", err)
	}
}

// A sidecar that was simply not running (no interruption) still falls back to the built-in decoder.
func TestUnavailableSidecarStillFallsBack(t *testing.T) {
	path := filepath.Join(fixtureDir, "photo.jpg")
	rn, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	rn.UseRemote(&crashingRemote{err: sidecar.ErrUnavailable})
	if data, err := rn.Get(path, 96); err != nil || len(data) == 0 {
		t.Fatalf("fallback render: %v", err)
	}
}

func TestCrashLogIsBounded(t *testing.T) {
	var l crashLog
	for i := 0; i < crashLogCap*2; i++ {
		l.strike(crashKey{path: fmt.Sprint(i)})
	}
	if len(l.strikes) > crashLogCap {
		t.Fatalf("crash log holds %d entries, cap %d", len(l.strikes), crashLogCap)
	}
}

// An open breaker or a sidecar INTERNAL failure (a render panic) is final: the same bytes must not be handed to the
// unsandboxed in-process decoder.
func TestBreakerOpenAndInternalFailureNeverFallBack(t *testing.T) {
	path := filepath.Join(fixtureDir, "photo.jpg")
	cases := map[string]error{
		"breaker open":    fmt.Errorf("%w: %w", sidecar.ErrUnavailable, sidecar.ErrBreakerOpen),
		"render panicked": fmt.Errorf("%w: render panicked", errRemoteFailed),
	}
	for name, remoteErr := range cases {
		t.Run(name, func(t *testing.T) {
			rn, err := New(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			rn.UseRemote(&crashingRemote{err: remoteErr})
			data, err := rn.Get(path, 96)
			if err == nil || len(data) != 0 {
				t.Fatalf("got %d bytes, err %v; want an error and no in-process render", len(data), err)
			}
			if errors.Is(err, ErrNotSupported) {
				t.Fatalf("err = %v, want a transient error, not ErrNotSupported", err)
			}
		})
	}
}

func TestSidecarRemoteMapsInternalToFinalError(t *testing.T) {
	got := mapRemoteError(&sidecar.RemoteError{Code: "INTERNAL", Message: "render panicked"})
	if !errors.Is(got, errRemoteFailed) {
		t.Fatalf("INTERNAL mapped to %v", got)
	}
}

type sequenceRemote struct {
	errs []error
	n    int
}

func (s *sequenceRemote) Render(context.Context, []byte, int) ([]byte, error) {
	e := s.errs[min(s.n, len(s.errs)-1)]
	s.n++
	return nil, e
}

// F-53: right after a crash the sidecar is briefly unavailable; the retry must not decode the suspect file in-process.
func TestRetryDuringRestartDoesNotDecodeInProcess(t *testing.T) {
	path := filepath.Join(fixtureDir, "photo.jpg")
	rn, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	rn.UseRemote(&sequenceRemote{errs: []error{
		fmt.Errorf("%w: %w", sidecar.ErrUnavailable, sidecar.ErrInterrupted),
		sidecar.ErrUnavailable,
	}})
	if _, err := rn.Get(path, 96); err == nil {
		t.Fatal("interrupted render should fail")
	}
	if data, err := rn.Get(path, 96); err == nil || len(data) != 0 {
		t.Fatalf("retry while restarting decoded in-process (err=%v, %d bytes)", err, len(data))
	}
}
