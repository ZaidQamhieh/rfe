package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ZaidQamhieh/rfe/agent/internal/store"
)

// wolStatus sends a WoL request with an invalid MAC as device d straight to the
// handler. 400 means the request got past authorization; 403 means it was refused.
func wolStatus(t *testing.T, d *store.Device) (int, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/wol", strings.NewReader(`{"mac":"not-a-mac"}`))
	req = req.WithContext(withDevice(req.Context(), d))
	rr := httptest.NewRecorder()
	wolRelayHandler(nil)(rr, req)
	return rr.Code, rr.Body.String()
}

// A low-privilege device (browse-only, the way a new code-paired phone starts)
// and even a device holding every file grant is refused without the wake grant.
func TestWolRelayHandler_WithoutWakeGrantForbidden(t *testing.T) {
	for name, d := range map[string]*store.Device{
		"browse-only":     {ID: "a", CanBrowse: true},
		"all file grants": {ID: "b", CanBrowse: true, CanDownload: true, CanUpload: true, CanModify: true, CanDelete: true, CanShare: true},
	} {
		code, body := wolStatus(t, d)
		if code != http.StatusForbidden {
			t.Fatalf("%s: got %d, want 403: %s", name, code, body)
		}
		var e struct{ Code string }
		if err := json.Unmarshal([]byte(body), &e); err != nil || e.Code != "CAPABILITY_DENIED" {
			t.Fatalf("%s: error body %s, want code CAPABILITY_DENIED (%v)", name, body, err)
		}
	}
}

func TestWolRelayHandler_WithWakeGrantProceeds(t *testing.T) {
	if code, body := wolStatus(t, &store.Device{ID: "a", CanWake: true}); code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400 (invalid MAC, past the grant check): %s", code, body)
	}
}

// Owner (password-login) devices skip per-device grants, as for file grants.
func TestWolRelayHandler_OwnerDeviceBypassesGrant(t *testing.T) {
	if code, body := wolStatus(t, &store.Device{ID: "o", ViaLogin: true}); code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400 (past the grant check): %s", code, body)
	}
}

// The read-only master switch still wins over the wake grant.
func TestWolRelayHandler_ReadOnlyBeatsWakeGrant(t *testing.T) {
	if code, body := wolStatus(t, &store.Device{ID: "r", ReadOnly: true, CanWake: true}); code != http.StatusForbidden {
		t.Fatalf("got %d, want 403: %s", code, body)
	}
}

// End to end through the store: a freshly code-paired device is refused, an
// admin PATCH grants wake, and a second PATCH takes it away again.
func TestWakeGrantAdminPatchFlow(t *testing.T) {
	root := t.TempDir()
	db, st := newTestDepsWithRoots(t, []string{root})
	id, err := db.UpsertDevice("client-w", "phone-w", "tok-w", "", false)
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	adminID, err := db.UpsertDevice("", "admin-session", "tok-admin", "", true)
	if err != nil {
		t.Fatalf("upsert admin: %v", err)
	}
	admin, _ := db.GetDeviceByID(adminID)
	device := func() *store.Device {
		d, err := db.GetDeviceByID(id)
		if err != nil || d == nil {
			t.Fatalf("get: (%+v, %v)", d, err)
		}
		return d
	}
	patch := func(body string) map[string]any {
		req := httptest.NewRequest(http.MethodPatch, "/v1/devices/"+id, strings.NewReader(body))
		req = req.WithContext(withDevice(req.Context(), admin))
		req = withURLParam(req, map[string]string{"id": id})
		rr := httptest.NewRecorder()
		setDeviceJailHandler(db, st)(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("PATCH %s: %d %s", body, rr.Code, rr.Body.String())
		}
		var out map[string]any
		if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return out
	}

	if code, body := wolStatus(t, device()); code != http.StatusForbidden {
		t.Fatalf("new code-paired device: got %d, want 403: %s", code, body)
	}
	if out := patch(`{"wake":true}`); out["wake"] != true {
		t.Fatalf("PATCH response lacks wake=true: %v", out)
	}
	d := device()
	if !d.CanWake || !d.CanBrowse || d.CanDownload {
		t.Fatalf("wake PATCH must set wake only: %+v", d)
	}
	if code, body := wolStatus(t, d); code != http.StatusBadRequest {
		t.Fatalf("granted device: got %d, want 400 (past the gate): %s", code, body)
	}
	// An unrelated file-grant PATCH keeps wake; an explicit false removes it.
	if out := patch(`{"download":true}`); out["wake"] != true || out["download"] != true {
		t.Fatalf("file PATCH changed wake: %v", out)
	}
	if out := patch(`{"wake":false}`); out["wake"] != false {
		t.Fatalf("PATCH response lacks wake=false: %v", out)
	}
	if code, body := wolStatus(t, device()); code != http.StatusForbidden {
		t.Fatalf("after revoking wake: got %d, want 403: %s", code, body)
	}
}

// A non-admin device cannot grant itself wake through the same endpoint.
func TestWakeGrantPatchRefusedForOrdinaryDevice(t *testing.T) {
	root := t.TempDir()
	db, st := newTestDepsWithRoots(t, []string{root})
	id, err := db.UpsertDevice("client-w", "phone-w", "tok-w", "", false)
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}
	self, _ := db.GetDeviceByID(id)
	req := httptest.NewRequest(http.MethodPatch, "/v1/devices/"+id, strings.NewReader(`{"wake":true}`))
	req = req.WithContext(withDevice(req.Context(), self))
	req = withURLParam(req, map[string]string{"id": id})
	rr := httptest.NewRecorder()
	setDeviceJailHandler(db, st)(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("got %d, want 403: %s", rr.Code, rr.Body.String())
	}
	if d, _ := db.GetDeviceByID(id); d == nil || d.CanWake {
		t.Fatalf("wake was granted anyway: %+v", d)
	}
}

func TestFileCapabilitiesJSON_ReportsWake(t *testing.T) {
	if got := fileCapabilitiesJSON(&store.Device{CanBrowse: true}); got["wake"] {
		t.Fatalf("browse-only device must report wake=false: %v", got)
	}
	if got := fileCapabilitiesJSON(&store.Device{CanWake: true}); !got["wake"] {
		t.Fatalf("device with the grant must report wake=true: %v", got)
	}
	if got := fileCapabilitiesJSON(&store.Device{ViaLogin: true}); !got["wake"] {
		t.Fatalf("owner device must report wake=true: %v", got)
	}
}
