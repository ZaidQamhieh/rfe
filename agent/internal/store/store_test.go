package store

import (
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestListAndRevokeDevices(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	if err := db.CreateDevice("id-1", "phone-a", "tok-a"); err != nil {
		t.Fatalf("create a: %v", err)
	}
	if err := db.CreateDevice("id-2", "phone-b", "tok-b"); err != nil {
		t.Fatalf("create b: %v", err)
	}

	list, err := db.ListDevices()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 devices, got %d", len(list))
	}

	if err := db.RevokeDevice("id-1"); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	// A revoked device's token no longer resolves to a usable device.
	d, err := db.DeviceByToken("tok-a")
	if err != nil {
		t.Fatalf("by token: %v", err)
	}
	if d == nil || !d.Revoked {
		t.Fatalf("expected device present and revoked, got %+v", d)
	}
}

func TestSetDeviceReadOnly(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	if err := db.CreateDevice("id-ro", "phone", "tok-ro"); err != nil {
		t.Fatalf("create: %v", err)
	}

	// New devices default to read-write.
	d, err := db.DeviceByToken("tok-ro")
	if err != nil || d == nil {
		t.Fatalf("by token: %v (d=%+v)", err, d)
	}
	if d.ReadOnly {
		t.Fatal("new device should default to read-write")
	}

	// Flip to read-only and confirm it round-trips through every read path.
	if err := db.SetDeviceReadOnly("id-ro", true); err != nil {
		t.Fatalf("set read-only: %v", err)
	}
	d, _ = db.DeviceByToken("tok-ro")
	if !d.ReadOnly {
		t.Fatal("DeviceByToken: expected read-only")
	}
	d, _ = db.GetDeviceByID("id-ro")
	if !d.ReadOnly {
		t.Fatal("GetDeviceByID: expected read-only")
	}
	list, _ := db.ListDevices()
	if len(list) != 1 || !list[0].ReadOnly {
		t.Fatalf("ListDevices: expected one read-only device, got %+v", list)
	}

	// Clearing it returns to read-write.
	if err := db.SetDeviceReadOnly("id-ro", false); err != nil {
		t.Fatalf("clear read-only: %v", err)
	}
	d, _ = db.DeviceByToken("tok-ro")
	if d.ReadOnly {
		t.Fatal("expected read-write after clearing")
	}
}

func TestUpsertDeviceDedupesByClientID(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	// First pairing of a phone with a stable client id.
	id1, err := db.UpsertDevice("android-abc", "Mobile App", "tok-1", "", false)
	if err != nil {
		t.Fatalf("upsert 1: %v", err)
	}
	// Simulate a revoke, then a re-pair of the SAME phone (e.g. after the user
	// cleared app data). It must reuse the same row, not create a second one.
	if err := db.RevokeDevice(id1); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	id2, err := db.UpsertDevice("android-abc", "Mobile App", "tok-2", "", false)
	if err != nil {
		t.Fatalf("upsert 2: %v", err)
	}
	if id2 != id1 {
		t.Fatalf("re-pair should reuse device id %q, got %q", id1, id2)
	}

	list, err := db.ListDevices()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 device after re-pair, got %d", len(list))
	}
	if list[0].Revoked {
		t.Fatalf("re-pair should clear the revoked flag")
	}
	// The new token works; the old one no longer resolves to this device.
	if d, _ := db.DeviceByToken("tok-2"); d == nil || d.ID != id1 {
		t.Fatalf("new token should resolve to the reused device")
	}

	// A different phone (distinct client id) still gets its own row.
	if _, err := db.UpsertDevice("android-xyz", "Mobile App", "tok-3", "", false); err != nil {
		t.Fatalf("upsert other: %v", err)
	}
	if list, _ := db.ListDevices(); len(list) != 2 {
		t.Fatalf("a distinct phone should add a row, got %d", len(list))
	}

	// Empty client id (legacy/non-Android) never dedups — each call is new.
	a, _ := db.UpsertDevice("", "Legacy", "tok-4", "", false)
	b, _ := db.UpsertDevice("", "Legacy", "tok-5", "", false)
	if a == b {
		t.Fatalf("empty client id must not dedup")
	}
}

