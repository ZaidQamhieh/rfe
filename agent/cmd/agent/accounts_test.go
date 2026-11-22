//go:build linux

package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ZaidQamhieh/rfe/agent/internal/security"
	"github.com/ZaidQamhieh/rfe/agent/internal/store"
)

// fakeAccountServer serves GET /v1/accounts/key like an enabled account server (TLS, self-signed).
func fakeAccountServer(t *testing.T, status int) (hostPort, keyB64, fp string) {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	keyB64 = base64.StdEncoding.EncodeToString(pub)
	fp = security.KeyFingerprint(pub)
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/accounts/key" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if status != http.StatusOK {
			_ = json.NewEncoder(w).Encode(map[string]string{"code": "ACCOUNT_SERVER_DISABLED", "message": "off"})
			return
		}
		// A lying keyFingerprint field must be ignored: the CLI computes its own.
		_ = json.NewEncoder(w).Encode(map[string]string{
			"name": "lab server", "publicKey": keyB64, "keyFingerprint": strings.Repeat("0", 64), "algorithm": "Ed25519",
		})
	}))
	t.Cleanup(srv.Close)
	return strings.TrimPrefix(srv.URL, "https://"), keyB64, fp
}

func accountsDataDir(t *testing.T) string {
	t.Helper()
	pass3Home(t)
	return t.TempDir()
}

func trustedIn(t *testing.T, dir string) *store.TrustedAccountServer {
	t.Helper()
	db, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ts, err := db.TrustedAccountServer()
	if err != nil {
		t.Fatal(err)
	}
	return ts
}

func TestAccountsTrust_YesPinsKeyAndPrintsOwnFingerprint(t *testing.T) {
	dir := accountsDataDir(t)
	host, key, fp := fakeAccountServer(t, http.StatusOK)
	out, _, err := pass3Run(t, "accounts", "trust", host, "--yes", "--fingerprint", fp, "-data", dir)
	if err != nil {
		t.Fatalf("trust: %v (out %q)", err, out)
	}
	if !strings.Contains(out, fp) {
		t.Fatalf("output lacks the computed fingerprint %s: %q", fp, out)
	}
	if strings.Contains(out, strings.Repeat("0", 64)) {
		t.Fatalf("output shows the fingerprint the server claimed: %q", out)
	}
	ts := trustedIn(t, dir)
	if ts == nil || ts.PublicKey != key || ts.Fingerprint != fp || ts.Name != "lab server" {
		t.Fatalf("trusted = %+v", ts)
	}
}

func TestAccountsTrust_ConfirmationDecides(t *testing.T) {
	dir := accountsDataDir(t)
	host, _, fp := fakeAccountServer(t, http.StatusOK)
	for _, tc := range []struct {
		input   string
		pinned  bool
		wantErr bool
	}{{"n\n", false, true}, {"\n", false, true}, {"", false, true}, {"y\n", true, false}} {
		accountsConfirmInput = strings.NewReader(tc.input)
		out, _, err := pass3Run(t, "accounts", "trust", host, "-data", dir)
		if (err != nil) != tc.wantErr {
			t.Fatalf("input %q: err = %v", tc.input, err)
		}
		if !strings.Contains(out, fp) {
			t.Fatalf("input %q: the fingerprint was not shown before asking: %q", tc.input, out)
		}
		if got := trustedIn(t, dir) != nil; got != tc.pinned {
			t.Fatalf("input %q: pinned = %v, want %v", tc.input, got, tc.pinned)
		}
	}
}

func TestAccountsTrust_ServerErrorsAndBadHosts(t *testing.T) {
	dir := accountsDataDir(t)
	off, _, offFp := fakeAccountServer(t, http.StatusForbidden)
	if _, _, err := pass3Run(t, "accounts", "trust", off, "--yes", "--fingerprint", offFp, "-data", dir); err == nil || !strings.Contains(err.Error(), "ACCOUNT_SERVER_DISABLED") {
		t.Fatalf("err = %v, want one naming ACCOUNT_SERVER_DISABLED", err)
	}
	for _, bad := range []string{"https://example.test:8765", "example.test", "user@example.test:8765", "example.test:8765/v1", ""} {
		if _, _, err := pass3Run(t, "accounts", "trust", bad, "--yes", "-data", dir); err == nil {
			t.Errorf("host %q accepted", bad)
		}
	}
	if trustedIn(t, dir) != nil {
		t.Fatal("a failed trust pinned something")
	}
}

