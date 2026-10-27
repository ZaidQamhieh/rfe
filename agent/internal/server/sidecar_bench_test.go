package server

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/fsops"
	"github.com/ZaidQamhieh/rfe/agent/internal/sidecar"
)

// vmHWM reads a process's peak resident set (kB) from /proc; 0 where that does not exist.
func vmHWM(pid string) int64 {
	b, err := os.ReadFile("/proc/" + pid + "/status")
	if err != nil {
		return 0
	}
	for _, l := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(l, "VmHWM:") {
			var kb int64
			fmt.Sscanf(strings.TrimSpace(strings.TrimPrefix(l, "VmHWM:")), "%d", &kb)
			return kb
		}
	}
	return 0
}

func median(d []time.Duration) time.Duration {
	sort.Slice(d, func(i, j int) bool { return d[i] < d[j] })
	return d[len(d)/2]
}

// TestBenchIndex measures the Go index against rfe-indexd on a real tree. It only runs when RFE_BENCH_ROOT is
// set, and RFE_BENCH_IMPL picks "go" or "rust" so each gets a process (and so a peak RSS) of its own:
//
//	RFE_BENCH_ROOT=$HOME RFE_BENCH_IMPL=go go test -run TestBenchIndex -v ./internal/server
func TestBenchIndex(t *testing.T) {
	root := os.Getenv("RFE_BENCH_ROOT")
	if root == "" {
		t.Skip("RFE_BENCH_ROOT not set")
	}
	impl := os.Getenv("RFE_BENCH_IMPL")
	ops := fsops.New([]string{root}, false)
	queries := []string{"q=photo", "q=" + "%2A.jpg", "q=e&types=image", "q=zzzqqq-no-match"}
	roots := []string{root}

	var qtimes = map[string]time.Duration{}
	var build time.Duration
	var entries int
	var sidecarKB int64

	switch impl {
	case "rust":
		bin := indexdBinary(t)
		si := &sidecarIndex{ops: ops, sup: sidecar.NewSupervisor(sidecar.Config{Name: "rfe-indexd", Path: bin}, nil)}
		si.sup.Run()
		defer si.sup.Stop()
		for i := 0; i < 400; i++ {
			if _, err := si.sup.Client(); err == nil {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		c := mustClient(t, si)
		start := time.Now()
		var resp buildResp
		if err := c.Call(context.Background(), "index.build", buildReq{Roots: roots, MaxEntries: indexMaxEntries, MaxBytes: indexMaxEstimatedBytes}, &resp); err != nil {
			t.Fatal(err)
		}
		build = time.Since(start)
		entries = resp.Entries
		for _, q := range queries {
			var ds []time.Duration
			for i := 0; i < 15; i++ {
				s := time.Now()
				si.query(diffQuery(t, q), roots, 200)
				ds = append(ds, time.Since(s))
			}
			qtimes[q] = median(ds)
		}
		sidecarKB = vmHWM(fmt.Sprint(c.Pid()))
	default:
		t.Fatal("RFE_BENCH_IMPL must be go or rust")
	}
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	t.Logf("BENCH impl=%s root=%s entries=%d build=%v goProcessPeakRSS=%dMB sidecarPeakRSS=%dMB goHeapInuse=%dMB",
		impl, root, entries, build.Round(time.Millisecond), vmHWM("self")/1024, sidecarKB/1024, ms.HeapInuse>>20)
	for _, q := range queries {
		t.Logf("BENCH   query %-24s median %v", q, qtimes[q])
	}
}

func mustClient(t *testing.T, si *sidecarIndex) *sidecar.Client {
	t.Helper()
	c, err := si.sup.Client()
	if err != nil {
		t.Fatal(err)
	}
	return c
}
