package server

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/sidecar"
)

func TestPublishedSidecarStatsAreReadable(t *testing.T) {
	dir := t.TempDir()
	sidecar.ResetEventsForTest()
	sidecar.Note("thumbd.fallback")
	sidecar.Note("thumbd.fallback")
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { PublishSidecarStats(ctx, dir); close(done) }()
	deadline := time.Now().Add(2 * time.Second)
	for {
		rep, age, err := ReadSidecarStats(dir)
		if err == nil {
			if rep.Events["thumbd.fallback"] != 2 || age > time.Minute {
				t.Fatalf("report %+v age %v", rep, age)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("stats file never appeared: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	<-done
}

func TestSidecarReportSaysWhenAnInstallRunsInProcess(t *testing.T) {
	t.Setenv("RFE_SIDECAR_DIR", t.TempDir())
	for _, line := range SidecarReport() {
		if want := "not installed (this build runs it in-process"; !strings.Contains(line, want) {
			t.Fatalf("report line %q lacks %q", line, want)
		}
	}
}
