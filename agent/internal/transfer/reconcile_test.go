package transfer

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ZaidQamhieh/rfe/agent/internal/store"
	"github.com/google/uuid"
)

// openWithChunk opens an 8-byte, 2-chunk session and uploads chunk 1 (bytes 4..8), so the temp file
// must be at least 8 bytes long for the database's record of received chunks to be true.
func openWithChunk(t *testing.T, tm *Manager, dataDir string) *store.Transfer {
	t.Helper()
	payload := []byte("abcdefgh")
	id := uuid.New().String()
	sess, err := tm.OpenSession(id, filepath.Join(dataDir, "out", id+".bin"), int64(len(payload)), 4, sha256hex(payload), false, "")
	if err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	if err := tm.WriteChunk(id, 1, payload[4:], sha256hex(payload[4:])); err != nil {
		t.Fatalf("WriteChunk: %v", err)
	}
	return sess
}

func statusOf(t *testing.T, db *store.DB, id string) string {
	t.Helper()
	tr, err := db.GetTransfer(id)
	if err != nil || tr == nil {
		t.Fatalf("GetTransfer(%s) = %+v, %v", id, tr, err)
	}
	return tr.Status
}

// A temp file that was truncated below the end of a received chunk makes the session unresumable, so
// starting the manager must fail it instead of letting the phone skip the chunk.
func TestNewResetsSessionWithShortTempFile(t *testing.T) {
	tm, db, dataDir := setupManager(t)
	sess := openWithChunk(t, tm, dataDir)
	if err := os.Truncate(sess.TempPath, 6); err != nil {
		t.Fatal(err)
	}
	if _, err := New(db, filepath.Dir(sess.TempPath)); err != nil {
		t.Fatalf("New: %v", err)
	}
	if got := statusOf(t, db, sess.ID); got != store.TransferFailed {
		t.Fatalf("status after restart = %q, want %q", got, store.TransferFailed)
	}
}

// A session whose temp file vanished (a restored backup, a cleaned temp dir) is reset the same way.
func TestNewResetsSessionWithMissingTempFile(t *testing.T) {
	tm, db, dataDir := setupManager(t)
	sess := openWithChunk(t, tm, dataDir)
	if err := os.Remove(sess.TempPath); err != nil {
		t.Fatal(err)
	}
	if _, err := New(db, filepath.Dir(sess.TempPath)); err != nil {
		t.Fatalf("New: %v", err)
	}
	if got := statusOf(t, db, sess.ID); got != store.TransferFailed {
		t.Fatalf("status after restart = %q, want %q", got, store.TransferFailed)
	}
}

// A healthy session (temp file at full length) must survive a restart untouched and stay resumable.
func TestNewKeepsHealthySession(t *testing.T) {
	tm, db, dataDir := setupManager(t)
	sess := openWithChunk(t, tm, dataDir)
	tm2, err := New(db, filepath.Dir(sess.TempPath))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got := statusOf(t, db, sess.ID); got != store.TransferOpen {
		t.Fatalf("healthy session status = %q, want %q", got, store.TransferOpen)
	}
	if _, err := tm2.Status(sess.ID); err != nil {
		t.Fatalf("Status of healthy session: %v", err)
	}
}