func TestPairingCodeLifecycle(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	// A valid code can be consumed exactly once.
	if err := db.CreatePairingCode("ABC123", time.Now().Add(time.Hour), "", false); err != nil {
		t.Fatalf("create: %v", err)
	}
	if !db.ConsumePairingCode("ABC123").Valid {
		t.Fatalf("expected first consume to succeed")
	}
	if db.ConsumePairingCode("ABC123").Valid {
		t.Fatalf("expected second consume to fail (single-use)")
	}

	// An expired code is rejected.
	if err := db.CreatePairingCode("OLD999", time.Now().Add(-time.Minute), "", false); err != nil {
		t.Fatalf("create expired: %v", err)
	}
	if db.ConsumePairingCode("OLD999").Valid {
		t.Fatalf("expected expired code to be rejected")
	}

	// An unknown code is rejected.
	if db.ConsumePairingCode("NOPE").Valid {
		t.Fatalf("expected unknown code to be rejected")
	}

	// A guest code carries its jailRoot/readOnly defaults through to Consume.
	if err := db.CreatePairingCode("GUEST01", time.Now().Add(time.Hour), "/home/pc/Shared", true); err != nil {
		t.Fatalf("create guest: %v", err)
	}
	info := db.ConsumePairingCode("GUEST01")
	if !info.Valid || info.JailRoot != "/home/pc/Shared" || !info.ReadOnly {
		t.Fatalf("expected guest defaults to round-trip, got %+v", info)
	}
}

