package server

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/pairing"
	"github.com/ZaidQamhieh/rfe/agent/internal/security"
)

func TestChallengeHandler_MintsSingleUseNonce(t *testing.T) {
	nonces := newNonceStore()
	handler := challengeHandler(nonces, "")

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/challenge", nil)
	handler(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var resp map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["nonce"] == "" {
		t.Fatal("expected a non-empty nonce")
	}
	if !nonces.Consume(resp["nonce"]) {
		t.Fatal("freshly minted nonce should be consumable once")
	}
	if nonces.Consume(resp["nonce"]) {
		t.Fatal("nonce should not be consumable twice")
	}
}

// TestLoginHandler_DeviceKeyMismatchRejected verifies that a device id
// already pinned to one Ed25519 key can't mint a new token by presenting a
// different key — the point of pinning (see verifyDeviceProof).
func TestLoginHandler_DeviceKeyMismatchRejected(t *testing.T) {
	db, _ := newTestDeps(t)
	hash, err := security.HashPassword("correct-horse-battery")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if err := db.CreateUser("owner", hash); err != nil {
		t.Fatalf("create user: %v", err)
	}
	cfg := Config{Name: "test-pc", CertFingerprint: testProofFp}
	nonces := newNonceStore()
	handler := loginHandler(cfg, db, nonces)

	// First login pins a key to device id "android-123".
	pubKey1, nonce1, sig1 := signedDeviceProof(t, nonces)
	body1 := `{"username":"owner","password":"correct-horse-battery","deviceId":"android-123",` +
		`"devicePublicKey":"` + pubKey1 + `","nonce":"` + nonce1 + `","signature":"` + sig1 + `"}`
	rr1 := httptest.NewRecorder()
	req1 := httptest.NewRequest(http.MethodPost, "/v1/login", strings.NewReader(body1))
	handler(rr1, req1)
	if rr1.Code != http.StatusOK {
		t.Fatalf("first login expected 200, got %d: %s", rr1.Code, rr1.Body.String())
	}

	// A second login for the SAME device id but a DIFFERENT key must be rejected.
	pubKey2, nonce2, sig2 := signedDeviceProof(t, nonces)
	body2 := `{"username":"owner","password":"correct-horse-battery","deviceId":"android-123",` +
		`"devicePublicKey":"` + pubKey2 + `","nonce":"` + nonce2 + `","signature":"` + sig2 + `"}`
	rr2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPost, "/v1/login", strings.NewReader(body2))
	handler(rr2, req2)
	if rr2.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 on key mismatch, got %d: %s", rr2.Code, rr2.Body.String())
	}
	var got apiError
	if err := json.Unmarshal(rr2.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if got.Code != "DEVICE_KEY_MISMATCH" {
		t.Fatalf("unexpected error code: %+v", got)
	}
}

func TestLoginHandler_InvalidSignatureRejected(t *testing.T) {
	db, _ := newTestDeps(t)
	hash, err := security.HashPassword("correct-horse-battery")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if err := db.CreateUser("owner", hash); err != nil {
		t.Fatalf("create user: %v", err)
	}
	cfg := Config{Name: "test-pc", CertFingerprint: testProofFp}
	nonces := newNonceStore()
	handler := loginHandler(cfg, db, nonces)

	pubKey, _, _ := signedDeviceProof(t, nonces)
	nonce, err := nonces.Mint()
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	body := `{"username":"owner","password":"correct-horse-battery",` +
		`"devicePublicKey":"` + pubKey + `","nonce":"` + nonce + `","signature":"bm90LWEtcmVhbC1zaWduYXR1cmU="}`
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/login", strings.NewReader(body))
	handler(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", rr.Code, rr.Body.String())
	}
	var got apiError
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if got.Code != "INVALID_SIGNATURE" {
		t.Fatalf("unexpected error code: %+v", got)
	}
}