func TestAccountsUntrustStatusEnableDisable(t *testing.T) {
	dir := accountsDataDir(t)
	host, _, fp := fakeAccountServer(t, http.StatusOK)
	out, _, err := pass3Run(t, "accounts", "status", "-data", dir)
	if err != nil || !strings.Contains(out, "off") || !strings.Contains(out, "none") {
		t.Fatalf("status = %q, %v", out, err)
	}
	if _, _, err := pass3Run(t, "accounts", "trust", host, "--yes", "--fingerprint", fp, "-data", dir); err != nil {
		t.Fatal(err)
	}
	if _, _, err := pass3Run(t, "accounts", "enable", "--name", "mine", "-data", dir); err != nil {
		t.Fatalf("enable: %v", err)
	}
	out, _, err = pass3Run(t, "accounts", "status", "-data", dir)
	if err != nil || !strings.Contains(out, "on") || !strings.Contains(out, "mine") || !strings.Contains(out, fp) {
		t.Fatalf("status = %q, %v", out, err)
	}
	if _, _, err := pass3Run(t, "accounts", "disable", "-data", dir); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if _, _, err := pass3Run(t, "accounts", "untrust", "-data", dir); err != nil {
		t.Fatalf("untrust: %v", err)
	}
	if trustedIn(t, dir) != nil {
		t.Fatal("still trusted after untrust")
	}
	db, _ := store.Open(dir)
	defer db.Close()
	cfg, _ := db.AccountServer()
	if cfg.Enabled {
		t.Fatal("still enabled after disable")
	}
	entries, _ := db.AuditEntries(100, 0)
	var n int
	for _, e := range entries {
		if e.Action == store.AuditAccountServer {
			n++
		}
	}
	if n < 4 {
		t.Fatalf("CLI audited %d account-server changes, want 4", n)
	}
}

func TestAccountsUsageErrors(t *testing.T) {
	dir := accountsDataDir(t)
	for _, args := range [][]string{
		{"accounts"},
		{"accounts", "bogus"},
		{"accounts", "trust", "-data", dir},
		{"accounts", "untrust", "extra", "-data", dir},
		{"accounts", "status", "extra", "-data", dir},
	} {
		if _, _, err := pass3Run(t, args[0], args[1:]...); err == nil {
			t.Errorf("%v: no error", args)
		}
	}
}

func TestAccountsRotateKey_NewFingerprintKeepsRoleAndAudits(t *testing.T) {
	dir := accountsDataDir(t)
	if _, _, err := pass3Run(t, "accounts", "enable", "--name", "mine", "-data", dir); err != nil {
		t.Fatal(err)
	}
	status := func() string {
		out, _, err := pass3Run(t, "accounts", "status", "-data", dir)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	oldFp := strings.TrimSpace(strings.SplitN(strings.SplitN(status(), "Signing key fingerprint:", 2)[1], "\n", 2)[0])
	out, _, err := pass3Run(t, "accounts", "rotate-key", "-data", dir)
	if err != nil {
		t.Fatalf("rotate-key: %v", err)
	}
	after := status()
	if strings.Contains(out, oldFp) || !strings.Contains(out, "accounts trust") || strings.Contains(after, oldFp) {
		t.Fatalf("rotate-key output %q, status after %q (old fingerprint %s)", out, after, oldFp)
	}
	newFp := strings.TrimSpace(strings.SplitN(strings.SplitN(after, "Signing key fingerprint:", 2)[1], "\n", 2)[0])
	if !strings.Contains(out, newFp) || !strings.Contains(after, "on") || !strings.Contains(after, "mine") {
		t.Fatalf("rotate-key output %q does not show the new fingerprint %s, or the role changed: %q", out, newFp, after)
	}
	db, _ := store.Open(dir)
	defer db.Close()
	entries, _ := db.AuditEntries(100, 0)
	var rotated bool
	for _, e := range entries {
		if e.Action == store.AuditAccountServer && strings.Contains(e.Detail, "rotat") && strings.Contains(e.Detail, newFp) {
			rotated = true
		}
	}
	if !rotated {
		t.Fatalf("no audit row for the rotation: %+v", entries)
	}
}

func TestAccountsRotateKey_RefusesWithoutAKeyAndExtraArgs(t *testing.T) {
	dir := accountsDataDir(t)
	if _, _, err := pass3Run(t, "accounts", "rotate-key", "-data", dir); err == nil {
		t.Fatal("rotate-key without a key succeeded")
	}
	if _, _, err := pass3Run(t, "accounts", "rotate-key", "extra", "-data", dir); err == nil {
		t.Fatal("rotate-key accepted a stray argument")
	}
}

func TestAccountsRevokeAll_RaisesEpochAndAudits(t *testing.T) {
	dir := accountsDataDir(t)
	for want := int64(1); want <= 2; want++ {
		out, _, err := pass3Run(t, "accounts", "revoke-all", "-data", dir)
		if err != nil {
			t.Fatalf("revoke-all: %v", err)
		}
		if !strings.Contains(out, fmt.Sprintf("epoch raised to %d", want)) {
			t.Fatalf("output %q does not show epoch %d", out, want)
		}
	}
	if _, _, err := pass3Run(t, "accounts", "revoke-all", "extra", "-data", dir); err == nil {
		t.Fatal("revoke-all accepted a stray argument")
	}
	db, _ := store.Open(dir)
	defer db.Close()
	if n, err := db.AccountEpoch(); err != nil || n != 2 {
		t.Fatalf("epoch = %d, %v", n, err)
	}
	entries, _ := db.AuditEntries(100, 0)
	var seen bool
	for _, e := range entries {
		if e.Action == store.AuditAccountServer && strings.Contains(e.Detail, "account epoch raised to 2: revoke-all") {
			seen = true
		}
	}
	if !seen {
		t.Fatalf("no audit row for the epoch bump: %+v", entries)
	}
}
