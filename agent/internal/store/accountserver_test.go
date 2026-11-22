package store

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/ZaidQamhieh/rfe/agent/internal/security"
)

func openTestStore(t *testing.T) (*DB, string) {
	t.Helper()
	dir := t.TempDir()
	db, err := Open(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db, dir
}

func newTrustKey(t *testing.T) (string, string) {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(pub), security.KeyFingerprint(pub)
}

func TestAccountServer_OffByDefaultAndNoKeyFile(t *testing.T) {
	db, dir := openTestStore(t)
	cfg, err := db.AccountServer()
	if err != nil || cfg.Enabled {
		t.Fatalf("AccountServer() = %+v, %v; want disabled", cfg, err)
	}
	if _, err := os.Stat(filepath.Join(dir, AccountKeyFile)); !os.IsNotExist(err) {
		t.Fatalf("key file exists while the role is off (stat err = %v)", err)
	}
}

func TestAccountServer_EnableCreatesPrivateKeyOnceAndKeepsIt(t *testing.T) {
	db, dir := openTestStore(t)
	if err := db.SetAccountServer(true, "home"); err != nil {
		t.Fatalf("enable: %v", err)
	}
	cfg, _ := db.AccountServer()
	if !cfg.Enabled || cfg.Name != "home" {
		t.Fatalf("config = %+v", cfg)
	}
	path := filepath.Join(dir, AccountKeyFile)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("key file: %v", err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("key file mode = %v, want 0600", info.Mode().Perm())
	}
	if AccountKeyFile == "agent-key.pem" {
		t.Fatal("account key must not be the TLS key file")
	}
	first, err := db.AccountSigningKey()
	if err != nil {
		t.Fatal(err)
	}
	// Disable and re-enable, and reopen: the same key (peers pinned it).
	if err := db.SetAccountServer(false, "home"); err != nil {
		t.Fatal(err)
	}
	if err := db.SetAccountServer(true, "home"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	db2, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db2.Close()
	second, err := db2.AccountSigningKey()
	if err != nil {
		t.Fatal(err)
	}
	if !first.Equal(second) {
		t.Fatal("signing key changed across disable/enable/reopen")
	}
}

func TestAccountServer_SigningKeyRefusedWhileOff(t *testing.T) {
	db, _ := openTestStore(t)
	if _, err := db.AccountSigningKey(); !errors.Is(err, ErrAccountServerOff) {
		t.Fatalf("err = %v, want ErrAccountServerOff", err)
	}
}

func TestTrustedAccountServer_SetGetRemove(t *testing.T) {
	db, _ := openTestStore(t)
	if got, err := db.TrustedAccountServer(); err != nil || got != nil {
		t.Fatalf("initial = %+v, %v", got, err)
	}
	key, fp := newTrustKey(t)
	set, err := db.SetTrustedAccountServer("home server", key)
	if err != nil {
		t.Fatalf("set: %v", err)
	}
	if set.Fingerprint != fp || set.Name != "home server" || set.PublicKey != key {
		t.Fatalf("set = %+v", set)
	}
	got, err := db.TrustedAccountServer()
	if err != nil || got == nil || *got != set {
		t.Fatalf("get = %+v, %v; want %+v", got, err, set)
	}
	if err := db.RemoveTrustedAccountServer(); err != nil {
		t.Fatal(err)
	}
	if got, _ := db.TrustedAccountServer(); got != nil {
		t.Fatalf("still trusted after remove: %+v", got)
	}
}

func TestTrustedAccountServer_RejectsMalformedKey(t *testing.T) {
	db, _ := openTestStore(t)
	for name, key := range map[string]string{
		"empty":      "",
		"not base64": "***",
		"short":      base64.StdEncoding.EncodeToString([]byte("short")),
	} {
		if _, err := db.SetTrustedAccountServer("x", key); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func federatedFixture(t *testing.T) (*DB, string) {
	t.Helper()
	db, _ := openTestStore(t)
	key, fp := newTrustKey(t)
	if _, err := db.SetTrustedAccountServer("srv", key); err != nil {
		t.Fatal(err)
	}
	return db, fp
}

func TestLoginDeviceFederated_MintsOwnerDeviceWithoutLocalAccount(t *testing.T) {
	db, fp := federatedFixture(t)
	id, err := db.LoginDeviceFederated("client-1", "phone", "tok-fed", "pubkey", "alice", fp)
	if err != nil {
		t.Fatalf("LoginDeviceFederated: %v", err)
	}
	dev, err := db.DeviceByToken("tok-fed")
	if err != nil || dev == nil {
		t.Fatalf("DeviceByToken = %+v, %v", dev, err)
	}
	if dev.ID != id || dev.ViaLogin || dev.CanModify || dev.CanDelete || dev.CanShare || !dev.CanBrowse || dev.Revoked || dev.PublicKey != "pubkey" {
		t.Fatalf("device = %+v", dev)
	}
	user, issuer, err := db.DeviceAccount(id)
	if err != nil || user != "alice" || issuer != fp {
		t.Fatalf("DeviceAccount = %q, %q, %v", user, issuer, err)
	}
	if has, _ := db.HasAnyUser(); has {
		t.Fatal("a federated login must not create a local account")
	}
}

func TestLoginDeviceFederated_RequiresCurrentlyTrustedIssuer(t *testing.T) {
	db, _ := federatedFixture(t)
	_, otherFp := newTrustKey(t)
	if _, err := db.LoginDeviceFederated("c", "phone", "tok", "pk", "alice", otherFp); !errors.Is(err, ErrNoTrustedAccountServer) {
		t.Fatalf("err = %v, want ErrNoTrustedAccountServer", err)
	}
	if dev, _ := db.DeviceByToken("tok"); dev != nil {
		t.Fatal("a token was minted for an untrusted issuer")
	}
}

func TestFederatedDevice_DiesWhenServerUntrustedOrKeyReplaced(t *testing.T) {
	db, fp := federatedFixture(t)
	if _, err := db.LoginDeviceFederated("c1", "phone", "tok-a", "pk", "alice", fp); err != nil {
		t.Fatal(err)
	}
	if err := db.RemoveTrustedAccountServer(); err != nil {
		t.Fatal(err)
	}
	if dev, _ := db.DeviceByToken("tok-a"); dev != nil {
		t.Fatal("federated token still works after the server was untrusted")
	}
	// Re-trusting the same key does not resurrect it: the session was revoked.
	key, fp2 := newTrustKey(t)
	if _, err := db.SetTrustedAccountServer("srv2", key); err != nil {
		t.Fatal(err)
	}
	if _, err := db.LoginDeviceFederated("c2", "tablet", "tok-b", "pk2", "bob", fp2); err != nil {
		t.Fatal(err)
	}
	rotated, _ := newTrustKey(t)
	if _, err := db.SetTrustedAccountServer("srv2", rotated); err != nil {
		t.Fatal(err)
	}
	if dev, _ := db.DeviceByToken("tok-b"); dev != nil {
		t.Fatal("federated token still works after the pinned key was replaced")
	}
	if dev, _ := db.DeviceByToken("tok-a"); dev != nil {
		t.Fatal("old federated token revived")
	}
}

func TestFederatedDevice_LocalAccountRulesUnchanged(t *testing.T) {
	db, fp := federatedFixture(t)
	if err := db.CreateUser("alice", "hash-a"); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateUser("keeper", "hash-k"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.LoginDeviceFederated("c1", "phone", "tok-fed", "pk", "alice", fp); err != nil {
		t.Fatal(err)
	}
	// Deleting a local account of the same name does not end the other server's session.
	if err := db.DeleteUser("alice"); err != nil {
		t.Fatal(err)
	}
	if dev, _ := db.DeviceByToken("tok-fed"); dev == nil {
		t.Fatal("deleting a same-named local account revoked a federated session")
	}
	// A password login by the same client clears the federated marker, so the
	// device is bound to the local account again and not to the server.
	if _, err := db.LoginDevice("c1", "phone", "tok-pw", "pk", "keeper", "hash-k"); err != nil {
		t.Fatal(err)
	}
	if _, issuer, _ := db.DeviceAccount(mustDeviceID(t, db, "tok-pw")); issuer != "" {
		t.Fatalf("issuer after password login = %q, want empty", issuer)
	}
	if err := db.RemoveTrustedAccountServer(); err != nil {
		t.Fatal(err)
	}
	if dev, _ := db.DeviceByToken("tok-pw"); dev == nil {
		t.Fatal("untrusting the server ended a password-login session")
	}
}

func mustDeviceID(t *testing.T, db *DB, token string) string {
	t.Helper()
	dev, err := db.DeviceByToken(token)
	if err != nil || dev == nil {
		t.Fatalf("DeviceByToken(%s) = %v, %v", token, dev, err)
	}
	return dev.ID
}
