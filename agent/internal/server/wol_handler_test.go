package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ZaidQamhieh/rfe/agent/internal/store"
)

// TestWolRelayHandler_ReadOnlyDeviceForbidden is the PR-61 regression: a
// guest/read-only paired device must not be able to send Wake-on-LAN.
func TestWolRelayHandler_ReadOnlyDeviceForbidden(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/v1/wol", strings.NewReader(`{"mac":"aa:bb:cc:dd:ee:ff"}`))
	req = req.WithContext(withDevice(req.Context(), &store.Device{ReadOnly: true}))
	rr := httptest.NewRecorder()
	wolRelayHandler(nil)(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for a read-only device, got %d: %s", rr.Code, rr.Body.String())
	}
}

// TestWolRelayHandler_NoDeviceForbidden covers the defensive nil case (no
// device in context at all).
func TestWolRelayHandler_NoDeviceForbidden(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/v1/wol", strings.NewReader(`{"mac":"aa:bb:cc:dd:ee:ff"}`))
	rr := httptest.NewRecorder()
	wolRelayHandler(nil)(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403 with no device in context, got %d: %s", rr.Code, rr.Body.String())
	}
}

// TestWolRelayHandler_FullAccessDeviceProceeds proves the normal, intended
// caller (an ordinary full-access paired phone, not a guest) is unaffected —
// this is a real UDP broadcast, so it asserts on the MAC-parsing behavior
// past the authorization gate rather than dial success.
func TestWolRelayHandler_FullAccessDeviceProceeds(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/v1/wol", strings.NewReader(`{"mac":"not-a-mac"}`))
	req = req.WithContext(withDevice(req.Context(), &store.Device{ReadOnly: false}))
	rr := httptest.NewRecorder()
	wolRelayHandler(nil)(rr, req)

	// Past the authorization gate, an invalid MAC is a 400 — not the 403 a
	// blocked device would get.
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 (invalid MAC, past the auth gate), got %d: %s", rr.Code, rr.Body.String())
	}
}

// Audit F-145: one device may not flood the LAN broadcast.
func TestWolRelayHandler_RateLimitedPerDevice(t *testing.T) {
	handler := wolRelayHandler(nil)
	send := func(id string) int {
		req := httptest.NewRequest(http.MethodPost, "/v1/wol", strings.NewReader(`{"mac":"not-a-mac"}`))
		req = req.WithContext(withDevice(req.Context(), &store.Device{ID: id}))
		rr := httptest.NewRecorder()
		handler(rr, req)
		return rr.Code
	}
	for i := 0; i < wolRateLimitAttempts; i++ {
		if code := send("dev-a"); code != http.StatusBadRequest {
			t.Fatalf("request %d = %d, want 400 (past the limiter)", i, code)
		}
	}
	if code := send("dev-a"); code != http.StatusTooManyRequests {
		t.Fatalf("request over the budget = %d, want 429", code)
	}
	if code := send("dev-b"); code != http.StatusBadRequest {
		t.Fatalf("another device is limited by dev-a: %d", code)
	}
}
