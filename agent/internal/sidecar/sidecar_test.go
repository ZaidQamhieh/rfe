package sidecar

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

func startFake(t *testing.T, mode string) *Client {
	t.Helper()
	c, err := Start(fakeCfg(mode))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func TestCallRoundTrip(t *testing.T) {
	c := startFake(t, "ok")
	var resp struct{ Text string }
	if err := c.Call(context.Background(), "echo", map[string]string{"text": "hi"}, &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Text != "hi" {
		t.Fatalf("got %q", resp.Text)
	}
	if c.Version() != "1.0.0" {
		t.Fatalf("version %q", c.Version())
	}
}

func TestRemoteErrorAndUnknownOp(t *testing.T) {
	c := startFake(t, "ok")
	var re *RemoteError
	if err := c.Call(context.Background(), "fail", struct{}{}, nil); !errors.As(err, &re) || re.Code != "BAD_REQUEST" {
		t.Fatalf("got %v", err)
	}
	if err := c.Call(context.Background(), "nope", struct{}{}, nil); !errors.As(err, &re) || re.Code != "NOT_SUPPORTED" {
		t.Fatalf("got %v", err)
	}
}

func TestConcurrentCallsAreMatchedByID(t *testing.T) {
	c := startFake(t, "ok")
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			want := strings.Repeat("a", i+1)
			var resp struct{ Text string }
			if err := c.Call(context.Background(), "echo", map[string]string{"text": want}, &resp); err != nil || resp.Text != want {
				t.Errorf("call %d: %v %q", i, err, resp.Text)
			}
		}()
	}
	wg.Wait()
}

func TestBodyFrame(t *testing.T) {
	c := startFake(t, "ok")
	body, err := c.CallBody(context.Background(), "body", struct{}{}, nil, nil)
	if err != nil || len(body) != 1<<20 {
		t.Fatalf("len %d err %v", len(body), err)
	}
}

func TestCancelSendsCancelAndReturnsCtxErr(t *testing.T) {
	c := startFake(t, "ok")
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := c.Call(ctx, "slow", struct{}{}, nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v", err)
	}
	if time.Since(start) > time.Second {
		t.Fatal("cancel was not prompt")
	}
	// The connection stays usable afterwards.
	if err := c.Call(context.Background(), "ping", struct{}{}, nil); err != nil {
		t.Fatal(err)
	}
}

func TestCrashFailsPendingAndMarksUnavailable(t *testing.T) {
	c := startFake(t, "ok")
	err := c.Call(context.Background(), "crash", struct{}{}, nil)
	if !errors.Is(err, ErrUnavailable) || !errors.Is(err, ErrInterrupted) {
		t.Fatalf("a crash during a call must be unavailable AND interrupted, got %v", err)
	}
	select {
	case <-c.Done():
	case <-time.After(time.Second):
		t.Fatal("Done not closed")
	}
	err = c.Call(context.Background(), "ping", struct{}{}, nil)
	if !errors.Is(err, ErrUnavailable) || errors.Is(err, ErrInterrupted) {
		t.Fatalf("a call to an already dead sidecar is unavailable but not interrupted, got %v", err)
	}
}

func TestHandshakeRefusals(t *testing.T) {
	for _, mode := range []string{"badproto", "badname"} {
		if _, err := Start(fakeCfg(mode)); err == nil {
			t.Fatalf("%s: handshake accepted", mode)
		}
	}
	cfg := fakeCfg("ok")
	cfg.ExpectVersion = "2.0.0"
	if _, err := Start(cfg); err == nil {
		t.Fatal("version mismatch accepted")
	}
	cfg = fakeCfg("ok")
	cfg.Path = "/nonexistent/rfe-fake"
	if _, err := Start(cfg); err == nil {
		t.Fatal("missing binary accepted")
	}
}

