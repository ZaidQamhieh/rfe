//go:build !windows

package launcher

import (
	"os"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestSignalIsForwardedToTheAgent starts an agent that waits for SIGTERM, hands the launcher that signal and
// expects the agent to have received it and the launcher to return without restarting anything.
func TestSignalIsForwardedToTheAgent(t *testing.T) {
	r := newRig(t, "v1=wait", "v1")
	r.setState(State{Current: "v1"})
	sig := make(chan os.Signal, 1)
	type result struct {
		code int
		err  error
	}
	done := make(chan result, 1)
	go func() {
		c, err := r.run(sig)
		done <- result{c, err}
	}()
	deadline := time.Now().Add(20 * time.Second)
	for r.started() != "v1" {
		if time.Now().After(deadline) {
			t.Fatal("agent never started")
		}
		time.Sleep(10 * time.Millisecond)
	}
	sig <- syscall.SIGTERM
	select {
	case res := <-done:
		if res.err != nil || res.code != 0 {
			t.Fatalf("code %d err %v", res.code, res.err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("launcher did not return after the signal")
	}
	if got := r.started(); !strings.HasSuffix(got, "term") || strings.Count(got, "v1") != 1 {
		t.Fatalf("log = %q", got)
	}
}
