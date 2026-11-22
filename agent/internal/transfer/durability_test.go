package transfer

import (
	"bytes"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

const durChunk = 64 << 10

// durContent returns n random bytes: the file a test uploads.
func durContent(t *testing.T, n int) []byte {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return b
}

// durPart is chunk n of content, cut at the chunk size.
func durPart(content []byte, n int) []byte {
	return content[n*durChunk : min((n+1)*durChunk, len(content))]
}

// enospc is the error a full disk returns from a write or allocation.
func enospc(op string) error { return &os.PathError{Op: op, Path: "temp", Err: syscall.ENOSPC} }

// sparseReservations makes OpenSession size temp files by Truncate, as before space reservation existed, for
// tests that declare sizes (tens of GiB) the test disk cannot hold.
func sparseReservations(t *testing.T) {
	t.Helper()
	old := reserveSpace
	reserveSpace = func(f *os.File, size int64) error { return f.Truncate(size) }
	t.Cleanup(func() { reserveSpace = old })
}

// swapSyncData replaces the data-sync seam for one test.
func swapSyncData(t *testing.T, fn func(*os.File) error) {
	t.Helper()
	old := syncData
	syncData = fn
	t.Cleanup(func() { syncData = old })
}

// TestPowerLossNeverLosesAnAcknowledgedChunk simulates the disk image after power loss: only bytes that were
// synced survive, everything else reads back as zeros. A chunk the agent recorded as received must then
// still hold its real bytes, so resuming and completing succeeds instead of failing the whole-file hash.
func TestPowerLossNeverLosesAnAcknowledgedChunk(t *testing.T) {
	tm, db, dataDir := setupManager(t)
	content := durContent(t, 4*durChunk+1000)
	target := filepath.Join(dataDir, "out.bin")
	const id = "power-loss"
	if _, err := tm.OpenSession(id, target, int64(len(content)), durChunk, sha256hex(content), false, "dev"); err != nil {
		t.Fatal(err)
	}
	temp := tm.tempPathFor(id)

	durable := make([]byte, len(content)) // what the disk would hold after a power cut
	swapSyncData(t, func(f *os.File) error {
		b, err := os.ReadFile(f.Name())
		if err != nil {
			return err
		}
		copy(durable, b)
		return nil
	})

	for _, n := range []int{0, 2, 3} {
		if err := tm.WriteChunk(id, n, durPart(content, n), sha256hex(durPart(content, n))); err != nil {
			t.Fatalf("chunk %d: %v", n, err)
		}
	}

	// Power cut: the temp file comes back as the durable image, then the agent restarts.
	if err := os.WriteFile(temp, durable, 0o600); err != nil {
		t.Fatal(err)
	}
	tm2, err := New(db, filepath.Join(dataDir, "tmp"))
	if err != nil {
		t.Fatalf("restart: %v", err)
	}
	st, err := tm2.Status(id)
	if err != nil {
		t.Fatalf("status after restart: %v", err)
	}
	if len(st.ReceivedChunks) == 0 {
		t.Fatal("no chunk is recorded as received although three were acknowledged")
	}
	for _, n := range st.ReceivedChunks {
		if !bytes.Equal(durable[n*durChunk:min((n+1)*durChunk, len(durable))], durPart(content, n)) {
			t.Fatalf("chunk %d is recorded as received but its bytes were not on disk (zeros after power loss)", n)
		}
	}

	for n := 0; n < st.TotalChunks; n++ {
		if slices.Contains(st.ReceivedChunks, n) {
			continue
		}
		if err := tm2.WriteChunk(id, n, durPart(content, n), sha256hex(durPart(content, n))); err != nil {
			t.Fatalf("resume chunk %d: %v", n, err)
		}
	}
	if _, _, err := tm2.Complete(id); err != nil {
		t.Fatalf("complete after resume: %v", err)
	}
	got, err := os.ReadFile(target)
	if err != nil || !bytes.Equal(got, content) {
		t.Fatalf("published file differs from the upload (err=%v)", err)
	}
}

// TestOpenSessionOutOfSpaceFailsEarly: when the disk cannot hold the declared size, opening fails with
// ErrInsufficientStorage and leaves no row, no temp file and no reserved quota behind.
func TestOpenSessionOutOfSpaceFailsEarly(t *testing.T) {
	tm, _, dataDir := setupManager(t)
	old := reserveSpace
	reserveSpace = func(f *os.File, size int64) error { return enospc("fallocate") }
	t.Cleanup(func() { reserveSpace = old })
	content := durContent(t, 2*durChunk)
	const id = "no-space-open"
	_, err := tm.OpenSession(id, filepath.Join(dataDir, "o.bin"), int64(len(content)), durChunk, sha256hex(content), false, "dev")
	if !errors.Is(err, ErrInsufficientStorage) {
		t.Fatalf("OpenSession on a full disk: err = %v, want ErrInsufficientStorage", err)
	}
	if _, lerr := tm.Lookup(id); !errors.Is(lerr, ErrNotFound) {
		t.Errorf("session row left behind: %v", lerr)
	}
	if _, serr := os.Stat(tm.tempPathFor(id)); !errors.Is(serr, os.ErrNotExist) {
		t.Errorf("temp file left behind: %v", serr)
	}
	reserveSpace = old
	if _, err := tm.OpenSession(id, filepath.Join(dataDir, "o.bin"), int64(len(content)), durChunk, sha256hex(content), false, "dev"); err != nil {
		t.Fatalf("open after space returned: %v", err)
	}
}

// TestChunkOutOfSpaceIsRetryable: a full disk during a chunk's write or sync is ErrInsufficientStorage, the
// chunk is not recorded, and the same session accepts the chunk once space is back and completes.
func TestChunkOutOfSpaceIsRetryable(t *testing.T) {
	for _, tc := range []struct {
		name   string
		inject func(t *testing.T) func()
	}{
		{"write", func(t *testing.T) func() {
			old := writeChunkAt
			writeChunkAt = func(f *os.File, p []byte, off int64) (int, error) { return 0, enospc("write") }
			restore := func() { writeChunkAt = old }
			t.Cleanup(restore)
			return restore
		}},
		{"sync", func(t *testing.T) func() {
			old := syncData
			syncData = func(f *os.File) error { return enospc("fdatasync") }
			restore := func() { syncData = old }
			t.Cleanup(restore)
			return restore
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tm, _, dataDir := setupManager(t)
			content := durContent(t, 2*durChunk)
			target := filepath.Join(dataDir, "o.bin")
			const id = "no-space-chunk"
			if _, err := tm.OpenSession(id, target, int64(len(content)), durChunk, sha256hex(content), false, "dev"); err != nil {
				t.Fatal(err)
			}
			repair := tc.inject(t)
			err := tm.WriteChunk(id, 0, durPart(content, 0), sha256hex(durPart(content, 0)))
			if !errors.Is(err, ErrInsufficientStorage) {
				t.Fatalf("WriteChunk on a full disk: err = %v, want ErrInsufficientStorage", err)
			}
			st, err := tm.Status(id)
			if err != nil || st.Status != "open" || len(st.ReceivedChunks) != 0 {
				t.Fatalf("session after the failed chunk = (%+v, %v), want open with no chunks", st, err)
			}
			repair()
			for n := 0; n < 2; n++ {
				if err := tm.WriteChunk(id, n, durPart(content, n), sha256hex(durPart(content, n))); err != nil {
					t.Fatalf("retry chunk %d: %v", n, err)
				}
			}
			if _, _, err := tm.Complete(id); err != nil {
				t.Fatalf("complete: %v", err)
			}
		})
	}
}

// openBatchSession opens a session of chunks full chunks and returns what a batching test needs.
func openBatchSession(t *testing.T, chunks int) (tm *Manager, id string, content []byte) {
	t.Helper()
	tm, _, dataDir := setupManager(t)
	content = durContent(t, chunks*durChunk)
	id = "batch"
	if _, err := tm.OpenSession(id, filepath.Join(dataDir, "o.bin"), int64(len(content)), durChunk, sha256hex(content), false, "dev"); err != nil {
		t.Fatal(err)
	}
	return tm, id, content
}

// countSyncs replaces the sync seam with a counter.
func countSyncs(t *testing.T) *atomic.Int64 {
	t.Helper()
	var n atomic.Int64
	swapSyncData(t, func(*os.File) error { n.Add(1); return nil })
	return &n
}

// commitConcurrently writes and commits chunks 0..count-1 from separate goroutines, all registered as writers
// first so none can be flushed before the others have joined.
func commitConcurrently(t *testing.T, tm *Manager, id string, content []byte, count int) {
	t.Helper()
	temp := tm.tempPathFor(id)
	ss := make([]*sessionSync, count)
	for n := range ss {
		ss[n] = tm.beginChunk(id, temp)
	}
	var wg sync.WaitGroup
	errs := make([]error, count)
	for n := 0; n < count; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := tm.writeChunkBytes(id, temp, durPart(content, n), int64(n)*durChunk); err != nil {
				tm.abandonChunk(ss[n])
				errs[n] = err
				return
			}
			errs[n] = tm.commitChunk(ss[n], n)
		}()
	}
	wg.Wait()
	for n, err := range errs {
		if err != nil {
			t.Fatalf("chunk %d: %v", n, err)
		}
	}
}

