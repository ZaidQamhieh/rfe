package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ZaidQamhieh/rfe/agent/internal/store"
	"github.com/go-chi/chi/v5"
)

func appRequestWithID(method, id string) *http.Request {
	req := httptest.NewRequest(method, "/v1/apps/"+id+"/launch", nil)
	route := chi.NewRouteContext()
	route.URLParams.Add("id", id)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, route))
}

func TestListAppsHandlerRequiresCatalogGrant(t *testing.T) {
	for _, tc := range []struct {
		name   string
		device *store.Device
	}{
		{name: "no device"},
		{name: "launch-only", device: &store.Device{LaunchApps: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/v1/apps", nil)
			if tc.device != nil {
				req = req.WithContext(withDevice(req.Context(), tc.device))
			}
			rr := httptest.NewRecorder()
			listAppsHandler()(rr, req)
			if rr.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want 403; body=%s", rr.Code, rr.Body.String())
			}
		})
	}
}

func TestLaunchAppHandlerRequiresBothGrantsAndAuditsDenial(t *testing.T) {
	db, _ := newTestDeps(t)
	for _, tc := range []struct {
		name   string
		device *store.Device
	}{
		{name: "no device"},
		{name: "catalog only", device: &store.Device{ID: "catalog-only", ViewApps: true}},
		{name: "launch only", device: &store.Device{ID: "launch-only", LaunchApps: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := appRequestWithID(http.MethodPost, "app_"+strings.Repeat("a", 64))
			if tc.device != nil {
				req = req.WithContext(withDevice(req.Context(), tc.device))
			}
			rr := httptest.NewRecorder()
			launchAppHandler(db)(rr, req)
			if rr.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want 403; body=%s", rr.Code, rr.Body.String())
			}
		})
	}
	entries, err := db.AuditEntries(10, 0)
	if err != nil {
		t.Fatalf("read audit: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("got %d denial audit entries, want 3", len(entries))
	}
	for _, entry := range entries {
		if entry.Action != store.AuditAppLaunch || !strings.Contains(entry.Detail, "outcome=permission_denied") {
			t.Errorf("unexpected audit entry: %+v", entry)
		}
	}
}

func TestLaunchAppHandlerRejectsAndHashesMalformedID(t *testing.T) {
	db, _ := newTestDeps(t)
	device := &store.Device{ID: "bad-id-client", ViewApps: true, LaunchApps: true}
	const supplied = "../../var/log/messages"
	req := appRequestWithID(http.MethodPost, supplied)
	req = req.WithContext(withDevice(req.Context(), device))
	rr := httptest.NewRecorder()
	launchAppHandler(db)(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rr.Code, rr.Body.String())
	}
	entries, err := db.AuditEntries(10, 0)
	if err != nil {
		t.Fatalf("read audit: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("got %d audit entries, want 1", len(entries))
	}
	if entries[0].Target == supplied || !strings.HasPrefix(entries[0].Target, "invalid_") || len(entries[0].Target) != len("invalid_")+16 {
		t.Fatalf("malformed route text was not safely summarized: %q", entries[0].Target)
	}
	if entries[0].Detail != "outcome=invalid_id" {
		t.Fatalf("detail = %q, want invalid_id outcome", entries[0].Detail)
	}
}

func TestAppIDAndDisplaySanitizers(t *testing.T) {
	valid := "app_" + strings.Repeat("f", 64)
	if got, ok := canonicalAppID(valid); !ok || got != valid {
		t.Fatalf("canonical valid ID = (%q,%v)", got, ok)
	}
	for _, invalid := range []string{"", "app_" + strings.Repeat("A", 64), valid + "x", "../app"} {
		if got, ok := canonicalAppID(invalid); ok || got != "" {
			t.Errorf("canonicalAppID(%q) = (%q,%v), want empty/false", invalid, got, ok)
		}
	}
	if got := auditAppID(valid, true); got != valid {
		t.Fatalf("valid audit ID changed: %q", got)
	}
	if got := auditAppID("line\nbreak", false); got == "line\nbreak" || !strings.HasPrefix(got, "invalid_") {
		t.Fatalf("invalid audit ID is not opaque: %q", got)
	}
	if got := safeAppDisplay("  Demo/\x00\\App\x7f Name  "); got != "Demo  App Name" {
		t.Fatalf("safe app display = %q", got)
	}
	long := strings.Repeat("界", 400)
	if got := safeAppDisplay(long); len([]rune(got)) > 240 {
		t.Fatalf("sanitized display has %d runes, want at most 240", len([]rune(got)))
	}
}
