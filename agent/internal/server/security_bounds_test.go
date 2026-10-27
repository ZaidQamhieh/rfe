package server

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDecodeJSONBodyRequiresExactlyOneDocument(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantStatus int
		wantOK     bool
	}{
		{name: "single document", body: `{"name":"rfe"}`, wantStatus: http.StatusOK, wantOK: true},
		{name: "second document", body: `{"name":"rfe"} {"name":"other"}`, wantStatus: http.StatusBadRequest},
		{name: "trailing garbage", body: `{"name":"rfe"} trailing`, wantStatus: http.StatusBadRequest},
		{name: "oversized document", body: `{"name":"` + strings.Repeat("a", maxJSONBody) + `"}`, wantStatus: http.StatusRequestEntityTooLarge},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tc.body))
			rr := httptest.NewRecorder()
			var payload struct {
				Name string `json:"name"`
			}
			if got := decodeJSONBody(rr, req, &payload); got != tc.wantOK {
				t.Fatalf("decodeJSONBody() = %v, want %v", got, tc.wantOK)
			}
			if rr.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", rr.Code, tc.wantStatus, rr.Body.String())
			}
			if tc.wantOK && payload.Name != "rfe" {
				t.Fatalf("decoded name = %q, want rfe", payload.Name)
			}
		})
	}
}

func TestKeyedLimiterCapsActiveKeysAndBoundsOverflowBudget(t *testing.T) {
	limiter := newKeyedLimiter(1, time.Hour)
	limiter.maxKeys = 2

	if !limiter.Allow("source-one") || !limiter.Allow("source-two") {
		t.Fatal("first request from each tracked source should be allowed")
	}
	if !limiter.Allow("overflow-one") {
		t.Fatal("first untracked source should use the overflow budget")
	}
	if limiter.Allow("overflow-one") {
		t.Fatal("an untracked source must still be limited")
	}
	// Untracked sources share a handful of windows by hash, so one exhausted window must not shut out every
	// other newcomer (audit F-06).
	allowed := 0
	for i := 0; i < 200; i++ {
		if limiter.Allow(fmt.Sprintf("newcomer-%d", i)) {
			allowed++
		}
	}
	if allowed < overflowBuckets/2 {
		t.Fatalf("only %d of 200 new sources were allowed; the overflow is one big shared window again", allowed)
	}
	if got := len(limiter.perKey); got != limiter.maxKeys {
		t.Fatalf("tracked source map grew to %d keys, want cap %d", got, limiter.maxKeys)
	}
}

// A key that is sent to an overflow window (the key map is full) is still seen as exhausted.
func TestKeyedLimiterExhaustedSeesOverflowWindows(t *testing.T) {
	k := newKeyedLimiter(2, time.Minute)
	k.maxKeys = 1
	k.Allow("occupies-the-only-slot")
	if k.Exhausted("late") {
		t.Fatal("a fresh key must not be exhausted")
	}
	k.Allow("late")
	k.Allow("late")
	if !k.Exhausted("late") {
		t.Fatal("a key in an overflow window that used its attempts must be exhausted")
	}
}
