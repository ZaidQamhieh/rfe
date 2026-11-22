package transfer

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/store"
)

// h6StaleRow stores an open session that never got a chunk and was last touched two hours ago, with its temp file.
func h6StaleRow(t *testing.T, tm *Manager, db *store.DB, id string) {
	t.Helper()
	tr := &store.Transfer{ID: id, TargetPath: filepath.Join(t.TempDir(), id), TotalSize: 4, ChunkSize: 4, TotalChunks: 1,
		SHA256: sha256hex([]byte("data")), TempPath: tm.tempPathFor(id), Status: store.TransferOpen, DeviceID: "dev-stale"}
	if err := db.CreateTransfer(tr); err != nil {
		t.Fatal(err)
	}
	if err := db.TouchOpenTransfer(id, time.Now().Add(-2*time.Hour).Unix()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tm.tempPathFor(id), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
}

// OpenSession admits a session without running the stale sweep: leftovers (many old orphan temps, stale
// empty sessions) stay for the hourly sweep, so the open's cost does not grow with them and nothing
// walks the temp folder under the manager's lock.
func TestH6OpenSessionDoesNotRunTheStaleSweep(t *testing.T) {
	tm, db, dataDir := setupManager(t)
	old := time.Now().Add(-(TransferStaleAfter + 24*time.Hour))
	const orphans = 5000
	for i := 0; i < orphans; i++ {
		p := filepath.Join(tm.tempDir, fmt.Sprintf("orphan-%04d.tmp", i))
		if err := os.WriteFile(p, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, old, old); err != nil {
			t.Fatal(err)
		}
	}
	h6StaleRow(t, tm, db, "h6-stale-1")
	h6StaleRow(t, tm, db, "h6-stale-2")

	start := time.Now()
	if _, err := tm.OpenSession("h6-fresh", filepath.Join(dataDir, "fresh.bin"), 4, 4, sha256hex([]byte("data")), false, "dev"); err != nil {
		t.Fatal(err)
	}
	t.Logf("OpenSession with %d orphans and 2 stale rows: %s", orphans, time.Since(start))

	entries, err := os.ReadDir(tm.tempDir)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(entries), orphans+3; got != want {
		t.Errorf("temp folder holds %d entries after OpenSession, want %d: the open ran the sweep", got, want)
	}
	for _, id := range []string{"h6-stale-1", "h6-stale-2"} {
		if tr, _ := db.GetTransfer(id); tr == nil {
			t.Errorf("stale row %s was removed by OpenSession", id)
		}
	}

	// The sweep that runs hourly still removes all of it.
	if _, err := tm.CleanupStaleSessions(time.Now()); err != nil {
		t.Fatal(err)
	}
	entries, _ = os.ReadDir(tm.tempDir)
	if len(entries) != 1 {
		t.Errorf("temp folder holds %d entries after the sweep, want only the fresh session's temp", len(entries))
	}
	for _, id := range []string{"h6-stale-1", "h6-stale-2"} {
		if tr, _ := db.GetTransfer(id); tr != nil {
			t.Errorf("stale row %s survived the sweep", id)
		}
	}
	if tr, _ := db.GetTransfer("h6-fresh"); tr == nil {
		t.Errorf("the fresh session was swept")
	}
}

// The periodic worker sweeps at startup and on every tick.
func TestH6HourlyWorkerRemovesStaleRows(t *testing.T) {
	tm, db, _ := setupManager(t)
	h6StaleRow(t, tm, db, "h6-startup")
	stop := tm.StartStaleCleanup(10 * time.Millisecond)
	defer stop()
	if tr, _ := db.GetTransfer("h6-startup"); tr != nil {
		t.Errorf("startup sweep left a stale row")
	}
	h6StaleRow(t, tm, db, "h6-tick")
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if tr, _ := db.GetTransfer("h6-tick"); tr == nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Errorf("the periodic sweep never removed the stale row")
}