func TestFilePermissions_MigrationAndDefaults(t *testing.T) {
	dir := t.TempDir()
	legacy, err := sql.Open("sqlite", filepath.Join(dir, "agent.db"))
	if err != nil {
		t.Fatalf("open legacy db: %v", err)
	}
	_, err = legacy.Exec(`CREATE TABLE devices (
id TEXT PRIMARY KEY, label TEXT NOT NULL, token_hash TEXT NOT NULL UNIQUE,
client_id TEXT NOT NULL DEFAULT '',
created INTEGER NOT NULL, last_seen INTEGER NOT NULL, revoked INTEGER NOT NULL DEFAULT 0,
last_address TEXT NOT NULL DEFAULT '', last_version TEXT NOT NULL DEFAULT '',
jail_root TEXT NOT NULL DEFAULT '', read_only INTEGER NOT NULL DEFAULT 0,
public_key TEXT NOT NULL DEFAULT '', via_login INTEGER NOT NULL DEFAULT 0,
username TEXT NOT NULL DEFAULT '', view_apps INTEGER NOT NULL DEFAULT 0,
launch_apps INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE pairing_codes (code TEXT PRIMARY KEY, expires INTEGER NOT NULL);
INSERT INTO devices (id,label,token_hash,created,last_seen,read_only) VALUES
('legacy-rw','legacy rw','hash-rw',1,1,0),
('legacy-ro','legacy ro','hash-ro',1,1,1);
PRAGMA user_version=5;`)
	if err != nil {
		t.Fatalf("seed legacy db: %v", err)
	}
	if err := legacy.Close(); err != nil {
		t.Fatalf("close legacy db: %v", err)
	}

	db, err := Open(dir)
	if err != nil {
		t.Fatalf("upgrade db: %v", err)
	}
	defer db.Close()
	for id, wantWrite := range map[string]bool{"legacy-rw": true, "legacy-ro": false} {
		d, err := db.GetDeviceByID(id)
		if err != nil || d == nil {
			t.Fatalf("get %s: (%+v, %v)", id, d, err)
		}
		if !d.CanBrowse || !d.CanDownload || d.CanUpload != wantWrite || d.CanModify != wantWrite || d.CanDelete != wantWrite || !d.CanShare {
			t.Fatalf("migrated %s permissions = %+v, want legacy access preserved", id, d)
		}
	}

	pairedID, err := db.UpsertDevice("new-paired", "phone", "pair-token", "", false)
	if err != nil {
		t.Fatalf("create paired device: %v", err)
	}
	paired, _ := db.GetDeviceByID(pairedID)
	if paired == nil || !paired.CanBrowse || paired.CanDownload || paired.CanUpload || paired.CanModify || paired.CanDelete || paired.CanShare {
		t.Fatalf("new paired device permissions = %+v, want browse-only", paired)
	}
	if err := db.SetDeviceFilePermissions(pairedID, false, true, false, true, false, true); err != nil {
		t.Fatalf("customize paired grants: %v", err)
	}
	replay, err := db.db.Begin()
	if err != nil {
		t.Fatalf("begin replayed migration: %v", err)
	}
	if err := migrateDeviceFilePermissions(replay); err != nil {
		t.Fatalf("replay migration: %v", err)
	}
	if err := replay.Commit(); err != nil {
		t.Fatalf("commit replayed migration: %v", err)
	}
	paired, _ = db.GetDeviceByID(pairedID)
	if paired == nil || paired.CanBrowse || !paired.CanDownload || paired.CanUpload || !paired.CanModify || paired.CanDelete || !paired.CanShare {
		t.Fatalf("replayed migration overwrote customized grants: %+v", paired)
	}

	ownerID, err := db.UpsertDevice("new-owner", "owner", "owner-token", "", true)
	if err != nil {
		t.Fatalf("create owner device: %v", err)
	}
	owner, _ := db.GetDeviceByID(ownerID)
	if owner == nil || !owner.ViaLogin || !owner.CanBrowse || !owner.CanDownload || !owner.CanUpload || !owner.CanModify || !owner.CanDelete || !owner.CanShare {
		t.Fatalf("owner device permissions = %+v, want all grants", owner)
	}
	downgradedID, err := db.UpsertDevice("new-owner", "phone", "owner-repair-token", "", false)
	if err != nil || downgradedID != ownerID {
		t.Fatalf("re-pair owner device: id=%q err=%v", downgradedID, err)
	}
	downgraded, _ := db.GetDeviceByID(ownerID)
	if downgraded == nil || downgraded.ViaLogin || !downgraded.CanBrowse || downgraded.CanDownload || downgraded.CanUpload || downgraded.CanModify || downgraded.CanDelete || downgraded.CanShare {
		t.Fatalf("owner-to-paired transition permissions = %+v, want browse-only", downgraded)
	}
}

func TestSetDeviceFilePermissionsAndRevokesShares(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	if err := db.CreateDevice("device", "phone", "token"); err != nil {
		t.Fatalf("create device: %v", err)
	}
	if err := db.CreateShareToken("active-share", "/tmp/file", "device", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("create share: %v", err)
	}
	if err := db.SetDeviceFilePermissions("device", true, false, true, false, true, false); err != nil {
		t.Fatalf("set file permissions: %v", err)
	}
	d, err := db.GetDeviceByID("device")
	if err != nil || d == nil || !d.CanBrowse || d.CanDownload || !d.CanUpload || d.CanModify || !d.CanDelete || d.CanShare {
		t.Fatalf("updated permissions = (%+v, %v)", d, err)
	}
	if token, err := db.GetShareToken("active-share"); err != nil || token != nil {
		t.Fatalf("revoked grant must remove active share: (%+v, %v)", token, err)
	}
}

