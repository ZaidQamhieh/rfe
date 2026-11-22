package transfer

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

const idemKey = "0123456789abcdef0123456789abcdef"

func idemOpen(t *testing.T, tm *Manager, id, target, device, key string) (sessionID string, existed bool, err error) {
	t.Helper()
	content := []byte("hello world")
	s, existed, err := tm.OpenSessionWithKey(id, target, int64(len(content)), 64, sha256hex(content), false, device, key, nil)
	if s != nil {
		sessionID = s.ID
	}
	return sessionID, existed, err
}

// The same key from the same device returns the one session, without a second row or temp file.
func TestOpenSessionWithKeyRepeatReturnsSameSession(t *testing.T) {
	tm, db, dataDir := setupManager(t)
	target := filepath.Join(dataDir, "out", "f.bin")
	first, existed, err := idemOpen(t, tm, "id-1", target, "dev1", idemKey)
	if err != nil || existed {
		t.Fatalf("first open: id=%q existed=%v err=%v", first, existed, err)
	}
	second, existed, err := idemOpen(t, tm, "id-2", target, "dev1", idemKey)
	if err != nil || !existed || second != first {
		t.Fatalf("repeat open: id=%q existed=%v err=%v; want %q existed", second, existed, err, first)
	}
	if open, _ := db.ListOpenTransfers(); len(open) != 1 {
		t.Fatalf("open sessions = %d, want 1", len(open))
	}
	if got, _ := db.GetTransfer("id-2"); got != nil {
		t.Fatalf("the repeat created row id-2: %+v", got)
	}
}

// Losing the acknowledgement more times than the per-device quota allows must never exhaust the quota.
func TestOpenSessionWithKeyLostAcksDoNotLeakQuota(t *testing.T) {
	tm, _, dataDir := setupManager(t)
	target := filepath.Join(dataDir, "out", "f.bin")
	for i := 0; i < MaxActiveTransfersPerDevice+2; i++ {
		id := "id-" + string(rune('a'+i))
		if _, _, err := idemOpen(t, tm, id, target, "dev1", idemKey); err != nil {
			t.Fatalf("open %d with the same key: %v", i, err)
		}
	}
	if _, _, err := idemOpen(t, tm, "id-new", target, "dev1", "ffffffffffffffffffffffffffffffff"); err != nil {
		t.Fatalf("a different key must still get a slot: %v", err)
	}
}

func TestOpenSessionWithKeyDifferentKeysAndDevicesAreIndependent(t *testing.T) {
	tm, _, dataDir := setupManager(t)
	target := filepath.Join(dataDir, "out", "f.bin")
	a, _, err := idemOpen(t, tm, "id-a", target, "dev1", idemKey)
	if err != nil {
		t.Fatal(err)
	}
	b, existed, err := idemOpen(t, tm, "id-b", target, "dev1", "ffffffffffffffffffffffffffffffff")
	if err != nil || existed || b == a {
		t.Fatalf("different key: id=%q existed=%v err=%v", b, existed, err)
	}
	c, existed, err := idemOpen(t, tm, "id-c", target, "dev2", idemKey)
	if err != nil || existed || c == a {
		t.Fatalf("other device, same key: id=%q existed=%v err=%v", c, existed, err)
	}
}

// A key whose session finished, or whose repeat describes a different upload, is refused rather than answered
// with a session for other bytes.
func TestOpenSessionWithKeyRefusesUsedAndMismatchedKeys(t *testing.T) {
	tm, db, dataDir := setupManager(t)
	target := filepath.Join(dataDir, "out", "f.bin")
	id, _, err := idemOpen(t, tm, "id-1", target, "dev1", idemKey)
	if err != nil {
		t.Fatal(err)
	}
	other := []byte("different")
	if _, _, err := tm.OpenSessionWithKey("id-x", target, int64(len(other)), 64, sha256hex(other), false, "dev1", idemKey, nil); !errors.Is(err, ErrIdempotencyKeyUsed) {
		t.Fatalf("mismatched repeat err = %v, want ErrIdempotencyKeyUsed", err)
	}
	if err := db.SetTransferStatus(id, "failed"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := idemOpen(t, tm, "id-y", target, "dev1", idemKey); !errors.Is(err, ErrIdempotencyKeyUsed) {
		t.Fatalf("repeat on a failed session err = %v, want ErrIdempotencyKeyUsed", err)
	}
}

// A repeat counts as activity, so a client that keeps retrying keeps its session from the early expiry.
func TestOpenSessionWithKeyRepeatRefreshesActivity(t *testing.T) {
	tm, db, dataDir := setupManager(t)
	target := filepath.Join(dataDir, "out", "f.bin")
	id, _, err := idemOpen(t, tm, "id-1", target, "dev1", idemKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.TouchOpenTransfer(id, 1000); err != nil {
		t.Fatal(err)
	}
	if _, _, err := idemOpen(t, tm, "id-2", target, "dev1", idemKey); err != nil {
		t.Fatal(err)
	}
	got, _ := db.GetTransfer(id)
	if got == nil || got.UpdatedAt <= 1000 {
		t.Fatalf("repeat did not refresh updated_at: %+v", got)
	}
}

// A session that never received a chunk expires after EmptyTransferStaleAfter (1 h) of inactivity, one that
// holds a chunk keeps the week-long window.
func TestEmptySessionExpiresAfterOneHour(t *testing.T) {
	tm, db, dataDir := setupManager(t)
	target := filepath.Join(dataDir, "out", "f.bin")
	emptyID, _, err := idemOpen(t, tm, "empty", target, "dev1", idemKey)
	if err != nil {
		t.Fatal(err)
	}
	usedID, _, err := idemOpen(t, tm, "used", target, "dev1", "ffffffffffffffffffffffffffffffff")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.MarkChunkReceived(usedID, 0); err != nil {
		t.Fatal(err)
	}
	base := time.Now()
	for _, id := range []string{emptyID, usedID} {
		if err := db.TouchOpenTransfer(id, base.Unix()); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := tm.CleanupStaleSessions(base.Add(EmptyTransferStaleAfter - time.Minute)); err != nil {
		t.Fatal(err)
	}
	if got, _ := db.GetTransfer(emptyID); got == nil {
		t.Fatal("empty session expired before one hour")
	}
	if _, err := tm.CleanupStaleSessions(base.Add(EmptyTransferStaleAfter + time.Minute)); err != nil {
		t.Fatal(err)
	}
	if got, _ := db.GetTransfer(emptyID); got != nil {
		t.Fatalf("empty session still present after one hour: %+v", got)
	}
	if got, _ := db.GetTransfer(usedID); got == nil {
		t.Fatal("a session holding a chunk must not take the one-hour expiry")
	}
}
