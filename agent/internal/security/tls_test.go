package security

import (
	"crypto/x509"
	"encoding/hex"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"
)

// LoadOrCreateCert must generate a self-signed cert on first run, persist it
// with a private key that is NOT world-readable, and return a usable pair.
func TestLoadOrCreateCertGeneratesAndPersists(t *testing.T) {
	dir := t.TempDir()

	cert, err := LoadOrCreateCert(dir)
	if err != nil {
		t.Fatalf("LoadOrCreateCert: %v", err)
	}
	if len(cert.Certificate) == 0 {
		t.Fatal("returned certificate has no DER bytes")
	}

	for _, name := range []string{"agent-cert.pem", "agent-key.pem"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("expected %s to be written: %v", name, err)
		}
	}

	// The private key must be owner-only (0600) — a world-readable key would
	// let any local user impersonate the agent.
	info, err := os.Stat(filepath.Join(dir, "agent-key.pem"))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("key file perms = %o, want 0600", perm)
	}
}

// A second call must load the existing cert rather than minting a new one —
// otherwise the phone's pinned fingerprint would break on every restart.
func TestLoadOrCreateCertIsIdempotent(t *testing.T) {
	dir := t.TempDir()

	first, err := LoadOrCreateCert(dir)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	second, err := LoadOrCreateCert(dir)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if Fingerprint(first) != Fingerprint(second) {
		t.Error("fingerprint changed between calls; cert was regenerated instead of loaded")
	}
}

// Fingerprint must be a deterministic 64-char lowercase hex SHA-256.
func TestFingerprintFormat(t *testing.T) {
	cert, err := LoadOrCreateCert(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fp := Fingerprint(cert)
	if len(fp) != 64 {
		t.Errorf("fingerprint length = %d, want 64", len(fp))
	}
	if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(fp) {
		t.Errorf("fingerprint %q is not lowercase hex", fp)
	}
	if _, err := hex.DecodeString(fp); err != nil {
		t.Errorf("fingerprint is not valid hex: %v", err)
	}
}

// The generated cert should carry the expected identity, a long validity
// window, server-auth usage, and a loopback SAN.
func TestGeneratedCertProperties(t *testing.T) {
	cert, err := LoadOrCreateCert(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatalf("parse leaf: %v", err)
	}

	if leaf.Subject.CommonName != "remote-file-explorer-agent" {
		t.Errorf("CommonName = %q", leaf.Subject.CommonName)
	}
	if leaf.NotAfter.Before(time.Now().AddDate(9, 0, 0)) {
		t.Errorf("NotAfter = %v, want ~10 years out", leaf.NotAfter)
	}
	hasServerAuth := false
	for _, u := range leaf.ExtKeyUsage {
		if u == x509.ExtKeyUsageServerAuth {
			hasServerAuth = true
		}
	}
	if !hasServerAuth {
		t.Error("cert is missing ExtKeyUsageServerAuth")
	}
	hasLoopback := false
	for _, ip := range leaf.IPAddresses {
		if ip.IsLoopback() {
			hasLoopback = true
		}
	}
	if !hasLoopback {
		t.Error("cert SANs do not include a loopback IP")
	}
}
