package thumbs

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image/color"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// countingRemote is a Remote that always fails the same way and counts how often it was asked to render.
type countingRemote struct {
	calls atomic.Int32
	err   error
}

func (c *countingRemote) Render(context.Context, []byte, int) ([]byte, error) {
	c.calls.Add(1)
	return nil, c.err
}

func isNotSupported(err error) bool { return errors.Is(err, ErrNotSupported) }

// A file rewritten in place with the same name, size, mtime and inode differs only in its change time. The
// cache key must include that, or the old thumbnail is served for the new content.
func TestRendererCacheKeyChangesWhenFileIsRewrittenInPlaceWithSameSizeAndMtime(t *testing.T) {
	root := t.TempDir()
	renderer, err := New(filepath.Join(root, "cache"))
	if err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(root, "a.png")
	old := time.Unix(1000, 0)
	writeTestPNG(t, src, 40, 20, color.NRGBA{R: 255, A: 255})
	redBytes, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	writeTestPNG(t, src, 40, 20, color.NRGBA{R: 200, A: 255})
	blueBytes, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if len(redBytes) != len(blueBytes) || bytes.Equal(redBytes, blueBytes) {
		t.Fatalf("fixture: need two different files of equal size, got %d and %d bytes", len(redBytes), len(blueBytes))
	}
	if err := os.WriteFile(src, redBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(src, old, old); err != nil {
		t.Fatal(err)
	}
	red, err := renderer.Get(src, 16)
	if err != nil {
		t.Fatal(err)
	}
	// Let the file system's change-time clock tick between the two writes.
	time.Sleep(20 * time.Millisecond)
	if err := os.WriteFile(src, blueBytes, 0o600); err != nil { // same inode: truncated and rewritten
		t.Fatal(err)
	}
	if err := os.Chtimes(src, old, old); err != nil {
		t.Fatal(err)
	}
	blue, err := renderer.Get(src, 16)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(red, blue) {
		t.Fatal("a file rewritten with the same size and mtime was served from the cache")
	}
}

// A file that cannot be decoded is decoded once per TTL, not on every request.
func TestFailedRenderIsCachedForTheTTL(t *testing.T) {
	root := t.TempDir()
	renderer, err := New(filepath.Join(root, "cache"))
	if err != nil {
		t.Fatal(err)
	}
	remote := &countingRemote{err: fmt.Errorf("%w: broken", ErrNotSupported)}
	renderer.UseRemote(remote)
	clock := time.Now()
	renderer.now = func() time.Time { return clock }
	src := filepath.Join(root, "broken.png")
	if err := os.WriteFile(src, []byte("not an image"), 0o600); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if _, err := renderer.Get(src, 16); !isNotSupported(err) {
			t.Fatalf("request %d: err = %v, want ErrNotSupported", i, err)
		}
	}
	if got := remote.calls.Load(); got != 1 {
		t.Fatalf("decoded %d times within the TTL, want 1", got)
	}
	clock = clock.Add(negativeCacheTTL + time.Second)
	if _, err := renderer.Get(src, 16); !isNotSupported(err) {
		t.Fatalf("after the TTL: err = %v, want ErrNotSupported", err)
	}
	if got := remote.calls.Load(); got != 2 {
		t.Fatalf("decoded %d times after the TTL, want 2", got)
	}
}

// Cancelled and busy renders are not the file's fault and must not be remembered as failures.
func TestTransientFailuresAreNotCached(t *testing.T) {
	root := t.TempDir()
	renderer, err := New(filepath.Join(root, "cache"))
	if err != nil {
		t.Fatal(err)
	}
	remote := &countingRemote{err: ErrBusy}
	renderer.UseRemote(remote)
	src := filepath.Join(root, "ok.png")
	writeTestPNG(t, src, 40, 20, color.NRGBA{R: 255, A: 255})
	for i := 0; i < 2; i++ {
		if _, err := renderer.Get(src, 16); err == nil {
			t.Fatal("want the busy error")
		}
	}
	if got := remote.calls.Load(); got != 2 {
		t.Fatalf("busy render attempted %d times, want 2 (not cached)", got)
	}
}

// The failure memory is bounded: past the cap the oldest entry is forgotten and the newest kept.
func TestFailedRenderCacheIsCappedAndEvictsOldest(t *testing.T) {
	root := t.TempDir()
	renderer, err := New(filepath.Join(root, "cache"))
	if err != nil {
		t.Fatal(err)
	}
	remote := &countingRemote{err: fmt.Errorf("%w: broken", ErrNotSupported)}
	renderer.UseRemote(remote)
	n := maxNegativeEntries + 1
	paths := make([]string, n)
	for i := range paths {
		paths[i] = filepath.Join(root, fmt.Sprintf("broken-%d.png", i))
		if err := os.WriteFile(paths[i], []byte("not an image"), 0o600); err != nil {
			t.Fatal(err)
		}
		_, _ = renderer.Get(paths[i], 16)
	}
	renderer.mu.Lock()
	size := len(renderer.failures)
	renderer.mu.Unlock()
	if size > maxNegativeEntries {
		t.Fatalf("%d remembered failures, cap is %d", size, maxNegativeEntries)
	}
	before := remote.calls.Load()
	_, _ = renderer.Get(paths[n-1], 16)
	if remote.calls.Load() != before {
		t.Error("the newest failure was forgotten")
	}
	_, _ = renderer.Get(paths[0], 16)
	if remote.calls.Load() != before+1 {
		t.Error("the oldest failure was kept past the cap")
	}
}

// Orphan temp files older than the grace period are removed, so a pile of them cannot keep the cache over its
// cap and make every write prune again.
func TestPruneRemovesOrphanTempFilesPastTheGrace(t *testing.T) {
	dir := t.TempDir()
	var orphans []string
	for i := 0; i < 3; i++ {
		p := filepath.Join(dir, fmt.Sprintf(".thumb-%d.tmp", i))
		f, err := os.Create(p)
		if err != nil {
			t.Fatal(err)
		}
		if err := f.Truncate(testSparseFileBytes * 2); err != nil { // sparse: no disk used
			f.Close()
			t.Skipf("filesystem cannot make sparse files: %v", err)
		}
		f.Close()
		age := time.Now().Add(-10 * time.Minute) // fresh by the old one-hour grace, orphaned by the new one
		if err := os.Chtimes(p, age, age); err != nil {
			t.Fatal(err)
		}
		orphans = append(orphans, p)
	}
	rn, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range orphans {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("orphan temp file past the grace was kept (stat err = %v)", err)
		}
	}
	if rn.cacheBytes != 0 {
		t.Errorf("cacheBytes = %d, want 0", rn.cacheBytes)
	}
}