func TestResolveDeviceIDByPrefix(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	if err := db.CreateDevice("9789abcd-1111", "phone-a", "tok-a"); err != nil {
		t.Fatalf("create a: %v", err)
	}
	if err := db.CreateDevice("9789ffff-2222", "phone-b", "tok-b"); err != nil {
		t.Fatalf("create b: %v", err)
	}
	if err := db.CreateDevice("0000eeee-3333", "phone-c", "tok-c"); err != nil {
		t.Fatalf("create c: %v", err)
	}

	// Unique prefix resolves.
	if got, err := db.ResolveDeviceID("0000"); err != nil || got != "0000eeee-3333" {
		t.Fatalf("unique prefix: got %q err %v", got, err)
	}
	// Exact id resolves even when it's also a prefix of itself.
	if got, err := db.ResolveDeviceID("9789abcd-1111"); err != nil || got != "9789abcd-1111" {
		t.Fatalf("exact id: got %q err %v", got, err)
	}
	// Ambiguous prefix errors.
	if _, err := db.ResolveDeviceID("9789"); err == nil {
		t.Fatalf("expected ambiguous prefix to error")
	}
	// No match errors.
	if _, err := db.ResolveDeviceID("zzzz"); err == nil {
		t.Fatalf("expected no-match to error")
	}
}

func TestDeleteDeviceRemovesRow(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	if err := db.CreateDevice("id-1", "phone-a", "tok-a"); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := db.DeleteDevice("id-1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	list, err := db.ListDevices()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected device removed, got %d rows", len(list))
	}
}

// TestTouchDeviceRecordsAddressAndVersion verifies TouchDevice persists the
// caller's last network address and reported app version, that ListDevices
// and DeviceByToken round-trip them, and that pre-existing rows (created
// before these columns existed in spirit) default to "".
func TestTouchDeviceRecordsAddressAndVersion(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	if err := db.CreateDevice("id-1", "phone-a", "tok-a"); err != nil {
		t.Fatalf("create: %v", err)
	}

	// Freshly created row defaults to "" for both new columns.
	d, err := db.DeviceByToken("tok-a")
	if err != nil {
		t.Fatalf("by token: %v", err)
	}
	if d == nil || d.LastAddress != "" || d.LastVersion != "" {
		t.Fatalf("expected empty defaults, got %+v", d)
	}

	// TouchDevice records address + version.
	if err := db.TouchDevice("id-1", "192.168.1.42", "1.10.0+18"); err != nil {
		t.Fatalf("touch: %v", err)
	}

	// Round-trips via DeviceByToken.
	d, err = db.DeviceByToken("tok-a")
	if err != nil {
		t.Fatalf("by token after touch: %v", err)
	}
	if d == nil || d.LastAddress != "192.168.1.42" || d.LastVersion != "1.10.0+18" {
		t.Fatalf("expected address/version recorded, got %+v", d)
	}

	// Round-trips via ListDevices.
	list, err := db.ListDevices()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 || list[0].LastAddress != "192.168.1.42" || list[0].LastVersion != "1.10.0+18" {
		t.Fatalf("expected address/version in list, got %+v", list)
	}

	// A subsequent touch with an empty version overwrites the previous one.
	// Clear the debounce window (PR-41) so this second touch actually persists
	// rather than being coalesced within touchInterval.
	db.touchMu.Lock()
	db.lastTouch = make(map[string]time.Time)
	db.touchMu.Unlock()
	if err := db.TouchDevice("id-1", "100.64.0.5", ""); err != nil {
		t.Fatalf("touch 2: %v", err)
	}
	d, err = db.DeviceByToken("tok-a")
	if err != nil {
		t.Fatalf("by token after touch 2: %v", err)
	}
	if d == nil || d.LastAddress != "100.64.0.5" || d.LastVersion != "" {
		t.Fatalf("expected updated address and cleared version, got %+v", d)
	}
}

