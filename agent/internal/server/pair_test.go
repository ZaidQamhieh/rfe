package server

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/pairing"
	"github.com/ZaidQamhieh/rfe/agent/internal/settings"
	"github.com/ZaidQamhieh/rfe/agent/internal/store"
)

// TestPairHandler_BadDeviceProofDoesNotBurnPairingCode verifies a recoverable
// device-proof failure (here, no proof fields at all) does not consume the
// one-time pairing code — the user should be able to retry with the same
// code, no trip back to the terminal required.
func TestPairHandler_BadDeviceProofDoesNotBurnPairingCode(t *testing.T) {
	db, _ := newTestDeps(t)
	pm := pairing.New(db, "127.0.0.1:8765", "", "fingerprint")
	cfg := Config{Name: "test-pc", CertFingerprint: testProofFp}
	handler := pairHandler(cfg, db, pm, newNonceStore())

	code, _, err := pm.Mint(time.Minute)
	if err != nil {
		t.Fatalf("mint: %v", err)
	}

	body := `{"pairingCode":"` + code + `","deviceLabel":"phone"}`
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/pair", strings.NewReader(body))
	handler(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 DEVICE_KEY_REQUIRED, got %d: %s", rr.Code, rr.Body.String())
	}

	if !pm.Consume(code).Valid {
		t.Fatal("pairing code should still be valid after a missing-device-proof rejection")
	}
}

// TestPairHandler_RateLimitedAfterTooManyAttempts verifies /v1/pair returns
// 429 once more than pairRateLimitAttempts requests land within the window,
// regardless of whether the supplied pairing codes are valid.
func TestPairHandler_RateLimitedAfterTooManyAttempts(t *testing.T) {
	db, _ := newTestDeps(t)
	pm := pairing.New(db, "127.0.0.1:8765", "", "fingerprint")
	cfg := Config{Name: "test-pc", CertFingerprint: testProofFp}
	nonces := newNonceStore()
	handler := pairHandler(cfg, db, pm, nonces)

	// First pairRateLimitAttempts requests are not rate-limited (they fail
	// with 401 INVALID_CODE since the code is bogus) — device proof must be
	// valid on each one (a fresh nonce per attempt) so the request actually
	// reaches the pairing-code check instead of failing earlier on proof.
	for i := 0; i < pairRateLimitAttempts; i++ {
		pubKey, nonce, sig := signedDeviceProof(t, nonces)
		body := `{"pairingCode":"WRONGCODE","deviceLabel":"phone",` +
			`"devicePublicKey":"` + pubKey + `","nonce":"` + nonce + `","signature":"` + sig + `"}`
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/v1/pair", strings.NewReader(body))
		handler(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: expected 401, got %d: %s", i+1, rr.Code, rr.Body.String())
		}
	}

	// The next attempt exceeds the limit and should be rejected with 429
	// before even reaching body validation.
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/pair", strings.NewReader(`{"pairingCode":"WRONGCODE","deviceLabel":"phone"}`))
	handler(rr, req)
	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d: %s", rr.Code, rr.Body.String())
	}
	var got apiError
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode error body: %v", err)
	}
	if got.Code != "RATE_LIMITED" {
		t.Fatalf("unexpected error code: %+v", got)
	}
}

func TestPairHandler_InvalidBody(t *testing.T) {
	db, _ := newTestDeps(t)
	pm := pairing.New(db, "127.0.0.1:8765", "", "fingerprint")
	cfg := Config{Name: "test-pc", CertFingerprint: testProofFp}
	handler := pairHandler(cfg, db, pm, newNonceStore())

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/pair", strings.NewReader("not json"))
	handler(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestPairHandler_ValidPairing(t *testing.T) {
	db, _ := newTestDeps(t)
	pm := pairing.New(db, "127.0.0.1:8765", "10.0.0.1", "sha256-fp")
	cfg := Config{
		Name:             "test-pc",
		CertFingerprint:  "sha256-fp",
		Address:          "127.0.0.1:8765",
		TailscaleAddress: "10.0.0.1",
	}
	nonces := newNonceStore()
	handler := pairHandler(cfg, db, pm, nonces)

	code, _, err := pm.Mint(time.Minute)
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	pubKey, nonce, sig := signedDeviceProof(t, nonces)

	body := `{"pairingCode":"` + code + `","deviceLabel":"my-phone","deviceId":"android-123",` +
		`"devicePublicKey":"` + pubKey + `","nonce":"` + nonce + `","signature":"` + sig + `"}`
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/pair", strings.NewReader(body))
	handler(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var resp pairResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.DeviceToken == "" || resp.DeviceID == "" {
		t.Fatalf("expected token+deviceID, got %+v", resp)
	}
	if resp.AgentName != "test-pc" {
		t.Fatalf("expected agentName test-pc, got %s", resp.AgentName)
	}
	paired, err := db.GetDeviceByID(resp.DeviceID)
	if err != nil || paired == nil || !paired.CanBrowse || paired.CanDownload || paired.CanUpload || paired.CanModify || paired.CanDelete || paired.CanShare {
		t.Fatalf("newly paired device should be browse-only: (%+v, %v)", paired, err)
	}
}

// TestGeneratePairingHandler_IncludesScannableQR verifies POST /pairing/generate
// (admin-only) returns a decodable QR PNG alongside the text code, so the web
// companion can offer the same scan-to-pair path as `rfe pair`'s
// terminal QR — previously the endpoint only ever returned the text code.
func TestGeneratePairingHandler_IncludesScannableQR(t *testing.T) {
	db, _ := newTestDeps(t)
	pm := pairing.New(db, "127.0.0.1:8765", "", "sha256-fp")
	st, err := settings.Load(db, false, nil, "test-pc")
	if err != nil {
		t.Fatalf("settings.Load: %v", err)
	}
	handler := generatePairingHandler(pm, st)

	admin := &store.Device{ID: "admin-1", ViaLogin: true}
	req := httptest.NewRequest(http.MethodPost, "/v1/pairing/generate", strings.NewReader(`{}`))
	req = req.WithContext(withDevice(req.Context(), admin))
	rr := httptest.NewRecorder()
	handler(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var resp struct {
		PairingCode string `json:"pairingCode"`
		QRPngBase64 string `json:"qrPngBase64"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.PairingCode == "" {
		t.Fatal("expected non-empty pairingCode")
	}
	png, err := base64.StdEncoding.DecodeString(resp.QRPngBase64)
	if err != nil {
		t.Fatalf("qrPngBase64 did not decode: %v", err)
	}
	if len(png) < 8 || string(png[1:4]) != "PNG" {
		t.Fatalf("qrPngBase64 did not decode to a PNG (got %d bytes)", len(png))
	}
}

// TestFixedWindowLimiter_AllowsBurstThenBlocks exercises the limiter directly,
// including that the window resets over time.
func TestFixedWindowLimiter_AllowsBurstThenBlocks(t *testing.T) {
	l := newFixedWindowLimiter(3, time.Minute)
	now := time.Now()

	for i := 0; i < 3; i++ {
		if !l.allowAt(now) {
			t.Fatalf("attempt %d should be allowed", i+1)
		}
	}
	if l.allowAt(now) {
		t.Fatal("4th attempt within window should be blocked")
	}

	// After the window elapses, attempts are allowed again.
	later := now.Add(time.Minute + time.Second)
	if !l.allowAt(later) {
		t.Fatal("attempt after window should be allowed")
	}
}
