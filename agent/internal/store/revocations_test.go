package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func logPath(db *DB) string { return filepath.Join(db.dir, RevocationsLogFile) }

func TestRevokeAndRemoveAppendToLog(t *testing.T) {
	db := openTestDB(t)
	if err := db.CreateDevice("dev-a", "A", "tok-a"); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateDevice("dev-b", "B", "tok-b"); err != nil {
		t.Fatal(err)
	}
	if err := db.RevokeDevice("dev-a"); err != nil {
		t.Fatal(err)
	}
	if err := db.DeleteDevice("dev-b"); err != nil {
		t.Fatal(err)
	}
	if err := db.RevokeDevice("no-such-device"); err != nil { // changes no row, so it is not logged
		t.Fatal(err)
	}
	got, err := ReadRevocations(logPath(db))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].DeviceID != "dev-a" || got[0].Kind != RevocationRevoke ||
		got[1].DeviceID != "dev-b" || got[1].Kind != RevocationRemove {
		t.Fatalf("log = %+v, want revoke dev-a then remove dev-b", got)
	}
	if got[0].Time.IsZero() || time.Since(got[0].Time) > time.Minute {
		t.Fatalf("log time = %v", got[0].Time)
	}
	if info, err := os.Stat(logPath(db)); err != nil || info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("log mode = %v, err %v; want owner-only", info.Mode(), err)
	}
}

func TestDeleteUserLogsTheDevicesItRevokes(t *testing.T) {
	db := openTestDB(t)
	if err := db.CreateUser("alice", "hash-a"); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateUser("bob", "hash-b"); err != nil {
		t.Fatal(err)
	}
	id, err := db.LoginDevice("client-1", "phone", "tok-1", "pk", "alice", "hash-a")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.DeleteUser("alice"); err != nil {
		t.Fatal(err)
	}
	got, err := ReadRevocations(logPath(db))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].DeviceID != id || got[0].Kind != RevocationRevoke {
		t.Fatalf("log = %+v, want one revoke for %s", got, id)
	}
}

// ReplayRevocation re-applies a revoke or remove to a database that predates it, and doing it twice
// changes nothing further.
func TestReplayRevocationIsIdempotent(t *testing.T) {
	db := openTestDB(t)
	for _, id := range []string{"dev-a", "dev-b", "dev-c"} {
		if err := db.CreateDevice(id, id, "tok-"+id); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 2; i++ {
		if err := db.ReplayRevocation("dev-a", RevocationRevoke); err != nil {
			t.Fatal(err)
		}
		if err := db.ReplayRevocation("dev-b", RevocationRemove); err != nil {
			t.Fatal(err)
		}
		if err := db.ReplayRevocation("never-existed", RevocationRemove); err != nil {
			t.Fatal(err)
		}
	}
	if d, _ := db.GetDeviceByID("dev-a"); d == nil || !d.Revoked {
		t.Fatalf("dev-a = %+v, want revoked", d)
	}
	if d, _ := db.GetDeviceByID("dev-b"); d != nil {
		t.Fatalf("dev-b = %+v, want removed", d)
	}
	if d, _ := db.GetDeviceByID("dev-c"); d == nil || d.Revoked {
		t.Fatalf("dev-c = %+v, want untouched", d)
	}
	if _, err := os.Stat(logPath(db)); !os.IsNotExist(err) {
		t.Fatalf("replay wrote to the log: %v", err)
	}
	if err := db.ReplayRevocation("dev-c", "bogus"); err == nil {
		t.Fatal("unknown kind accepted")
	}
}

func TestReadRevocationsTornAndMalformed(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, RevocationsLogFile)
	good := `{"device_id":"d1","time":"2026-01-02T03:04:05Z","kind":"revoke"}` + "\n"

	if err := os.WriteFile(p, []byte(good+`{"device_id":"d2","ti`), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := ReadRevocations(p)
	if err != nil || len(got) != 1 || got[0].DeviceID != "d1" {
		t.Fatalf("torn last line: got %+v, err %v; want just d1", got, err)
	}

	if err := os.WriteFile(p, []byte("not json\n"+good), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadRevocations(p); err == nil {
		t.Fatal("malformed middle line was accepted")
	}

	if got, err := ReadRevocations(filepath.Join(dir, "absent.log")); err != nil || len(got) != 0 {
		t.Fatalf("missing log: %+v, %v; want empty", got, err)
	}
}