// TestConsumeShareTokenLifecycle verifies a share token can be consumed
// exactly once (single-use), that expired tokens are rejected, and that
// unknown tokens are rejected.
func TestConsumeShareTokenLifecycle(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	if err := db.CreateShareToken("hash-1", "/srv/file.txt", "", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("create: %v", err)
	}

	path, ok, err := db.ConsumeShareToken("hash-1")
	if err != nil {
		t.Fatalf("consume: %v", err)
	}
	if !ok || path != "/srv/file.txt" {
		t.Fatalf("expected first consume to succeed with path, got ok=%v path=%q", ok, path)
	}

	// Single-use: a second consume of the same hash fails.
	_, ok, err = db.ConsumeShareToken("hash-1")
	if err != nil {
		t.Fatalf("consume 2: %v", err)
	}
	if ok {
		t.Fatal("expected second consume to fail (single-use)")
	}

	// An expired token is rejected (and removed).
	if err := db.CreateShareToken("hash-old", "/srv/old.txt", "", time.Now().Add(-time.Minute)); err != nil {
		t.Fatalf("create expired: %v", err)
	}
	_, ok, err = db.ConsumeShareToken("hash-old")
	if err != nil {
		t.Fatalf("consume expired: %v", err)
	}
	if ok {
		t.Fatal("expected expired token to be rejected")
	}

	// An unknown token is rejected.
	_, ok, err = db.ConsumeShareToken("does-not-exist")
	if err != nil {
		t.Fatalf("consume unknown: %v", err)
	}
	if ok {
		t.Fatal("expected unknown token to be rejected")
	}
}

// TestSweepExpiredShareTokens verifies the sweeper deletes only expired
// tokens, leaving active ones consumable.
func TestSweepExpiredShareTokens(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	if err := db.CreateShareToken("expired-1", "/a", "", time.Now().Add(-time.Hour)); err != nil {
		t.Fatalf("create expired-1: %v", err)
	}
	if err := db.CreateShareToken("expired-2", "/b", "", time.Now().Add(-time.Second)); err != nil {
		t.Fatalf("create expired-2: %v", err)
	}
	if err := db.CreateShareToken("active-1", "/c", "", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("create active-1: %v", err)
	}

	n, err := db.SweepExpiredShareTokens()
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if n != 2 {
		t.Fatalf("expected 2 swept, got %d", n)
	}

	tokens, err := db.ListShareTokens("")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(tokens) != 1 || tokens[0].TokenHash != "active-1" {
		t.Fatalf("expected only active-1 left, got %+v", tokens)
	}
}

// TestOpenEnablesWAL verifies the DSN params from Open actually take effect:
// modernc.org/sqlite only honors `_pragma=...` query params, so the
// mattn-style `_journal_mode`/`_busy_timeout` keys would otherwise be
// silently ignored and the database would stay in its default journal mode.
func TestOpenEnablesWAL(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	var mode string
	if err := db.db.QueryRow(`PRAGMA journal_mode`).Scan(&mode); err != nil {
		t.Fatalf("query journal_mode: %v", err)
	}
	if mode != "wal" {
		t.Fatalf("expected journal_mode=wal, got %q", mode)
	}

	var busyTimeout int
	if err := db.db.QueryRow(`PRAGMA busy_timeout`).Scan(&busyTimeout); err != nil {
		t.Fatalf("query busy_timeout: %v", err)
	}
	if busyTimeout != 5000 {
		t.Fatalf("expected busy_timeout=5000, got %d", busyTimeout)
	}
}

