package transfer

// Break tests (qa2) for the transfer engine's stale and orphan sweeps and for the
// completion lease. Written by qa-break; not run by it.
//
// Run: cd agent && go test ./internal/transfer/ -run TestQA2Break -count=1 -v -timeout 300s
//
// No test here starts goroutines. Each interleaving is forced: a lease is taken
// with BeginActivity, or a call is made from inside the publisher callback of
// CompleteWithPublisher, while the completion lease is held (transfer.go:264-288).
// Sweeps use the now argument of CleanupStaleSessions, so no test depends on timing.

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// qa2BreakFile writes content to path and sets its mtime.
func qa2BreakFile(t *testing.T, path, content string, mtime time.Time) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
}

// qa2BreakOpenSession opens an overwrite=false, single-chunk session for content.
func qa2BreakOpenSession(t *testing.T, tm *Manager, id, target string, content []byte) {
	t.Helper()
	if _, err := tm.OpenSession(id, target, int64(len(content)), len(content), sha256hex(content), false, ""); err != nil {
		t.Fatalf("OpenSession(%s): %v", id, err)
	}
}

// TestQA2Break_OrphanTempOlderThanSevenDaysIsRemoved attacks QR-3 (an
// unreferenced temporary file older than seven days is removed by age-gated
// cleanup). Scenario: <tempDir>/ghost.tmp is a regular file with an mtime eight
// days old and no session row references it. CleanupStaleSessions runs now.
// Expected: ghost.tmp is gone. Label: code failure. Basis: QR-3 Accept. Confidence:
// high. A failing run would mean an abandoned temp file survives and leaks disk
// (cleanupOrphanTempsLocked, transfer.go:415-465).
func TestQA2Break_OrphanTempOlderThanSevenDaysIsRemoved(t *testing.T) {
	tm, _, _ := setupManager(t)
	ghost := filepath.Join(tm.tempDir, "ghost.tmp")
	qa2BreakFile(t, ghost, "ghost", time.Now().Add(-(TransferStaleAfter + 24*time.Hour)))

	if _, err := tm.CleanupStaleSessions(time.Now()); err != nil {
		t.Fatalf("CleanupStaleSessions: %v", err)
	}
	if _, err := os.Lstat(ghost); !os.IsNotExist(err) {
		t.Fatalf("QR-3: an unreferenced temp file older than seven days survived cleanup (lstat err=%v)", err)
	}
}

// TestQA2Break_OrphanTempSixDaysOldIsKept checks the age gate of QR-3 (removal
// applies once the file is older than seven days). Scenario: edge.tmp, unreferenced,
// has an mtime six days old. Expected: edge.tmp is kept. Label: code failure. Basis:
// QR-3 ("age-gated cleanup ... once it is older than 7 days"; the gate itself is an
// inference). Confidence: medium.
func TestQA2Break_OrphanTempSixDaysOldIsKept(t *testing.T) {
	tm, _, _ := setupManager(t)
	edge := filepath.Join(tm.tempDir, "edge.tmp")
	qa2BreakFile(t, edge, "edge", time.Now().Add(-6*24*time.Hour))

	if _, err := tm.CleanupStaleSessions(time.Now()); err != nil {
		t.Fatalf("CleanupStaleSessions: %v", err)
	}
	if _, err := os.Lstat(edge); err != nil {
		t.Fatalf("QR-3 age gate: a six-day-old unreferenced temp file was removed (lstat err=%v)", err)
	}
}

// TestQA2Break_OrphanSweepNeverTouchesSymlinkTarget attacks the invariant that
// cleanup never deletes what a link points to. Scenario: <tempDir>/link.tmp is a
// symlink to an old regular file outside the temp directory. The sweep runs now.
// Expected: the target still exists with its bytes. The link itself may be left
// or removed; the requirement does not say which. Label: code failure. Basis:
// transfer.go:412-414 ("symlinks and other file types are left alone"; cleanup
// removes only regular files). Confidence: high. Skipped on Windows.
func TestQA2Break_OrphanSweepNeverTouchesSymlinkTarget(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink fixture")
	}
	tm, _, dataDir := setupManager(t)
	victim := filepath.Join(dataDir, "victim.bin")
	qa2BreakFile(t, victim, "victim", time.Now().Add(-(TransferStaleAfter + 24*time.Hour)))
	if err := os.Symlink(victim, filepath.Join(tm.tempDir, "link.tmp")); err != nil {
		t.Fatalf("os.Symlink: %v", err)
	}

	if _, err := tm.CleanupStaleSessions(time.Now()); err != nil {
		t.Fatalf("CleanupStaleSessions: %v", err)
	}
	got, err := os.ReadFile(victim)
	if err != nil || string(got) != "victim" {
		t.Fatalf("cleanup changed or removed the symlink target %s (read %q, err %v); transfer.go:412-414 says links are left alone", victim, got, err)
	}
}