// TestConcurrentChunksShareOneSync: chunks written at the same time are made durable by a single sync, and all
// of them are recorded once it finishes.
func TestConcurrentChunksShareOneSync(t *testing.T) {
	tm, id, content := openBatchSession(t, 6)
	syncs := countSyncs(t)
	commitConcurrently(t, tm, id, content, 6)
	if got := syncs.Load(); got != 1 {
		t.Errorf("syncs for 6 concurrent chunks = %d, want 1", got)
	}
	if st, err := tm.Status(id); err != nil || len(st.ReceivedChunks) != 6 {
		t.Errorf("received after the batch = (%v, %v), want all 6", st, err)
	}
}

// TestBatchSyncsAtChunkLimit: a batch never grows past syncBatchChunks; the chunks beyond it get their own sync.
func TestBatchSyncsAtChunkLimit(t *testing.T) {
	total := syncBatchChunks + 2
	tm, id, content := openBatchSession(t, total)
	syncs := countSyncs(t)
	commitConcurrently(t, tm, id, content, total)
	if got := syncs.Load(); got != 2 {
		t.Errorf("syncs for %d concurrent chunks = %d, want 2 (a full batch, then the rest)", total, got)
	}
	if st, err := tm.Status(id); err != nil || len(st.ReceivedChunks) != total {
		t.Errorf("received = (%v, %v), want all %d", st, err, total)
	}
}

// TestBatchSyncsAfterDelay: a chunk whose neighbour is stuck mid-write is not held up past syncBatchMaxDelay.
func TestBatchSyncsAfterDelay(t *testing.T) {
	tm, id, content := openBatchSession(t, 2)
	syncs := countSyncs(t)
	old := syncBatchMaxDelay
	syncBatchMaxDelay = 20 * time.Millisecond
	t.Cleanup(func() { syncBatchMaxDelay = old })

	temp := tm.tempPathFor(id)
	stuck := tm.beginChunk(id, temp) // a second writer that never finishes in time
	done := tm.beginChunk(id, temp)
	if err := tm.writeChunkBytes(id, temp, durPart(content, 0), 0); err != nil {
		t.Fatal(err)
	}
	if err := tm.commitChunk(done, 0); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if syncs.Load() != 1 {
		t.Errorf("syncs = %d, want 1 from the delay timer", syncs.Load())
	}
	if has, err := tm.db.HasChunk(id, 0); err != nil || !has {
		t.Errorf("chunk 0 recorded = (%v, %v), want true", has, err)
	}
	tm.abandonChunk(stuck)
}
