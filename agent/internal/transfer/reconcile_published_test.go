package transfer

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"

	"github.com/ZaidQamhieh/rfe/agent/internal/store"
)

// publishedCrashSession opens a one-chunk session, receives its chunk, then simulates a crash
// after the file became visible at target (written by hand, as publish would) with the temp file
// either removed or still present. It returns the reopened manager, db and session id.
func publishedCrashSession(t *testing.T, destContent []byte, removeTemp bool) (*Manager, *store.DB, string, string) {
	t.Helper()
	tm, db, dataDir := setupManager(t)
	content := []byte("published before the crash")
	target := filepath.Join(dataDir, "out", "file.bin")
	id := uuid.New().String()
	if _, err := tm.OpenSession(id, target, int64(len(content)), 64, sha256hex(content), false, "dev"); err != nil {
		t.Fatalf("OpenSession: %v", err)
	}
	if err := tm.WriteChunk(id, 0, content, sha256hex(content)); err != nil {
		t.Fatalf("WriteChunk: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, destContent, 0o644); err != nil {
		t.Fatal(err)
	}
	if removeTemp {
		if err := os.Remove(tm.tempPathFor(id)); err != nil {
			t.Fatal(err)
		}
	}
	// A restart: a fresh manager over the same database and temp dir reconciles open sessions.
	tm2, err := New(db, tm.tempDir)
	if err != nil {
		t.Fatalf("New after crash: %v", err)
	}
	return tm2, db, id, target
}

// A crash after publish and temp removal but before the row update must not leave the row failed:
// the destination already holds the verified bytes, so the session is completed.
func TestReconcile_PublishedTempGoneBecomesCompleted(t *testing.T) {
	_, db, id, target := publishedCrashSession(t, []byte("published before the crash"), true)
	got, err := db.GetTransfer(id)
	if err != nil || got == nil {
		t.Fatalf("GetTransfer: %v %v", got, err)
	}
	if got.Status != store.TransferCompleted {
		t.Fatalf("status = %q, want completed", got.Status)
	}
	if b, err := os.ReadFile(target); err != nil || string(b) != "published before the crash" {
		t.Fatalf("destination = %q, %v", b, err)
	}
}

// A crash after publish but before the temp was removed leaves the row open with every chunk: the
// session is completed and the leftover temp file removed, so a retried complete cannot hit CONFLICT.
func TestReconcile_PublishedTempPresentBecomesCompletedAndCleansTemp(t *testing.T) {
	tm, db, id, _ := publishedCrashSession(t, []byte("published before the crash"), false)
	got, err := db.GetTransfer(id)
	if err != nil || got == nil {
		t.Fatalf("GetTransfer: %v %v", got, err)
	}
	if got.Status != store.TransferCompleted {
		t.Fatalf("status = %q, want completed", got.Status)
	}
	if _, err := os.Stat(tm.tempPathFor(id)); !os.IsNotExist(err) {
		t.Fatalf("temp file still present: %v", err)
	}
}

// A destination with other bytes is not proof of publication: the broken session still fails.
func TestReconcile_DifferentDestinationStillFailsBrokenSession(t *testing.T) {
	_, db, id, _ := publishedCrashSession(t, []byte("someone else's file......."), true)
	got, err := db.GetTransfer(id)
	if err != nil || got == nil {
		t.Fatalf("GetTransfer: %v %v", got, err)
	}
	if got.Status != store.TransferFailed {
		t.Fatalf("status = %q, want failed", got.Status)
	}
}

// An unpublished full session with a pre-existing different destination stays open and resumable.
func TestReconcile_DifferentDestinationKeepsIntactSessionOpen(t *testing.T) {
	_, db, id, _ := publishedCrashSession(t, []byte("someone else's file......."), false)
	got, err := db.GetTransfer(id)
	if err != nil || got == nil {
		t.Fatalf("GetTransfer: %v %v", got, err)
	}
	if got.Status != store.TransferOpen {
		t.Fatalf("status = %q, want open", got.Status)
	}
}