// TestSetDeviceJailRoundTrip verifies SetDeviceJail persists and clears the
// per-device path jail (H2), that GetDeviceByID and DeviceByToken both
// reflect it, and that a fresh device defaults to an empty JailRoot
// (preserving today's behavior for existing rows).
func TestSetDeviceJailRoundTrip(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	if err := db.CreateDevice("id-1", "phone-a", "tok-a"); err != nil {
		t.Fatalf("create: %v", err)
	}

	// Default JailRoot is "".
	d, err := db.GetDeviceByID("id-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if d == nil || d.JailRoot != "" {
		t.Fatalf("expected empty default JailRoot, got %+v", d)
	}

	// Set a jail.
	if err := db.SetDeviceJail("id-1", "/srv/shared"); err != nil {
		t.Fatalf("set jail: %v", err)
	}
	d, err = db.GetDeviceByID("id-1")
	if err != nil {
		t.Fatalf("get after set: %v", err)
	}
	if d == nil || d.JailRoot != "/srv/shared" {
		t.Fatalf("expected JailRoot set, got %+v", d)
	}

	// DeviceByToken and ListDevices also reflect it.
	byToken, err := db.DeviceByToken("tok-a")
	if err != nil {
		t.Fatalf("by token: %v", err)
	}
	if byToken == nil || byToken.JailRoot != "/srv/shared" {
		t.Fatalf("expected JailRoot via DeviceByToken, got %+v", byToken)
	}
	list, err := db.ListDevices()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 || list[0].JailRoot != "/srv/shared" {
		t.Fatalf("expected JailRoot in list, got %+v", list)
	}

	// Clear the jail.
	if err := db.SetDeviceJail("id-1", ""); err != nil {
		t.Fatalf("clear jail: %v", err)
	}
	d, err = db.GetDeviceByID("id-1")
	if err != nil {
		t.Fatalf("get after clear: %v", err)
	}
	if d == nil || d.JailRoot != "" {
		t.Fatalf("expected JailRoot cleared, got %+v", d)
	}
}

// TestGetDeviceByIDUnknown verifies GetDeviceByID returns (nil,nil) for an
// id that doesn't exist (used by setDeviceJailHandler to 404).
func TestGetDeviceByIDUnknown(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	d, err := db.GetDeviceByID("does-not-exist")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if d != nil {
		t.Fatalf("expected nil for unknown id, got %+v", d)
	}
}

// TestMarkChunkReceivedConcurrent reproduces the lost-update race: many
// goroutines each mark a distinct chunk received concurrently. Without a
// transaction around the read-modify-write of received_chunks, concurrent
// writers can clobber each other's updates and some chunks go unrecorded.
func TestMarkChunkReceivedConcurrent(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	const n = 32
	tr := &Transfer{
		ID:          "transfer-1",
		TargetPath:  "/tmp/whatever",
		TotalSize:   int64(n) * 1024,
		ChunkSize:   1024,
		SHA256:      "deadbeef",
		TempPath:    "/tmp/whatever.tmp",
		TotalChunks: n,
	}
	if err := db.CreateTransfer(tr); err != nil {
		t.Fatalf("create transfer: %v", err)
	}

	const workers = 8
	var wg sync.WaitGroup
	chunkCh := make(chan int, n)
	for i := 0; i < n; i++ {
		chunkCh <- i
	}
	close(chunkCh)

	errCh := make(chan error, workers)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for chunk := range chunkCh {
				if err := db.MarkChunkReceived(tr.ID, chunk); err != nil {
					errCh <- err
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatalf("MarkChunkReceived: %v", err)
	}

	// PR-42: the chunk set lives in transfer_chunks now, not a JSON column on
	// the transfer row — GetTransfer deliberately no longer carries it.
	chunks, err := db.ChunkNumbers(tr.ID)
	if err != nil {
		t.Fatalf("chunk numbers: %v", err)
	}
	if len(chunks) != n {
		t.Fatalf("expected %d received chunks, got %d: %v", n, len(chunks), chunks)
	}
	got, err := db.GetTransfer(tr.ID)
	if err != nil {
		t.Fatalf("get transfer: %v", err)
	}
	if got.ReceivedCount != n {
		t.Fatalf("expected ReceivedCount %d, got %d", n, got.ReceivedCount)
	}
	seen := make(map[int]bool, n)
	for _, c := range chunks {
		seen[c] = true
	}
	for i := 0; i < n; i++ {
		if !seen[i] {
			t.Fatalf("chunk %d missing from received_chunks: %v", i, got.ReceivedChunks)
		}
	}
}

func TestDeleteTransfer(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	tr := &Transfer{ID: "transfer-1", TargetPath: "/tmp/whatever", TotalSize: 10, ChunkSize: 10, TotalChunks: 1}
	if err := db.CreateTransfer(tr); err != nil {
		t.Fatalf("create transfer: %v", err)
	}

	if err := db.DeleteTransfer(tr.ID); err != nil {
		t.Fatalf("delete transfer: %v", err)
	}
	if got, err := db.GetTransfer(tr.ID); err != nil || got != nil {
		t.Fatalf("expected (nil, nil) after delete, got (%+v, %v)", got, err)
	}
	if err := db.DeleteTransfer(tr.ID); err != sql.ErrNoRows {
		t.Fatalf("expected sql.ErrNoRows deleting again, got %v", err)
	}
}

func TestCreateAndGetUser(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	if err := db.CreateUser("owner", "hashed-value"); err != nil {
		t.Fatalf("create user: %v", err)
	}

	u, err := db.GetUserByUsername("owner")
	if err != nil {
		t.Fatalf("get user: %v", err)
	}
	if u == nil || u.PasswordHash != "hashed-value" {
		t.Fatalf("expected user with stored hash, got %+v", u)
	}

	none, err := db.GetUserByUsername("nobody")
	if err != nil {
		t.Fatalf("get nonexistent user: %v", err)
	}
	if none != nil {
		t.Fatalf("expected nil for nonexistent user, got %+v", none)
	}
}

func TestCreateUser_DuplicateUsernameFails(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	if err := db.CreateUser("owner", "hash-1"); err != nil {
		t.Fatalf("first create: %v", err)
	}
	if err := db.CreateUser("owner", "hash-2"); err == nil {
		t.Fatal("expected an error creating a duplicate username, got nil")
	}
}

// TestMigrate_SetsVersionAndIsIdempotent is the PR-46 regression: migrations
// are versioned and transactional, and reopening an already-migrated DB is a
// no-op rather than a replay.
func TestMigrate_SetsVersionAndIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	var version int
	if err := db.db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		t.Fatalf("read version: %v", err)
	}
	if version != len(migrations) {
		t.Fatalf("want user_version %d, got %d", len(migrations), version)
	}
	db.Close()

	// Reopening must not fail or renumber.
	db2, err := Open(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer db2.Close()
	if err := db2.db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		t.Fatalf("read version after reopen: %v", err)
	}
	if version != len(migrations) {
		t.Fatalf("after reopen: want user_version %d, got %d", len(migrations), version)
	}
}

