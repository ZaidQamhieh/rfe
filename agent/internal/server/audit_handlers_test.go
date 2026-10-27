package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ZaidQamhieh/rfe/agent/internal/store"
)

func TestListAuditHandler(t *testing.T) {
	db := auditTestDB(t)
	for _, target := range []string{"dev1", "dev2", "dev3"} {
		if err := db.AppendAudit(store.AuditPair, "phone", target, ""); err != nil {
			t.Fatalf("append: %v", err)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/audit?limit=2", nil)
	rr := httptest.NewRecorder()
	listAuditHandler(db)(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rr.Code)
	}
	var got auditResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Entries) != 2 {
		t.Fatalf("want 2 entries, got %d", len(got.Entries))
	}
	if got.Entries[0].Target != "dev3" {
		t.Fatalf("want newest first, got %q", got.Entries[0].Target)
	}
}

// The audited operation must still succeed end to end, and its entry must
// name the device that caused it.
func TestRevokeDeviceIsAudited(t *testing.T) {
	db, _, _ := newAuthTestDeps(t)
	if err := db.CreateDevice("victim", "old-phone", "tok-victim"); err != nil {
		t.Fatalf("create: %v", err)
	}
	admin := &store.Device{ID: "dev1", Label: "owner-laptop", ViaLogin: true}

	req := httptest.NewRequest(http.MethodDelete, "/v1/devices/victim", nil)
	req = req.WithContext(withDevice(req.Context(), admin))
	rr := httptest.NewRecorder()
	revokeDeviceHandler(db)(rr, req, "victim")

	if rr.Code != http.StatusNoContent {
		t.Fatalf("want 204, got %d", rr.Code)
	}
	entries, err := db.AuditEntries(10, 0)
	if err != nil {
		t.Fatalf("list audit: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("want 1 audit entry, got %d", len(entries))
	}
	if entries[0].Action != store.AuditDeviceRevoked || entries[0].Target != "victim" {
		t.Fatalf("wrong entry: %+v", entries[0])
	}
	if entries[0].Actor != "owner-laptop" {
		t.Fatalf("want the acting device's label, got %q", entries[0].Actor)
	}
}
