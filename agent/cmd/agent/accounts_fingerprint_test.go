package main

import (
	"net/http"
	"strings"
	"testing"
)

// Regression tests for the 2026-11-15 account-server security review, finding F5: --yes alone must not pin.

func TestAccountsTrust_YesRequiresFingerprint(t *testing.T) {
	dir := accountsDataDir(t)
	host, _, _ := fakeAccountServer(t, http.StatusOK)
	_, _, err := pass3Run(t, "accounts", "trust", host, "--yes", "-data", dir)
	if err == nil || !strings.Contains(err.Error(), "--fingerprint") {
		t.Fatalf("--yes without --fingerprint: err = %v, want one naming --fingerprint", err)
	}
	if trustedIn(t, dir) != nil {
		t.Fatal("--yes without --fingerprint pinned a key")
	}
}

func TestAccountsTrust_WrongFingerprintRefusedAndPinUnchanged(t *testing.T) {
	dir := accountsDataDir(t)
	hostA, keyA, fpA := fakeAccountServer(t, http.StatusOK)
	if _, _, err := pass3Run(t, "accounts", "trust", hostA, "--yes", "--fingerprint", fpA, "-data", dir); err != nil {
		t.Fatalf("first pin: %v", err)
	}
	hostB, _, fpB := fakeAccountServer(t, http.StatusOK)
	for _, wrong := range []string{fpA, strings.Repeat("0", 64), fpB[:32], "not hex"} {
		_, _, err := pass3Run(t, "accounts", "trust", hostB, "--yes", "--fingerprint", wrong, "-data", dir)
		if err == nil {
			t.Fatalf("fingerprint %q was accepted for another server's key", wrong)
		}
		if ts := trustedIn(t, dir); ts == nil || ts.PublicKey != keyA {
			t.Fatalf("after a refused trust the pin is %+v, want the first server's key", ts)
		}
	}
}

func TestAccountsTrust_FingerprintIsNormalised(t *testing.T) {
	dir := accountsDataDir(t)
	host, key, fp := fakeAccountServer(t, http.StatusOK)
	var spaced []string
	for i := 0; i < len(fp); i += 2 {
		spaced = append(spaced, strings.ToUpper(fp[i:i+2]))
	}
	if _, _, err := pass3Run(t, "accounts", "trust", host, "--yes", "--fingerprint", " "+strings.Join(spaced, ":")+" ", "-data", dir); err != nil {
		t.Fatalf("colon-separated upper-case fingerprint: %v", err)
	}
	if ts := trustedIn(t, dir); ts == nil || ts.PublicKey != key {
		t.Fatalf("trusted = %+v", ts)
	}
}