// TestQA2Break_OrphanSweepKeepsTempOfLiveSession attacks the invariant that a
// temp file referenced by a live session row is never removed as an orphan.
// Scenario: a session is opened and then touched by Status, so its row is fresh.
// Its temp file's mtime is set eight days old. The sweep runs now. Expected: the
// session still exists and its temp file still exists. Label: code failure. Basis:
// transfer.go:436-441 (a temp referenced by a row is skipped) and QR-3 (a session
// with recent activity is not eligible). Confidence: high.
func TestQA2Break_OrphanSweepKeepsTempOfLiveSession(t *testing.T) {
	tm, _, dataDir := setupManager(t)
	content := []byte("live")
	id := "qa2-live"
	qa2BreakOpenSession(t, tm, id, filepath.Join(dataDir, "live.bin"), content)
	// Status touches the row so the session is fresh (transfer.go:525-545).
	if _, err := tm.Status(id); err != nil {
		t.Fatalf("Status: %v", err)
	}
	temp := filepath.Join(tm.tempDir, id+".tmp")
	old := time.Now().Add(-(TransferStaleAfter + 24*time.Hour))
	if err := os.Chtimes(temp, old, old); err != nil {
		t.Fatal(err)
	}

	if _, err := tm.CleanupStaleSessions(time.Now()); err != nil {
		t.Fatalf("CleanupStaleSessions: %v", err)
	}
	if _, err := tm.Lookup(id); err != nil {
		t.Fatalf("a live session was removed by the sweep: %v", err)
	}
	if _, err := os.Stat(temp); err != nil {
		t.Fatalf("the temp file of a live session was removed as an orphan: %v", err)
	}
}

// TestQA2Break_StaleSweepKeepsTempWhileLeaseHeld attacks QR-3 and the lease rule:
// an upload with a chunk in flight must not be swept. The lease is taken with
// BeginActivity, as an HTTP chunk handler does (transfer.go:218-220). The sweep
// then runs with a clock eight days ahead, so the row is stale by time alone.
// Expected: the session and temp file survive the sweep. After the lease is
// released, a chunk is written and the session completes with the exact bytes.
// Label: code failure. Basis: transfer.go:218-220 (documented: the sweeper must not
// remove a temp file while an upload is active) and transfer.go:363 (sweep skips
// active sessions). Confidence: high.
func TestQA2Break_StaleSweepKeepsTempWhileLeaseHeld(t *testing.T) {
	tm, _, dataDir := setupManager(t)
	content := []byte("lease-bytes")
	id := "qa2-lease"
	target := filepath.Join(dataDir, "lease.bin")
	qa2BreakOpenSession(t, tm, id, target, content)

	release, err := tm.BeginActivity(id)
	if err != nil {
		t.Fatalf("BeginActivity: %v", err)
	}
	if _, err := tm.CleanupStaleSessions(time.Now().Add(TransferStaleAfter + 24*time.Hour)); err != nil {
		release()
		t.Fatalf("CleanupStaleSessions: %v", err)
	}
	if _, err := tm.Lookup(id); err != nil {
		release()
		t.Fatalf("a session with an active lease was removed by the stale sweep: %v", err)
	}
	temp := filepath.Join(tm.tempDir, id+".tmp")
	if _, err := os.Stat(temp); err != nil {
		release()
		t.Fatalf("the temp file of an active upload was removed by the sweep: %v", err)
	}
	release()

	if err := tm.WriteChunk(id, 0, content, sha256hex(content)); err != nil {
		t.Fatalf("WriteChunk after the sweep: %v", err)
	}
	_, finalPath, err := tm.Complete(id)
	if err != nil {
		t.Fatalf("Complete after the sweep: %v", err)
	}
	got, err := os.ReadFile(finalPath)
	if err != nil || !bytes.Equal(got, content) {
		t.Fatalf("published bytes differ after the sweep: read %q, err %v", got, err)
	}
}

// TestQA2Break_SweepDuringCompletionKeepsPublishedBytes attacks the completion
// lease: while publish runs, the stale sweep must neither remove the session nor
// the temp file, and the publisher must read the verified bytes. The sweep runs
// inside the publisher callback, so the completion lease is held (beginCompletion
// sets active, transfer.go:287). The clock is eight days ahead. Expected: no sweep
// error, the session is still present, and the publisher reads the exact content.
// Label: code failure. Basis: transfer.go:264-266 (completion prevents other
// activity until publish is done) and transfer.go:363 (active sessions are not
// swept). Confidence: high.
func TestQA2Break_SweepDuringCompletionKeepsPublishedBytes(t *testing.T) {
	tm, _, dataDir := setupManager(t)
	content := []byte("completion-bytes")
	id := "qa2-sweep-complete"
	qa2BreakOpenSession(t, tm, id, filepath.Join(dataDir, "sweep-complete.bin"), content)
	if err := tm.WriteChunk(id, 0, content, sha256hex(content)); err != nil {
		t.Fatalf("WriteChunk: %v", err)
	}

	var sweepErr, lookupErr, readErr error
	var got []byte
	publish := func(path string, r io.Reader, overwrite bool) (os.FileInfo, error) {
		_, sweepErr = tm.CleanupStaleSessions(time.Now().Add(TransferStaleAfter + 24*time.Hour))
		_, lookupErr = tm.Lookup(id)
		got, readErr = io.ReadAll(r)
		return nil, nil
	}
	if _, _, err := tm.CompleteWithPublisher(id, publish); err != nil {
		t.Fatalf("CompleteWithPublisher: %v", err)
	}
	if sweepErr != nil {
		t.Fatalf("stale sweep during completion: %v", sweepErr)
	}
	if lookupErr != nil {
		t.Fatalf("the stale sweep removed a session that a completion held: %v", lookupErr)
	}
	if readErr != nil || !bytes.Equal(got, content) {
		t.Fatalf("the publisher read %q (err %v) during completion, want the verified %q", got, readErr, content)
	}
}