func TestSupervisorRestartsAfterCrash(t *testing.T) {
	var ready sync.WaitGroup
	ready.Add(2)
	var once [2]sync.Once
	n := 0
	var mu sync.Mutex
	s := NewSupervisor(fakeCfg("ok"), func(*Client) {
		mu.Lock()
		i := n
		n++
		mu.Unlock()
		if i < 2 {
			once[i].Do(ready.Done)
		}
	})
	s.minBackoff, s.maxBackoff, s.pingEvery = 20*time.Millisecond, 100*time.Millisecond, time.Hour
	s.Run()
	defer s.Stop()

	waitUntil(t, func() bool { _, err := s.Client(); return err == nil })
	_ = s.Call(context.Background(), "crash", struct{}{}, nil)
	if _, err := s.Client(); err == nil {
		waitUntil(t, func() bool { _, err := s.Client(); return err != nil })
	}
	done := make(chan struct{})
	go func() { ready.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("sidecar was not restarted")
	}
	if err := s.Call(context.Background(), "ping", struct{}{}, nil); err != nil {
		t.Fatal(err)
	}
	st := s.Stats()
	if st.Starts < 2 || st.Crashes < 1 || !st.Running {
		t.Fatalf("stats after a crash and restart: %+v", st)
	}
}

func TestSupervisorCountsStartFailuresAndTheBreaker(t *testing.T) {
	ResetEventsForTest()
	cfg := fakeCfg("ok")
	cfg.Path = "/nonexistent/rfe-fake"
	cfg.Name = "fake-breaker"
	s := NewSupervisor(cfg, nil)
	s.minBackoff, s.maxBackoff = time.Millisecond, 5*time.Millisecond
	s.Run()
	defer s.Stop()
	waitUntil(t, func() bool { return s.Stats().BreakerOpen })
	st := s.Stats()
	if st.StartFailures < breakerFailures || st.BreakerOpens != 1 || st.Running {
		t.Fatalf("stats: %+v", st)
	}
	if _, err := s.Client(); !errors.Is(err, ErrUnavailable) || !errors.Is(err, ErrBreakerOpen) {
		t.Fatalf("Client() with the breaker open: %v, want ErrUnavailable and ErrBreakerOpen", err)
	}
	rep := Snapshot()
	if rep.Events["fake-breaker.breaker-open"] != 1 {
		t.Fatalf("events: %v", rep.Events)
	}
	var found bool
	for _, x := range rep.Sidecars {
		found = found || x.Name == "fake-breaker"
	}
	if !found {
		t.Fatalf("snapshot lacks the supervisor: %+v", rep.Sidecars)
	}
}

func TestSupervisorUnavailableWhenBinaryMissing(t *testing.T) {
	cfg := fakeCfg("ok")
	cfg.Path = "/nonexistent/rfe-fake"
	s := NewSupervisor(cfg, nil)
	s.minBackoff, s.maxBackoff = 10*time.Millisecond, 20*time.Millisecond
	s.Run()
	defer s.Stop()
	time.Sleep(60 * time.Millisecond)
	if err := s.Call(context.Background(), "ping", struct{}{}, nil); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("got %v", err)
	}
}

func waitUntil(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition not reached")
}

func TestReadFrameDoesNotTrustDeclaredLength(t *testing.T) {
	hdr := []byte{0x09, 0xa0, 0x00, 0x00, 1, 2, 3} // declares ~160 MiB, delivers 3 bytes
	if _, err := readFrame(bytes.NewReader(hdr)); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("err = %v, want unexpected EOF", err)
	}
}

// A reply written just before the sidecar exits must still reach the caller: Wait may not close the pipe early.
func TestReplyBeforeExitIsDelivered(t *testing.T) {
	for i := 0; i < 30; i++ {
		c, err := Start(fakeCfg("ok"))
		if err != nil {
			t.Fatal(err)
		}
		var resp struct{ Text string }
		if err := c.Call(context.Background(), "replyexit", struct{}{}, &resp); err != nil || resp.Text != "bye" {
			_ = c.Close()
			t.Fatalf("run %d: %v %q", i, err, resp.Text)
		}
		_ = c.Close()
	}
}

func TestStopWithoutRunReturns(t *testing.T) {
	s := NewSupervisor(fakeCfg("ok"), nil)
	done := make(chan struct{})
	go func() { s.Stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Stop blocked although Run was never called")
	}
}