func TestLoginHandler_MissingDeviceProofRejected(t *testing.T) {
	db, _ := newTestDeps(t)
	hash, err := security.HashPassword("correct-horse-battery")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if err := db.CreateUser("owner", hash); err != nil {
		t.Fatalf("create user: %v", err)
	}
	cfg := Config{Name: "test-pc", CertFingerprint: testProofFp}
	handler := loginHandler(cfg, db, newNonceStore())

	body := `{"username":"owner","password":"correct-horse-battery"}`
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/login", strings.NewReader(body))
	handler(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
	var got apiError
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if got.Code != "DEVICE_KEY_REQUIRED" {
		t.Fatalf("unexpected error code: %+v", got)
	}
}

// TestPairHandler_KeyChangeRePinsRatherThanRejects verifies pairing (unlike
// login) accepts a device id presenting a *different* key than what's
// pinned — consuming a fresh one-time code is itself a strong trust event
// (physical access to the host), covering a legitimate reinstall that
// generated a new device key. See verifyDeviceProof's enforceKeyPin doc.
func TestPairHandler_KeyChangeRePinsRatherThanRejects(t *testing.T) {
	db, _ := newTestDeps(t)
	pm := pairing.New(db, "127.0.0.1:8765", "", "fingerprint")
	cfg := Config{Name: "test-pc", CertFingerprint: testProofFp}
	nonces := newNonceStore()
	handler := pairHandler(cfg, db, pm, nonces)

	// First pairing pins a key to device id "android-123".
	code1, _, err := pm.Mint(time.Minute)
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	pubKey1, nonce1, sig1 := signedDeviceProof(t, nonces)
	body1 := `{"pairingCode":"` + code1 + `","deviceLabel":"phone","deviceId":"android-123",` +
		`"devicePublicKey":"` + pubKey1 + `","nonce":"` + nonce1 + `","signature":"` + sig1 + `"}`
	rr1 := httptest.NewRecorder()
	req1 := httptest.NewRequest(http.MethodPost, "/v1/pair", strings.NewReader(body1))
	handler(rr1, req1)
	if rr1.Code != http.StatusOK {
		t.Fatalf("first pairing expected 200, got %d: %s", rr1.Code, rr1.Body.String())
	}

	// Re-pairing the SAME device id with a fresh one-time code and a
	// DIFFERENT key (simulating a reinstall) must succeed, re-pinning the
	// new key rather than rejecting.
	code2, _, err := pm.Mint(time.Minute)
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	pubKey2, nonce2, sig2 := signedDeviceProof(t, nonces)
	if pubKey2 == pubKey1 {
		t.Fatal("test setup: expected a different key on the second pairing")
	}
	body2 := `{"pairingCode":"` + code2 + `","deviceLabel":"phone","deviceId":"android-123",` +
		`"devicePublicKey":"` + pubKey2 + `","nonce":"` + nonce2 + `","signature":"` + sig2 + `"}`
	rr2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPost, "/v1/pair", strings.NewReader(body2))
	handler(rr2, req2)
	if rr2.Code != http.StatusOK {
		t.Fatalf("re-pairing with a new key expected 200, got %d: %s", rr2.Code, rr2.Body.String())
	}

	pinned, err := db.DevicePublicKeyByClientID("android-123")
	if err != nil {
		t.Fatalf("DevicePublicKeyByClientID: %v", err)
	}
	if pinned != pubKey2 {
		t.Fatalf("expected the pinned key to be updated to the new key, got %q want %q", pinned, pubKey2)
	}
}

func TestChallengeHandler_AdvertisesTheBoundProof(t *testing.T) {
	rr := httptest.NewRecorder()
	challengeHandler(newNonceStore(), "")(rr, httptest.NewRequest(http.MethodPost, "/v1/auth/challenge", nil))
	var resp map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["proof"] != "v2" {
		t.Fatalf("a client that refuses bare-nonce signatures needs proof=v2, got %q", resp["proof"])
	}
}

