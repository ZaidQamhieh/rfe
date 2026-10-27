package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// F-147: one address that exhausts its register allowance must not lock out other addresses.
func TestRegisterLimiter_IsPerSource(t *testing.T) {
	h := registerHandler(Config{}, nil, nil, nil)
	post := func(addr string) int {
		req := httptest.NewRequest(http.MethodPost, "/v1/register", strings.NewReader("{}"))
		req.RemoteAddr = addr
		rr := httptest.NewRecorder()
		h(rr, req)
		return rr.Code
	}
	limited := false
	for i := 0; i < registerRateLimitAttempts+2; i++ {
		if post("203.0.113.1:1000") == http.StatusTooManyRequests {
			limited = true
		}
	}
	if !limited {
		t.Fatal("the noisy source should hit the limit")
	}
	if code := post("203.0.113.2:1000"); code == http.StatusTooManyRequests {
		t.Fatal("a different source must not be limited by the first one")
	}
}
