package transfer

import (
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"github.com/ZaidQamhieh/rfe/agent/internal/store"
)

// countRehash counts the whole-file re-hash passes complete makes while the test runs.
func countRehash(t *testing.T) *int {
	t.Helper()
	n := 0
	old := onWholeFileRehash
	onWholeFileRehash = func() { n++ }
	t.Cleanup(func() { onWholeFileRehash = old })
	return &n
}

// Chunks that arrive in order are hashed as they land, so complete does not read the file again.
func TestH6InOrderUploadSkipsTheRehashPass(t *testing.T) {
	rehash := countRehash(t)
	tm, _, dataDir := setupManager(t)
	data := []byte("in order upload, many chunks!")
	target := filepath.Join(dataDir, "out", "a.bin")
	h6Upload(t, tm, "h6-hash-a", target, data, false, h6InOrder(data))
	if err := h6Complete(tm, h6Ops(dataDir)["jailed"], "h6-hash-a"); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if *rehash != 0 {
		t.Errorf("complete re-hashed the file %d time(s) although every chunk arrived in order", *rehash)
	}
	if string(h6Read(t, target)) != string(data) {
		t.Errorf("content differs")
	}
}

// A wrong whole-file hash is still caught on the running-hash path: the session fails and the temp goes.
func TestH6InOrderUploadStillRejectsAWrongFileHash(t *testing.T) {
	rehash := countRehash(t)
	tm, db, dataDir := setupManager(t)
	data := []byte("content whose declared hash is wrong")
	target := filepath.Join(dataDir, "out", "bad.bin")
	if _, err := tm.OpenSession("h6-hash-bad", target, int64(len(data)), 4, sha256hex([]byte("something else")), false, "dev"); err != nil {
		t.Fatal(err)
	}
	for _, n := range h6InOrder(data) {
		end := min((n+1)*4, len(data))
		part := data[n*4 : end]
		if err := tm.WriteChunk("h6-hash-bad", n, part, sha256hex(part)); err != nil {
			t.Fatal(err)
		}
	}
	err := h6Complete(tm, h6Ops(dataDir)["jailed"], "h6-hash-bad")
	if !errors.Is(err, ErrFileMismatch) {
		t.Fatalf("complete = %v, want ErrFileMismatch", err)
	}
	if *rehash != 0 {
		t.Errorf("running-hash path re-hashed %d time(s)", *rehash)
	}
	if tr, _ := db.GetTransfer("h6-hash-bad"); tr == nil || tr.Status != store.TransferFailed {
		t.Errorf("session after a hash mismatch = %+v, want failed", tr)
	}
	if _, err := tm.Lookup("h6-hash-bad"); err != nil {
		t.Fatal(err)
	}
}

// Chunks written out of order and in parallel are still verified correctly; the hash follows once the gaps fill.
func TestH6OutOfOrderUploadCompletesWithTheRightHash(t *testing.T) {
	tm, _, dataDir := setupManager(t)
	data := []byte("0123456789abcdefghijklmnopqrstuvwxyz!?")
	target := filepath.Join(dataDir, "out", "ooo.bin")
	if _, err := tm.OpenSession("h6-ooo", target, int64(len(data)), 4, sha256hex(data), false, "dev"); err != nil {
		t.Fatal(err)
	}
	chunks := h6InOrder(data)
	var wg sync.WaitGroup
	for i := len(chunks) - 1; i >= 0; i-- {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			part := data[n*4 : min((n+1)*4, len(data))]
			if err := tm.WriteChunk("h6-ooo", n, part, sha256hex(part)); err != nil {
				t.Error(err)
			}
		}(chunks[i])
	}
	wg.Wait()
	if err := h6Complete(tm, h6Ops(dataDir)["jailed"], "h6-ooo"); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if string(h6Read(t, target)) != string(data) {
		t.Errorf("content differs")
	}
}

// A session resumed by a new process has no running hash for the chunks written before the restart, so complete
// falls back to the full re-hash pass (and still verifies).
func TestH6ResumedSessionFallsBackToTheRehashPass(t *testing.T) {
	rehash := countRehash(t)
	tm, db, dataDir := setupManager(t)
	data := []byte("resumed after a restart, bytes")
	target := filepath.Join(dataDir, "out", "res.bin")
	order := h6InOrder(data)
	h6Upload(t, tm, "h6-res", target, data, false, order[:2])

	tm2, err := New(db, tm.tempDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range order[2:] {
		part := data[n*4 : min((n+1)*4, len(data))]
		if err := tm2.WriteChunk("h6-res", n, part, sha256hex(part)); err != nil {
			t.Fatal(err)
		}
	}
	if err := h6Complete(tm2, h6Ops(dataDir)["jailed"], "h6-res"); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if *rehash != 1 {
		t.Errorf("re-hash passes = %d, want 1 for a resumed session", *rehash)
	}
	if string(h6Read(t, target)) != string(data) {
		t.Errorf("content differs")
	}
}