// TestVerifyDeviceProof_BoundToThisAgentsCertificate: a v2 signature is only good at the agent whose
// certificate fingerprint it names, so a nonce relayed from a second agent signs nothing useful here.
// A signature over the bare nonce (phone and browser clients) still verifies.
func TestVerifyDeviceProof_BoundToThisAgentsCertificate(t *testing.T) {
	const here = "aa11aa11aa11aa11aa11aa11aa11aa11aa11aa11aa11aa11aa11aa11aa11aa11"
	const elsewhere = "bb22bb22bb22bb22bb22bb22bb22bb22bb22bb22bb22bb22bb22bb22bb22bb22"
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	pubB64 := base64.StdEncoding.EncodeToString(pub)
	sign := func(message string) string {
		return base64.StdEncoding.EncodeToString(ed25519.Sign(priv, []byte(message)))
	}
	cases := []struct {
		name    string
		message func(nonce string) string
		ok      bool
	}{
		{"v2 for this agent", func(n string) string { return security.DeviceProofMessageV2(here, n) }, true},
		{"v2 for another agent", func(n string) string { return security.DeviceProofMessageV2(elsewhere, n) }, false},
		{"bare nonce (browser sign-in only)", func(n string) string { return n }, true},
		{"v2 label without the fingerprint", func(n string) string { return "rfe-device-proof-v2\n\n" + n }, false},
	}
	for _, c := range cases {
		nonces := newNonceStore()
		nonce, err := nonces.Mint()
		if err != nil {
			t.Fatalf("mint: %v", err)
		}
		rr := httptest.NewRecorder()
		err = verifyDeviceProof(nil, nonces, here, "dev", pubB64, nonce, sign(c.message(nonce)), rr, rePinOnKeyChange, acceptLegacyProof)
		if (err == nil) != c.ok {
			t.Errorf("%s: accepted=%v, want %v (%s)", c.name, err == nil, c.ok, rr.Body.String())
		}
	}
}

// Pairing refuses the bare-nonce proof: a relay can pass this agent's nonce to a phone and replay
// the signature, but a v2 signature names the certificate the phone saw.
func TestVerifyDeviceProof_PairingRequiresBoundProof(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	pubB64 := base64.StdEncoding.EncodeToString(pub)
	for _, c := range []struct {
		name string
		msg  func(n string) string
		ok   bool
	}{
		{"v2", func(n string) string { return security.DeviceProofMessageV2(testProofFp, n) }, true},
		{"bare nonce", func(n string) string { return n }, false},
	} {
		nonces := newNonceStore()
		nonce, _ := nonces.Mint()
		sig := base64.StdEncoding.EncodeToString(ed25519.Sign(priv, []byte(c.msg(nonce))))
		rr := httptest.NewRecorder()
		err := verifyDeviceProof(nil, nonces, testProofFp, "dev", pubB64, nonce, sig, rr, rePinOnKeyChange, requireBoundProof)
		if (err == nil) != c.ok {
			t.Errorf("%s: accepted=%v want %v", c.name, err == nil, c.ok)
		}
	}
}

// The browser companion cannot read the certificate it talks to, so the challenge carries the
// fingerprint it must bind its /pair and /register signature to (audit F-168).
func TestChallengeHandler_ReportsCertFingerprint(t *testing.T) {
	rr := httptest.NewRecorder()
	challengeHandler(newNonceStore(), "AB12CD")(rr, httptest.NewRequest(http.MethodPost, "/v1/auth/challenge", nil))
	var resp map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["certFingerprint"] != "ab12cd" || resp["proof"] != "v2" {
		t.Fatalf("challenge = %v", resp)
	}
	want := security.DeviceProofMessageV2("ab12cd", "n")
	if want != "rfe-device-proof-v2\nab12cd\nn" {
		t.Fatalf("message layout changed: %q", want)
	}
}