// TestQA2Break_DeleteSessionDuringCompletionRefused attacks FR-28 (deleting an
// upload session while a completion holds it returns TRANSFER_ACTIVE and removes
// nothing). DeleteSession runs inside the publisher callback, so the completion
// lease is held. Expected: DeleteSession returns ErrSessionActive, the temp file
// still exists, and the publisher reads the verified bytes. Label: code failure.
// Basis: FR-28 Accept ("a completion in progress holds the session"). Confidence: high.
func TestQA2Break_DeleteSessionDuringCompletionRefused(t *testing.T) {
	tm, _, dataDir := setupManager(t)
	content := []byte("delete-during-complete")
	id := "qa2-del-complete"
	qa2BreakOpenSession(t, tm, id, filepath.Join(dataDir, "del-complete.bin"), content)
	if err := tm.WriteChunk(id, 0, content, sha256hex(content)); err != nil {
		t.Fatalf("WriteChunk: %v", err)
	}
	temp := filepath.Join(tm.tempDir, id+".tmp")

	var delErr, tempErr, readErr error
	var got []byte
	publish := func(path string, r io.Reader, overwrite bool) (os.FileInfo, error) {
		delErr = tm.DeleteSession(id)
		_, tempErr = os.Stat(temp)
		got, readErr = io.ReadAll(r)
		return nil, nil
	}
	if _, _, err := tm.CompleteWithPublisher(id, publish); err != nil {
		t.Fatalf("CompleteWithPublisher: %v", err)
	}
	if !errors.Is(delErr, ErrSessionActive) {
		t.Fatalf("FR-28: DeleteSession during a completion returned %v, want ErrSessionActive (409 TRANSFER_ACTIVE)", delErr)
	}
	if tempErr != nil {
		t.Fatalf("FR-28: a refused delete removed the temp file of the completing session: %v", tempErr)
	}
	if readErr != nil || !bytes.Equal(got, content) {
		t.Fatalf("the publisher read %q (err %v) after a refused delete, want %q", got, readErr, content)
	}
}

// TestQA2Break_ChunkWriteDuringCompletionRefused attacks FR-32 (a chunk PUT while a
// completion holds the session returns TRANSFER_ACTIVE and does not change the
// session). Chunk 0 is already received, so the idempotent path would return nil
// if the lease check were skipped. The chunk is sent with different bytes of the
// same length and its own correct hash, inside the publisher callback. Expected:
// WriteChunk returns ErrSessionActive, and the publisher reads the originally
// verified bytes. Label: code failure. Basis: FR-32 Accept and transfer.go:264-266.
// Confidence: high.
func TestQA2Break_ChunkWriteDuringCompletionRefused(t *testing.T) {
	tm, _, dataDir := setupManager(t)
	content := []byte("chunk-during-complete")
	other := bytes.Repeat([]byte("X"), len(content))
	id := "qa2-chunk-complete"
	qa2BreakOpenSession(t, tm, id, filepath.Join(dataDir, "chunk-complete.bin"), content)
	if err := tm.WriteChunk(id, 0, content, sha256hex(content)); err != nil {
		t.Fatalf("WriteChunk: %v", err)
	}

	var writeErr, readErr error
	var got []byte
	publish := func(path string, r io.Reader, overwrite bool) (os.FileInfo, error) {
		writeErr = tm.WriteChunk(id, 0, other, sha256hex(other))
		got, readErr = io.ReadAll(r)
		return nil, nil
	}
	if _, _, err := tm.CompleteWithPublisher(id, publish); err != nil {
		t.Fatalf("CompleteWithPublisher: %v", err)
	}
	if !errors.Is(writeErr, ErrSessionActive) {
		t.Fatalf("FR-32: a chunk PUT during a completion returned %v, want ErrSessionActive (409 TRANSFER_ACTIVE)", writeErr)
	}
	if readErr != nil || !bytes.Equal(got, content) {
		t.Fatalf("the publisher read %q (err %v) after a refused chunk, want the verified %q", got, readErr, content)
	}
}