// TestMigrate_UpgradesLegacyZeroVersionDB: databases created before
// versioning report user_version=0 and must replay the whole list cleanly
// against a schema that already has every column — the real upgrade path for
// anyone running the agent today.
func TestMigrate_UpgradesLegacyZeroVersionDB(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	// Simulate the pre-versioning state: full schema, no version stamp.
	if _, err := db.db.Exec(`PRAGMA user_version = 0`); err != nil {
		t.Fatalf("reset version: %v", err)
	}
	if err := db.migrate(); err != nil {
		t.Fatalf("replay against existing schema must succeed, got: %v", err)
	}
	var version int
	if err := db.db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		t.Fatalf("read version: %v", err)
	}
	if version != len(migrations) {
		t.Fatalf("want user_version %d after upgrade, got %d", len(migrations), version)
	}
	db.Close()
}

// TestMigrate_RefusesNewerSchema: a DB written by a newer agent must not be
// run against older code that doesn't know its schema.
func TestMigrate_RefusesNewerSchema(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	if _, err := db.db.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, len(migrations)+1)); err != nil {
		t.Fatalf("bump version: %v", err)
	}
	if err := db.migrate(); err == nil {
		t.Fatal("expected migrate to refuse a newer schema version")
	}
}

// TestMigrateChunkTable_BackfillsLegacyJSON is the PR-42 upgrade path: a
// database still carrying received_chunks JSON must come out the other side
// with the same chunks in transfer_chunks, so an in-flight transfer keeps its
// resume progress across the upgrade.
func TestMigrateChunkTable_BackfillsLegacyJSON(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	tr := &Transfer{ID: "legacy", TargetPath: "/tmp/x", TotalSize: 30, ChunkSize: 10, TotalChunks: 3}
	if err := db.CreateTransfer(tr); err != nil {
		t.Fatalf("create: %v", err)
	}

	// Rebuild the pre-PR-42 shape: JSON column present, chunk table empty.
	if _, err := db.db.Exec(`DROP TABLE transfer_chunks`); err != nil {
		t.Fatalf("drop chunk table: %v", err)
	}
	if err := addColumnDirect(db, "transfers", "received_chunks", "TEXT NOT NULL DEFAULT '[]'"); err != nil {
		t.Fatalf("re-add legacy column: %v", err)
	}
	if _, err := db.db.Exec(`UPDATE transfers SET received_chunks='[0,2]' WHERE id=?`, tr.ID); err != nil {
		t.Fatalf("seed legacy json: %v", err)
	}
	if _, err := db.db.Exec(`PRAGMA user_version = 1`); err != nil {
		t.Fatalf("rewind version: %v", err)
	}

	if err := db.migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	chunks, err := db.ChunkNumbers(tr.ID)
	if err != nil {
		t.Fatalf("chunk numbers: %v", err)
	}
	if len(chunks) != 2 || chunks[0] != 0 || chunks[1] != 2 {
		t.Fatalf("backfill lost resume progress: want [0 2], got %v", chunks)
	}
	// The JSON column must be gone, so there is no second copy to drift.
	var legacy int
	if err := db.db.QueryRow(
		`SELECT COUNT(*) FROM pragma_table_info('transfers') WHERE name='received_chunks'`,
	).Scan(&legacy); err != nil {
		t.Fatalf("table info: %v", err)
	}
	if legacy != 0 {
		t.Fatal("legacy received_chunks column survived the migration")
	}
}

