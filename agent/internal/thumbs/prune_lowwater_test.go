package thumbs

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	testSparseFileBytes = 100 << 20 // logical size only: the files are sparse, so no disk is used
	testSmallFileBytes  = 20 << 20
	testSmallFiles      = 25
	testLowWaterNumer   = 9 // the cache is pruned down to 9/10 of its cap
	testLowWaterDenom   = 10
)

// sparseCacheFile creates a renderer-owned cache file of the given logical size with the given age.
func sparseCacheFile(t *testing.T, dir string, n int, size int64, age time.Duration) (string, time.Time) {
	t.Helper()
	name := fmt.Sprintf("%s_%d_%d_%d.jpg", strings.Repeat("ab", 32), 256, n, size)
	path := filepath.Join(dir, name)
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(size); err != nil {
		f.Close()
		t.Skipf("filesystem cannot make sparse files: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	mod := time.Now().Add(-age)
	if err := os.Chtimes(path, mod, mod); err != nil {
		t.Fatal(err)
	}
	return path, mod
}

// Pruning stops at the cap only if it leaves no headroom: the next write would rescan and sort the whole cache
// again. It must go below the cap to a low-water mark, removing the oldest files first.
func TestPruneGoesBelowTheCapToALowWaterMarkAndKeepsTheNewest(t *testing.T) {
	dir := t.TempDir()
	rn, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for i := 0; i < 6; i++ { // 600 MiB, oldest first
		p, _ := sparseCacheFile(t, dir, i, testSparseFileBytes, time.Duration(6-i)*time.Minute)
		paths = append(paths, p)
	}
	rn.cacheMu.Lock()
	rn.pruneCacheLocked()
	rn.cacheMu.Unlock()

	// 600 -> 500 MiB is under the 512 MiB cap but above the 460.8 MiB low-water mark, so two files go.
	for i, p := range paths {
		_, err := os.Stat(p)
		gone := os.IsNotExist(err)
		if want := i < 2; gone != want {
			t.Errorf("file %d (oldest = 0): removed = %v, want %v", i, gone, want)
		}
	}
	if limit := int64(maxThumbCacheBytes) / testLowWaterDenom * testLowWaterNumer; rn.cacheBytes > limit {
		t.Errorf("cacheBytes = %d after pruning, want at most the low-water mark %d", rn.cacheBytes, limit)
	}
}

// A write that pushes the cache over the cap prunes to the low-water mark, so the next writes do not prune again,
// and the entry just written survives.
func TestWriteCachePrunesWithHeadroomAndKeepsTheNewEntry(t *testing.T) {
	dir := t.TempDir()
	rn, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < testSmallFiles; i++ { // 500 MiB of existing entries
		sparseCacheFile(t, dir, i, testSmallFileBytes, time.Duration(testSmallFiles-i)*time.Minute)
	}
	rn.cacheMu.Lock()
	rn.pruneCacheLocked() // 500 MiB is under the cap: nothing is removed, the total is learned
	rn.cacheMu.Unlock()
	if rn.cacheBytes != testSmallFiles*testSmallFileBytes {
		t.Fatalf("setup: cacheBytes = %d", rn.cacheBytes)
	}
	newest := filepath.Join(dir, fmt.Sprintf("%s_%d_%d_%d.jpg", strings.Repeat("cd", 32), 256, 99, 3))
	if err := rn.writeCache(newest, make([]byte, testSmallFileBytes)); err != nil { // 520 MiB: over the cap by 8
		t.Fatal(err)
	}
	if _, err := os.Stat(newest); err != nil {
		t.Fatalf("the newest entry was pruned: %v", err)
	}
	limit := int64(maxThumbCacheBytes) / testLowWaterDenom * testLowWaterNumer
	// Stopping at the cap would leave 500 MiB and make the very next write prune again.
	if rn.cacheBytes > limit {
		t.Errorf("cacheBytes = %d after the over-cap write, want at most %d", rn.cacheBytes, limit)
	}
}
