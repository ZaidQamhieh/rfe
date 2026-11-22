package store

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

// TestMigrateWakeGrant_ExistingDevicesKeepWakeOnLAN proves the upgrade step
// keeps Wake-on-LAN for devices that could send it before (read-write rows),
// leaves read-only rows without it (they were refused before), and that a
// device paired afterwards starts without it.
func TestMigrateWakeGrant_ExistingDevicesKeepWakeOnLAN(t *testing.T) {
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
	for id, want := range map[string]bool{"legacy-rw": true, "legacy-ro": false} {
		d, err := db.GetDeviceByID(id)
		if err != nil || d == nil {
			t.Fatalf("get %s: (%+v, %v)", id, d, err)
		}
		if d.CanWake != want {
			t.Fatalf("migrated %s CanWake = %v, want %v", id, d.CanWake, want)
		}
	}

	pairedID, err := db.UpsertDevice("new-paired", "phone", "pair-token", "", false)
	if err != nil {
		t.Fatalf("create paired device: %v", err)
	}
	if paired, _ := db.GetDeviceByID(pairedID); paired == nil || paired.CanWake {
		t.Fatalf("new code-paired device must start without wake: %+v", paired)
	}
	ownerID, err := db.UpsertDevice("new-owner", "owner", "owner-token", "", true)
	if err != nil {
		t.Fatalf("create owner device: %v", err)
	}
	if owner, _ := db.GetDeviceByID(ownerID); owner == nil || !owner.CanWake {
		t.Fatalf("owner (login) device must report wake: %+v", owner)
	}

	// An admin turns the legacy device's wake off; replaying the step (a
	// user_version reset) must not hand it back.
	if err := db.SetDeviceGrants("legacy-rw", true, true, true, true, true, true, false); err != nil {
		t.Fatalf("SetDeviceGrants: %v", err)
	}
	replay, err := db.db.Begin()
	if err != nil {
		t.Fatalf("begin replay: %v", err)
	}
	if err := migrateDeviceWakeGrant(replay); err != nil {
		t.Fatalf("replay migration: %v", err)
	}
	if err := replay.Commit(); err != nil {
		t.Fatalf("commit replay: %v", err)
	}
	if d, _ := db.GetDeviceByID("legacy-rw"); d == nil || d.CanWake {
		t.Fatalf("replayed migration re-enabled a revoked wake grant: %+v", d)
	}
}

// TestWakeGrantLifecycle covers every path that writes the grant: the admin
// setter, the file-grant setter (which must not touch it), re-pairing, access
// reset and a federated login.
func TestWakeGrantLifecycle(t *testing.T) {
	db, fp := federatedFixture(t)
	id, err := db.UpsertDevice("client-w", "phone", "tok-w1", "", false)
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	wake := func() bool {
		t.Helper()
		d, err := db.GetDeviceByID(id)
		if err != nil || d == nil {
			t.Fatalf("get: (%+v, %v)", d, err)
		}
		return d.CanWake
	}
	if wake() {
		t.Fatal("a code-paired device starts without wake")
	}
	if err := db.SetDeviceGrants(id, true, false, false, false, false, false, true); err != nil {
		t.Fatalf("SetDeviceGrants: %v", err)
	}
	if !wake() {
		t.Fatal("SetDeviceGrants(wake=true) did not grant wake")
	}
	if err := db.SetDeviceFilePermissions(id, true, true, true, true, true, true); err != nil {
		t.Fatalf("SetDeviceFilePermissions: %v", err)
	}
	if !wake() {
		t.Fatal("SetDeviceFilePermissions must leave the wake grant alone")
	}
	if err := db.CreateShareToken("tok", "/tmp/f", id, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("share: %v", err)
	}
	if err := db.SetDeviceGrants(id, true, true, true, true, true, false, true); err != nil {
		t.Fatalf("SetDeviceGrants: %v", err)
	}
	if got, _ := db.GetShareToken("tok"); got != nil {
		t.Fatal("SetDeviceGrants(share=false) must revoke share links like SetDeviceFilePermissions")
	}

	// A re-pair by code keeps what the admin set; the access reset clears it.
	if _, err := db.UpsertDevice("client-w", "phone", "tok-w2", "", false); err != nil {
		t.Fatalf("re-pair: %v", err)
	}
	if !wake() {
		t.Fatal("re-pair by code must keep the admin's grants")
	}
	if err := db.ResetDeviceAccess(id); err != nil {
		t.Fatalf("ResetDeviceAccess: %v", err)
	}
	if wake() {
		t.Fatal("ResetDeviceAccess must clear wake")
	}

	// Becoming an owner device grants it; being demoted takes it away.
	if _, err := db.UpsertDevice("client-w", "phone", "tok-w3", "", true); err != nil {
		t.Fatalf("owner login: %v", err)
	}
	if !wake() {
		t.Fatal("owner device must have wake")
	}
	if _, err := db.UpsertDevice("client-w", "phone", "tok-w4", "", false); err != nil {
		t.Fatalf("demote: %v", err)
	}
	if wake() {
		t.Fatal("demotion from owner to code-paired must drop wake")
	}

	// A federated session never keeps wake, even one granted earlier.
	if err := db.SetDeviceGrants(id, true, true, true, false, false, false, true); err != nil {
		t.Fatalf("SetDeviceGrants: %v", err)
	}
	if _, err := db.LoginDeviceFederated("client-w", "phone", "tok-w5", "pubkey", "alice", fp); err != nil {
		t.Fatalf("federated login: %v", err)
	}
	if wake() {
		t.Fatal("federated login must reset wake to the federated profile (off)")
	}
}
