package webui

import (
	"net/http/httptest"
	"strings"
	"testing"
)

// A real embedded file must still be served as itself, and any unmatched
// path (client-side route, deep link, browser refresh) must fall back to
// index.html rather than 404 — react-router then renders the matching route.
func TestHandlerSPAFallback(t *testing.T) {
	h := Handler()

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest("GET", "/logo.png", nil))
	if rr.Code != 200 {
		t.Fatalf("real file /logo.png: got status %d, want 200", rr.Code)
	}

	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest("GET", "/app/files", nil))
	if rr.Code != 200 {
		t.Fatalf("client route /app/files: got status %d, want 200 (index.html fallback)", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "<div id=\"root\">") {
		t.Fatalf("client route /app/files: body doesn't look like index.html: %q", rr.Body.String())
	}
	csp := rr.Header().Get("Content-Security-Policy")
	for _, directive := range []string{"script-src 'self'", "frame-src 'none'", "frame-ancestors 'none'", "form-action 'self'", "object-src 'none'"} {
		if !strings.Contains(csp, directive) {
			t.Errorf("CSP is missing %q: %s", directive, csp)
		}
	}
	if rr.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", rr.Header().Get("Cache-Control"))
	}
}
