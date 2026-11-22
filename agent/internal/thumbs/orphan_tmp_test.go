package thumbs

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A crash between creating and renaming a cache write leaves a ".thumb-*.tmp" file nobody owns. Pruning must
// remove stale ones, and count recent ones (a write may be in flight) toward the cache size without touching them.
func TestPruneRemovesStaleOrphanTempFilesAndCountsRecentOnes(t *testing.T) {
	dir := t.TempDir()
	stale := filepath.Join(dir, ".thumb-111.tmp")
	recent := filepath.Join(dir, ".thumb-222.tmp")
	foreign := filepath.Join(dir, ".thumb-notours.dat")
	for p, n := range map[string]int{stale: 700, recent: 300, foreign: 50} {
		if err := os.WriteFile(p, make([]byte, n), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-3 * time.Hour)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(foreign, old, old); err != nil {
		t.Fatal(err)
	}

	rn, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("stale orphan temp file was kept (stat err = %v)", err)
	}
	if _, err := os.Stat(recent); err != nil {
		t.Errorf("recent temp file (possibly mid-write) was removed: %v", err)
	}
	if _, err := os.Stat(foreign); err != nil {
		t.Errorf("a file that is not a renderer temp file was removed: %v", err)
	}
	if rn.cacheBytes != 300 {
		t.Errorf("cacheBytes = %d, want 300 (the recent temp file counts, the stale one is gone)", rn.cacheBytes)
	}
}