// addColumnDirect is the test's own ALTER helper (addColumn takes a *sql.Tx).
func addColumnDirect(db *DB, table, column, spec string) error {
	_, err := db.db.Exec(fmt.Sprintf(`ALTER TABLE %s ADD COLUMN %s %s`, table, column, spec))
	return err
}

// TestRegisterAccount_EnforcesSingleAccountAtomically is the PR-45
// regression: "this computer has exactly one account" used to be decided by a
// SELECT in one transaction and enforced by an INSERT in another, so a second
// registration slipping between them created a second full admin.
func TestRegisterAccount_EnforcesSingleAccountAtomically(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	if _, err := db.RegisterAccount("client-1", "phone", "tok-1", "pk-1", "owner", "hash-1"); err != nil {
		t.Fatalf("first register: %v", err)
	}
	// A different username must still be refused: the rule is one account, not
	// one name.
	if _, err := db.RegisterAccount("client-2", "laptop", "tok-2", "pk-2", "intruder", "hash-2"); !errors.Is(err, ErrAccountExists) {
		t.Fatalf("second register: want ErrAccountExists, got %v", err)
	}
	users, err := db.ListUsers()
	if err != nil {
		t.Fatalf("list users: %v", err)
	}
	if len(users) != 1 || users[0].Username != "owner" {
		t.Fatalf("want exactly the first account, got %+v", users)
	}
}

// TestRegisterAccount_RollsBackDeviceOnFailure: the account and its device
// land together or not at all — a burned pairing code with a half-built
// account is unrecoverable for the user.
func TestRegisterAccount_RollsBackDeviceOnFailure(t *testing.T) {
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	if _, err := db.RegisterAccount("client-1", "phone", "tok-1", "pk-1", "owner", "hash-1"); err != nil {
		t.Fatalf("first register: %v", err)
	}
	// This one fails on the single-account rule, after which its device must
	// not exist.
	if _, err := db.RegisterAccount("client-2", "laptop", "tok-2", "pk-2", "intruder", "hash-2"); err == nil {
		t.Fatal("expected failure")
	}
	devices, err := db.ListDevices()
	if err != nil {
		t.Fatalf("list devices: %v", err)
	}
	for _, d := range devices {
		if d.Label == "laptop" {
			t.Fatal("a failed registration left its device row behind")
		}
	}
}
