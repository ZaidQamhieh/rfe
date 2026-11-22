package store

import (
	"testing"
	"time"
)

// Regression tests for the 2026-11-15 account-server security review, finding F1.

func fedShareFixture(t *testing.T) *DB {
	t.Helper()
	db, fp := federatedFixture(t)
	fedDev, err := db.LoginDeviceFederated("c-fed", "phone", "tok-fed", "pk", "alice", fp)
	if err != nil {
		t.Fatal(err)
	}
	plainDev, err := db.UpsertDevice("c-plain", "plain", "tok-plain", "", true)
	if err != nil {
		t.Fatal(err)
	}
	exp := time.Now().Add(time.Hour)
	if err := db.CreateShareToken("hash-fed", "/a", fedDev, exp); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateShareToken("hash-plain", "/b", plainDev, exp); err != nil {
		t.Fatal(err)
	}
	return db
}

func shareTokenExists(t *testing.T, db *DB, hash string) bool {
	t.Helper()
	tok, err := db.GetShareToken(hash)
	if err != nil {
		t.Fatal(err)
	}
	return tok != nil
}

func TestRemoveTrustedAccountServer_DeletesFederatedShareTokens(t *testing.T) {
	db := fedShareFixture(t)
	if err := db.RemoveTrustedAccountServer(); err != nil {
		t.Fatal(err)
	}
	if shareTokenExists(t, db, "hash-fed") {
		t.Fatal("a federated device's share link survived untrusting the account server")
	}
	if !shareTokenExists(t, db, "hash-plain") {
		t.Fatal("untrusting the account server deleted a non-federated device's share link")
	}
}

func TestSetTrustedAccountServer_RepinDeletesFederatedShareTokens(t *testing.T) {
	db := fedShareFixture(t)
	rotated, _ := newTrustKey(t)
	if _, err := db.SetTrustedAccountServer("srv", rotated); err != nil {
		t.Fatal(err)
	}
	if shareTokenExists(t, db, "hash-fed") {
		t.Fatal("a federated device's share link survived re-pinning to a different key")
	}
	if !shareTokenExists(t, db, "hash-plain") {
		t.Fatal("re-pinning deleted a non-federated device's share link")
	}
}

// Re-pinning the same key keeps the sessions alive, so their links must stay too.
func TestSetTrustedAccountServer_SameKeyKeepsFederatedShareTokens(t *testing.T) {
	db := fedShareFixture(t)
	ts, err := db.TrustedAccountServer()
	if err != nil || ts == nil {
		t.Fatalf("TrustedAccountServer = %+v, %v", ts, err)
	}
	if _, err := db.SetTrustedAccountServer("renamed", ts.PublicKey); err != nil {
		t.Fatal(err)
	}
	if !shareTokenExists(t, db, "hash-fed") {
		t.Fatal("re-pinning the same key deleted a still-valid session's share link")
	}
}
