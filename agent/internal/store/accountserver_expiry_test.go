package store

import (
	"crypto/ed25519"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/security"
)

func setFederatedLoginAge(t *testing.T, db *DB, deviceID string, age time.Duration) {
	t.Helper()
	at := time.Now().Add(-age).Unix()
	if _, err := db.db.Exec(`UPDATE devices SET federated_login_at=? WHERE id=?`, at, deviceID); err != nil {
		t.Fatal(err)
	}
}

func expiredAuditRows(t *testing.T, db *DB) []AuditEntry {
	t.Helper()
	entries, err := db.AuditEntries(200, 0)
	if err != nil {
		t.Fatal(err)
	}
	var out []AuditEntry
	for _, e := range entries {
		if e.Action == AuditFederatedSessionExpired {
			out = append(out, e)
		}
	}
	return out
}

func TestFederatedSession_ValidInsideTTL(t *testing.T) {
	db, fp := federatedFixture(t)
	id, err := db.LoginDeviceFederated("c1", "phone", "tok", "pk", "alice", fp)
	if err != nil {
		t.Fatal(err)
	}
	setFederatedLoginAge(t, db, id, FederatedSessionTTL-time.Hour)
	if d, err := db.DeviceByToken("tok"); err != nil || d == nil || d.Revoked {
		t.Fatalf("DeviceByToken = %+v, %v; want a live session", d, err)
	}
	if n := len(expiredAuditRows(t, db)); n != 0 {
		t.Fatalf("%d expiry audit rows for a live session", n)
	}
}

func TestFederatedSession_ExpiredTokenIsRefusedAndAuditedOnce(t *testing.T) {
	db, fp := federatedFixture(t)
	id, err := db.LoginDeviceFederated("c1", "phone", "tok", "pk", "alice", fp)
	if err != nil {
		t.Fatal(err)
	}
	setFederatedLoginAge(t, db, id, FederatedSessionTTL+time.Minute)
	for i := 0; i < 3; i++ {
		if d, err := db.DeviceByToken("tok"); err != nil || (d != nil && !d.Revoked) {
			t.Fatalf("call %d: DeviceByToken = %+v, %v; want no usable device", i, d, err)
		}
	}
	rows := expiredAuditRows(t, db)
	if len(rows) != 1 {
		t.Fatalf("%d expiry audit rows, want exactly 1", len(rows))
	}
	if rows[0].Target != id || rows[0].Actor != "alice@"+fp[:12] {
		t.Fatalf("audit row = %+v", rows[0])
	}
}

func TestFederatedSession_NoLoginTimeCountsAsExpired(t *testing.T) {
	db, fp := federatedFixture(t)
	id, err := db.LoginDeviceFederated("c1", "phone", "tok", "pk", "alice", fp)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.db.Exec(`UPDATE devices SET federated_login_at=NULL WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	if d, err := db.DeviceByToken("tok"); err != nil || (d != nil && !d.Revoked) {
		t.Fatalf("DeviceByToken = %+v, %v; want no usable device", d, err)
	}
}

func TestFederatedSession_FreshAssertionRenews(t *testing.T) {
	db, fp := federatedFixture(t)
	id, err := db.LoginDeviceFederated("c1", "phone", "tok-old", "pk", "alice", fp)
	if err != nil {
		t.Fatal(err)
	}
	setFederatedLoginAge(t, db, id, FederatedSessionTTL+time.Hour)
	if d, _ := db.DeviceByToken("tok-old"); d != nil && !d.Revoked {
		t.Fatal("expired session still usable")
	}
	again, err := db.LoginDeviceFederated("c1", "phone", "tok-new", "pk", "alice", fp)
	if err != nil || again != id {
		t.Fatalf("re-login = %q, %v; want device %q", again, err, id)
	}
	d, err := db.DeviceByToken("tok-new")
	if err != nil || d == nil || d.Revoked {
		t.Fatalf("renewed session = %+v, %v", d, err)
	}
}

func TestFederatedSession_OtherDevicesNeverExpire(t *testing.T) {
	db, _ := federatedFixture(t)
	id, err := db.UpsertDevice("c-plain", "plain", "tok-plain", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.db.Exec(`UPDATE devices SET created=1, last_seen=1 WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	if d, err := db.DeviceByToken("tok-plain"); err != nil || d == nil || d.Revoked {
		t.Fatalf("DeviceByToken = %+v, %v", d, err)
	}
}

func accountKeyFingerprint(t *testing.T, dir string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, AccountKeyFile))
	if err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(raw)
	if block == nil {
		t.Fatal("key file is not PEM")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	return security.KeyFingerprint(parsed.(ed25519.PrivateKey).Public().(ed25519.PublicKey))
}

func TestRotateAccountKey_ReplacesKeyAndKeepsRoleState(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		db, dir := openTestStore(t)
		if err := db.SetAccountServer(true, "home"); err != nil {
			t.Fatal(err)
		}
		before := accountKeyFingerprint(t, dir)
		if !enabled {
			if err := db.SetAccountServer(false, "home"); err != nil {
				t.Fatal(err)
			}
		}
		key, err := db.RotateAccountKey()
		if err != nil {
			t.Fatalf("enabled=%v: RotateAccountKey: %v", enabled, err)
		}
		fp := security.KeyFingerprint(key.Public().(ed25519.PublicKey))
		if fp == before || fp != accountKeyFingerprint(t, dir) {
			t.Fatalf("enabled=%v: new fp %s, old %s, on disk %s", enabled, fp, before, accountKeyFingerprint(t, dir))
		}
		if runtime.GOOS != "windows" {
			info, err := os.Stat(filepath.Join(dir, AccountKeyFile))
			if err != nil || info.Mode().Perm() != 0o600 {
				t.Fatalf("enabled=%v: key mode = %v, %v; want 0600", enabled, info.Mode().Perm(), err)
			}
		}
		cfg, err := db.AccountServer()
		if err != nil || cfg.Enabled != enabled || cfg.Name != "home" {
			t.Fatalf("enabled=%v: role after rotation = %+v, %v", enabled, cfg, err)
		}
		left, _ := filepath.Glob(filepath.Join(dir, ".account-key-*"))
		if len(left) != 0 {
			t.Fatalf("temp files left behind: %v", left)
		}
	}
}

func TestRotateAccountKey_NoKeyToRotate(t *testing.T) {
	db, dir := openTestStore(t)
	if _, err := db.RotateAccountKey(); err == nil {
		t.Fatal("rotating a key that was never created succeeded")
	}
	if _, err := os.Stat(filepath.Join(dir, AccountKeyFile)); !os.IsNotExist(err) {
		t.Fatalf("a key file appeared (stat err = %v)", err)
	}
}
