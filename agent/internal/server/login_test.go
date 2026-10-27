package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ZaidQamhieh/rfe/agent/internal/security"
)

func TestLoginHandler_InvalidBody(t *testing.T) {
	db, _ := newTestDeps(t)
	cfg := Config{Name: "test-pc", CertFingerprint: testProofFp}
	handler := loginHandler(cfg, db, newNonceStore())

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/login", strings.NewReader("not json"))
	handler(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestLoginHandler_UnknownUsername(t *testing.T) {
	db, _ := newTestDeps(t)
	cfg := Config{Name: "test-pc", CertFingerprint: testProofFp}
	handler := loginHandler(cfg, db, newNonceStore())

	body := `{"username":"nobody","password":"whatever123"}`
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
	if got.Code != "INVALID_CREDENTIALS" {
		t.Fatalf("unexpected error code: %+v", got)
	}
}

func TestLoginHandler_WrongPassword(t *testing.T) {
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

	body := `{"username":"owner","password":"wrong-password"}`
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/login", strings.NewReader(body))
	handler(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestLoginHandler_ValidLogin(t *testing.T) {
	db, _ := newTestDeps(t)
	hash, err := security.HashPassword("correct-horse-battery")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	if err := db.CreateUser("owner", hash); err != nil {
		t.Fatalf("create user: %v", err)
	}
	cfg := Config{
		Name:             "test-pc",
		CertFingerprint:  "sha256-fp",
		Address:          "127.0.0.1:8765",
		TailscaleAddress: "10.0.0.1",
	}
	nonces := newNonceStore()
	handler := loginHandler(cfg, db, nonces)
	pubKey, nonce, sig := signedDeviceProof(t, nonces)

	body := `{"username":"owner","password":"correct-horse-battery","deviceLabel":"my-laptop-browser",` +
		`"devicePublicKey":"` + pubKey + `","nonce":"` + nonce + `","signature":"` + sig + `"}`
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/login", strings.NewReader(body))
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

	// The device is now retrievable exactly like a paired device.
	dev, err := db.DeviceByToken(resp.DeviceToken)
	if err != nil {
		t.Fatalf("DeviceByToken: %v", err)
	}
	if dev == nil || dev.Label != "my-laptop-browser" {
		t.Fatalf("expected device to be created with label, got %+v", dev)
	}
}

func TestLoginHandler_RateLimitedAfterTooManyAttempts(t *testing.T) {
	db, _ := newTestDeps(t)
	cfg := Config{Name: "test-pc", CertFingerprint: testProofFp}
	handler := loginHandler(cfg, db, newNonceStore())

	body := `{"username":"nobody","password":"whatever123"}`
	for i := 0; i < loginRateLimitAttempts; i++ {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/v1/login", strings.NewReader(body))
		handler(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: expected 401, got %d: %s", i+1, rr.Code, rr.Body.String())
		}
	}

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/login", strings.NewReader(body))
	handler(rr, req)
	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d: %s", rr.Code, rr.Body.String())
	}
}

// Guesses spread over many source addresses still run out: the account itself stops answering after enough
// failures, while a different account is unaffected.
func TestLoginHandler_AccountLockoutAcrossAddresses(t *testing.T) {
	db, _ := newTestDeps(t)
	cfg := Config{Name: "test-pc", CertFingerprint: testProofFp}
	handler := loginHandler(cfg, db, newNonceStore())
	post := func(user, ip string) int {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/v1/login", strings.NewReader(`{"username":"`+user+`","password":"wrong-password"}`))
		req.RemoteAddr = ip + ":1234"
		handler(rr, req)
		return rr.Code
	}
	for i := 0; i < loginAccountFailures; i++ {
		if c := post("Victim", fmt.Sprintf("10.0.%d.%d", i/200, i%200+1)); c != http.StatusUnauthorized {
			t.Fatalf("attempt %d: expected 401, got %d", i+1, c)
		}
	}
	if c := post("victim", "10.9.9.9"); c != http.StatusTooManyRequests {
		t.Fatalf("expected the account to be locked (429), got %d", c)
	}
	if c := post("someone-else", "10.9.9.9"); c != http.StatusUnauthorized {
		t.Fatalf("another account must be unaffected, got %d", c)
	}
}
