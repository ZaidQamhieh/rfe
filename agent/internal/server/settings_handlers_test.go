package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZaidQamhieh/rfe/agent/internal/fsops"
	"github.com/ZaidQamhieh/rfe/agent/internal/settings"
	"github.com/ZaidQamhieh/rfe/agent/internal/store"
)

// asAdmin attaches an admin (via_login) device so a request passes the
// settings/bandwidth admin gate.
func asAdmin(r *http.Request) *http.Request {
	return r.WithContext(withDevice(r.Context(), &store.Device{ViaLogin: true}))
}

func newTestDeps(t *testing.T) (*store.DB, *settings.Store) {
	t.Helper()
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	st, err := settings.Load(db, false, nil, "test-pc")
	if err != nil {
		t.Fatalf("settings: %v", err)
	}
	return db, st
}

// newTestDepsWithRoots is like newTestDeps but seeds the agent's configured
// global roots, for tests that need to validate a jailRoot against them.
func newTestDepsWithRoots(t *testing.T, roots []string) (*store.DB, *settings.Store) {
	t.Helper()
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	st, err := settings.Load(db, false, roots, "test-pc")
	if err != nil {
		t.Fatalf("settings: %v", err)
	}
	return db, st
}

func TestSettingsHandler_GetAndPatch(t *testing.T) {
	_, st := newTestDeps(t)

	// GET reflects defaults.
	rr := httptest.NewRecorder()
	getSettingsHandler(st)(rr, asAdmin(httptest.NewRequest(http.MethodGet, "/v1/settings", nil)))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET code = %d", rr.Code)
	}
	var got map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &got)
	if got["readOnly"] != false || got["agentName"] != "test-pc" || got["photoBackupRoot"] != "" {
		t.Fatalf("unexpected GET body: %v", got)
	}

	// PATCH toggles read-only, renames, and sets the photo-backup root.
	body := `{"readOnly":true,"agentName":"new-name","photoBackupRoot":"/home/pc/PhoneBackups"}`
	rr2 := httptest.NewRecorder()
	patchSettingsHandler(st)(rr2, asAdmin(httptest.NewRequest(http.MethodPatch, "/v1/settings", strings.NewReader(body))))
	if rr2.Code != http.StatusOK {
		t.Fatalf("PATCH code = %d", rr2.Code)
	}
	if !st.IsReadOnly() || st.AgentName() != "new-name" || st.PhotoBackupRoot() != "/home/pc/PhoneBackups" {
		t.Fatalf("settings not applied: ro=%v name=%s photoBackupRoot=%s", st.IsReadOnly(), st.AgentName(), st.PhotoBackupRoot())
	}
}

func TestDevicesHandler_ListAndRevoke(t *testing.T) {
	db, _ := newTestDeps(t)
	_ = db.CreateDevice("id-keep", "keeper", "tok-keep")
	_ = db.CreateDevice("id-gone", "gone", "tok-gone")
	_ = db.TouchDevice("id-keep", "192.168.1.42", "1.10.0+18")

	// Keep an ordinary paired device for self-revoke coverage.
	cur, _ := db.DeviceByToken("tok-keep")
	adminID, err := db.UpsertDevice("", "admin-session", "tok-admin", "", true)
	if err != nil {
		t.Fatalf("upsert admin: %v", err)
	}
	admin, _ := db.GetDeviceByID(adminID)

	// An admin can list all devices; its own row is marked current.
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/devices", nil)
	req = req.WithContext(withDevice(req.Context(), admin))
	listDevicesHandler(db)(rr, req)
	var list []map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &list)
	if len(list) != 3 {
		t.Fatalf("expected 3 devices, got %d", len(list))
	}

	// The keeper's row reflects its recorded address/version; the untouched
	// device's row has the empty-string defaults.
	var keeper, goneRow, adminRow map[string]any
	for _, d := range list {
		switch d["id"] {
		case "id-keep":
			keeper = d
		case "id-gone":
			goneRow = d
		case adminID:
			adminRow = d
		}
	}
	if adminRow == nil || adminRow["current"] != true {
		t.Fatalf("expected admin device to be marked current, got %v", adminRow)
	}
	if keeper == nil || keeper["lastAddress"] != "192.168.1.42" || keeper["lastVersion"] != "1.10.0+18" {
		t.Fatalf("expected keeper lastAddress/lastVersion recorded, got %v", keeper)
	}
	if goneRow == nil || goneRow["lastAddress"] != "" || goneRow["lastVersion"] != "" {
		t.Fatalf("expected gone device to have empty lastAddress/lastVersion, got %v", goneRow)
	}

	// Revoking SELF succeeds (204) — a device managing itself is the only
	// permitted target.
	rrSelf := httptest.NewRecorder()
	reqSelf := httptest.NewRequest(http.MethodDelete, "/v1/devices/id-keep", nil)
	reqSelf = reqSelf.WithContext(withDevice(reqSelf.Context(), cur))
	revokeDeviceHandler(db)(rrSelf, reqSelf, "id-keep")
	if rrSelf.Code != http.StatusNoContent {
		t.Fatalf("expected 204 on self-revoke, got %d: %s", rrSelf.Code, rrSelf.Body.String())
	}
	keptDev, _ := db.GetDeviceByID("id-keep")
	if keptDev == nil || !keptDev.Revoked {
		t.Fatal("expected id-keep to be revoked")
	}

	// Revoking ANOTHER device is rejected (403 FORBIDDEN) — managing other
	// devices must be done on the PC.
	rrOther := httptest.NewRecorder()
	reqOther := httptest.NewRequest(http.MethodDelete, "/v1/devices/id-gone", nil)
	reqOther = reqOther.WithContext(withDevice(reqOther.Context(), cur))
	revokeDeviceHandler(db)(rrOther, reqOther, "id-gone")
	if rrOther.Code != http.StatusForbidden {
		t.Fatalf("expected 403 revoking other, got %d: %s", rrOther.Code, rrOther.Body.String())
	}
	var gotErr apiError
	_ = json.Unmarshal(rrOther.Body.Bytes(), &gotErr)
	if gotErr.Code != "FORBIDDEN" {
		t.Fatalf("expected FORBIDDEN error code, got %+v", gotErr)
	}
	gone, _ := db.DeviceByToken("tok-gone")
	if gone == nil || gone.Revoked {
		t.Fatal("expected id-gone to remain unrevoked")
	}

	// An admin device (via_login=true) MAY revoke another device.
	rrAdmin := httptest.NewRecorder()
	reqAdmin := httptest.NewRequest(http.MethodDelete, "/v1/devices/id-gone", nil)
	reqAdmin = reqAdmin.WithContext(withDevice(reqAdmin.Context(), admin))
	revokeDeviceHandler(db)(rrAdmin, reqAdmin, "id-gone")
	if rrAdmin.Code != http.StatusNoContent {
		t.Fatalf("expected 204 on admin revoking other, got %d: %s", rrAdmin.Code, rrAdmin.Body.String())
	}
	goneAfterAdmin, _ := db.GetDeviceByID("id-gone")
	if goneAfterAdmin == nil || !goneAfterAdmin.Revoked {
		t.Fatal("expected id-gone to be revoked by admin device")
	}
}

// TestDeleteDeviceHandler_SelfOnly verifies DELETE /v1/devices/{id}?purge=true
// (deleteDeviceHandler): the caller's own device id is the only permitted
// target — self succeeds with 204 and the row is gone, any other id is
// rejected with 403 FORBIDDEN and the row is left untouched.
func TestDeleteDeviceHandler_SelfOnly(t *testing.T) {
	db, _ := newTestDeps(t)
	_ = db.CreateDevice("id-self", "self", "tok-self")
	_ = db.CreateDevice("id-other", "other", "tok-other")
	cur, _ := db.DeviceByToken("tok-self")

	// Purging another device is forbidden.
	rrOther := httptest.NewRecorder()
	reqOther := httptest.NewRequest(http.MethodDelete, "/v1/devices/id-other?purge=true", nil)
	reqOther = reqOther.WithContext(withDevice(reqOther.Context(), cur))
	deleteDeviceHandler(db)(rrOther, reqOther, "id-other")
	if rrOther.Code != http.StatusForbidden {
		t.Fatalf("expected 403 purging other, got %d: %s", rrOther.Code, rrOther.Body.String())
	}
	var gotErr apiError
	_ = json.Unmarshal(rrOther.Body.Bytes(), &gotErr)
	if gotErr.Code != "FORBIDDEN" {
		t.Fatalf("expected FORBIDDEN error code, got %+v", gotErr)
	}
	if other, _ := db.GetDeviceByID("id-other"); other == nil {
		t.Fatal("expected id-other to remain")
	}

	// Purging self succeeds and the row is gone.
	rrSelf := httptest.NewRecorder()
	reqSelf := httptest.NewRequest(http.MethodDelete, "/v1/devices/id-self?purge=true", nil)
	reqSelf = reqSelf.WithContext(withDevice(reqSelf.Context(), cur))
	deleteDeviceHandler(db)(rrSelf, reqSelf, "id-self")
	if rrSelf.Code != http.StatusNoContent {
		t.Fatalf("expected 204 on self-purge, got %d: %s", rrSelf.Code, rrSelf.Body.String())
	}
	if self, _ := db.GetDeviceByID("id-self"); self != nil {
		t.Fatal("expected id-self to be deleted")
	}
}

// TestSetDeviceJailHandler_AdminOnly verifies PATCH /v1/devices/{id}: an
// ordinary (non-admin, i.e. paired via /pair) caller gets 403 FORBIDDEN even
// targeting itself, while an admin device (authenticated via /login or
// /register — see isAdminDevice) can set another device's jailRoot/readOnly.
func TestSetDeviceJailHandler_AdminOnly(t *testing.T) {
	root := t.TempDir()
	db, st := newTestDepsWithRoots(t, []string{root})
	if err := db.CreateDevice("id-1", "phone-a", "tok-a"); err != nil {
		t.Fatalf("create: %v", err)
	}
	nonAdmin, _ := db.DeviceByToken("tok-a")

	req := httptest.NewRequest(http.MethodPatch, "/v1/devices/id-1", strings.NewReader(`{"jailRoot":""}`))
	req = req.WithContext(withDevice(req.Context(), nonAdmin))
	req = withURLParam(req, map[string]string{"id": "id-1"})
	rr := httptest.NewRecorder()
	setDeviceJailHandler(db, st)(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-admin caller, got %d: %s", rr.Code, rr.Body.String())
	}

	// An admin device (via_login=true) can set another device's jailRoot.
	adminID, err := db.UpsertDevice("", "admin-session", "tok-admin", "", true)
	if err != nil {
		t.Fatalf("upsert admin: %v", err)
	}
	admin, _ := db.GetDeviceByID(adminID)

	reqAdmin := httptest.NewRequest(http.MethodPatch, "/v1/devices/id-1", strings.NewReader(`{"jailRoot":"`+root+`","readOnly":true}`))
	reqAdmin = reqAdmin.WithContext(withDevice(reqAdmin.Context(), admin))
	reqAdmin = withURLParam(reqAdmin, map[string]string{"id": "id-1"})
	rrAdmin := httptest.NewRecorder()
	setDeviceJailHandler(db, st)(rrAdmin, reqAdmin)
	if rrAdmin.Code != http.StatusOK {
		t.Fatalf("expected 200 for admin caller, got %d: %s", rrAdmin.Code, rrAdmin.Body.String())
	}
	d, err := db.GetDeviceByID("id-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if d == nil || d.JailRoot != filepath.Clean(root) || !d.ReadOnly {
		t.Fatalf("expected jailRoot/readOnly applied, got %+v", d)
	}

	// An invalid jailRoot (outside the global roots) is rejected with 400 and
	// leaves the device untouched.
	reqBad := httptest.NewRequest(http.MethodPatch, "/v1/devices/id-1", strings.NewReader(`{"jailRoot":"`+t.TempDir()+`"}`))
	reqBad = reqBad.WithContext(withDevice(reqBad.Context(), admin))
	reqBad = withURLParam(reqBad, map[string]string{"id": "id-1"})
	rrBad := httptest.NewRecorder()
	setDeviceJailHandler(db, st)(rrBad, reqBad)
	if rrBad.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for jailRoot outside global roots, got %d: %s", rrBad.Code, rrBad.Body.String())
	}
}

// TestValidateJailRoot covers the validation rules extracted from the old
// setDeviceJailHandler, now shared with the `rfe jail` admin CLI
// command via ValidateJailRoot/SetDeviceJail.
func TestValidateJailRoot(t *testing.T) {
	t.Run("empty clears", func(t *testing.T) {
		got, err := ValidateJailRoot("", []string{"/some/root"}, false)
		if err != nil || got != "" {
			t.Fatalf("expected (\"\", nil), got (%q, %v)", got, err)
		}
	})

	t.Run("relative path rejected", func(t *testing.T) {
		_, err := ValidateJailRoot("relative/path", nil, false)
		if err == nil {
			t.Fatal("expected error for relative path")
		}
	})

	t.Run("outside global roots rejected", func(t *testing.T) {
		root := t.TempDir()
		outside := t.TempDir()
		_, err := ValidateJailRoot(outside, []string{root}, false)
		if err == nil {
			t.Fatal("expected error for jailRoot outside global roots")
		}
	})

	t.Run("inside global roots accepted and cleaned", func(t *testing.T) {
		root := t.TempDir()
		sub := filepath.Join(root, "shared")
		if err := os.MkdirAll(sub, 0o755); err != nil {
			t.Fatalf("mkdir sub: %v", err)
		}
		got, err := ValidateJailRoot(sub, []string{root}, false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != filepath.Clean(sub) {
			t.Fatalf("expected %q, got %q", filepath.Clean(sub), got)
		}
	})

	t.Run("no global roots allows any absolute path", func(t *testing.T) {
		anyAbs := t.TempDir()
		got, err := ValidateJailRoot(anyAbs, nil, false)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != filepath.Clean(anyAbs) {
			t.Fatalf("expected %q, got %q", filepath.Clean(anyAbs), got)
		}
	})
}

// TestSetDeviceJail verifies SetDeviceJail validates via ValidateJailRoot and
// persists the cleaned jailRoot (or rejects without persisting on invalid
// input) — the logic the `rfe jail` admin CLI command calls.
func TestSetDeviceJail(t *testing.T) {
	root := t.TempDir()
	db, _ := newTestDepsWithRoots(t, []string{root})
	if err := db.CreateDevice("id-1", "phone-a", "tok-a"); err != nil {
		t.Fatalf("create: %v", err)
	}

	sub := filepath.Join(root, "shared")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("mkdir sub: %v", err)
	}

	// Set a valid jail within the global root.
	got, err := SetDeviceJail(db, "id-1", sub, []string{root}, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != filepath.Clean(sub) {
		t.Fatalf("expected %q, got %q", filepath.Clean(sub), got)
	}
	d, err := db.GetDeviceByID("id-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if d == nil || d.JailRoot != filepath.Clean(sub) {
		t.Fatalf("expected persisted JailRoot %q, got %+v", sub, d)
	}

	// Clear it.
	got2, err := SetDeviceJail(db, "id-1", "", []string{root}, false)
	if err != nil {
		t.Fatalf("unexpected error clearing: %v", err)
	}
	if got2 != "" {
		t.Fatalf("expected cleared jailRoot, got %q", got2)
	}
	d2, err := db.GetDeviceByID("id-1")
	if err != nil {
		t.Fatalf("get after clear: %v", err)
	}
	if d2 == nil || d2.JailRoot != "" {
		t.Fatalf("expected persisted JailRoot cleared, got %+v", d2)
	}

	// Invalid (outside roots) is rejected without persisting.
	outside := t.TempDir()
	if _, err := SetDeviceJail(db, "id-1", outside, []string{root}, false); err == nil {
		t.Fatal("expected error for jailRoot outside global roots")
	}
	d3, err := db.GetDeviceByID("id-1")
	if err != nil {
		t.Fatalf("get after rejected set: %v", err)
	}
	if d3 == nil || d3.JailRoot != "" {
		t.Fatalf("expected jailRoot to remain empty after rejection, got %+v", d3)
	}
}

// TestDeviceJailMiddleware_NarrowsOpsForJailedDevice verifies the
// post-auth middleware: a device with a non-empty JailRoot gets a
// per-request *fsops.Ops (via opsCtxKey/opsFromContext) narrowed to its
// jail, so a downstream handler resolving a path outside that jail (but
// inside the agent's global root) is rejected with FORBIDDEN — while a
// device with NO jailRoot continues to use the shared base ops unchanged.
func TestDeviceJailMiddleware_NarrowsOpsForJailedDevice(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "jailed")
	sibling := filepath.Join(root, "sibling")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("mkdir sub: %v", err)
	}
	if err := os.MkdirAll(sibling, 0o755); err != nil {
		t.Fatalf("mkdir sibling: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sibling, "secret.txt"), []byte("secret"), 0o644); err != nil {
		t.Fatalf("write secret: %v", err)
	}

	db, st := newTestDepsWithRoots(t, []string{root})
	baseOps := fsops.NewWithSettings(st)

	// A jailed device, confined to root/jailed.
	if err := db.CreateDevice("id-jailed", "jailed-phone", "tok-jailed"); err != nil {
		t.Fatalf("create jailed: %v", err)
	}
	if err := db.SetDeviceJail("id-jailed", sub); err != nil {
		t.Fatalf("set jail: %v", err)
	}
	jailedDevice, err := db.DeviceByToken("tok-jailed")
	if err != nil || jailedDevice == nil {
		t.Fatalf("device by token: %v", err)
	}

	// An unrestricted device.
	if err := db.CreateDevice("id-open", "open-phone", "tok-open"); err != nil {
		t.Fatalf("create open: %v", err)
	}
	openDevice, err := db.DeviceByToken("tok-open")
	if err != nil || openDevice == nil {
		t.Fatalf("device by token: %v", err)
	}

	// downstream is the handler under test: it resolves "path" through the
	// per-request ops from context, reporting FORBIDDEN vs OK.
	downstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		effectiveOps := opsFromContext(r.Context(), baseOps)
		path := r.URL.Query().Get("path")
		if _, err := effectiveOps.Resolve(path); err != nil {
			writeError(w, http.StatusForbidden, "FORBIDDEN", err.Error())
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	chain := deviceJailMiddleware(baseOps)(downstream)

	// Jailed device: a sibling path (inside the global root, outside the
	// device jail) is FORBIDDEN.
	req := httptest.NewRequest(http.MethodGet, "/v1/fs?path="+filepath.Join(sibling, "secret.txt"), nil)
	req = req.WithContext(withDevice(req.Context(), jailedDevice))
	rr := httptest.NewRecorder()
	chain.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for jailed device accessing sibling path, got %d: %s", rr.Code, rr.Body.String())
	}

	// Jailed device: a path inside its own jail is OK.
	req2 := httptest.NewRequest(http.MethodGet, "/v1/fs?path="+filepath.Join(sub, "file.txt"), nil)
	req2 = req2.WithContext(withDevice(req2.Context(), jailedDevice))
	rr2 := httptest.NewRecorder()
	chain.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusOK {
		t.Fatalf("expected 200 for jailed device accessing its own jail, got %d: %s", rr2.Code, rr2.Body.String())
	}

	// Unrestricted device: the same sibling path is OK (no regression).
	req3 := httptest.NewRequest(http.MethodGet, "/v1/fs?path="+filepath.Join(sibling, "secret.txt"), nil)
	req3 = req3.WithContext(withDevice(req3.Context(), openDevice))
	rr3 := httptest.NewRecorder()
	chain.ServeHTTP(rr3, req3)
	if rr3.Code != http.StatusOK {
		t.Fatalf("expected 200 for unrestricted device accessing sibling path, got %d: %s", rr3.Code, rr3.Body.String())
	}
}

func TestPatchSettingsHandler_InvalidJSON(t *testing.T) {
	_, st := newTestDeps(t)
	rr := httptest.NewRecorder()
	patchSettingsHandler(st)(rr, asAdmin(httptest.NewRequest(http.MethodPatch, "/v1/settings", strings.NewReader("not json"))))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

// TestPatchSettingsHandler_NonAdminForbidden is the PR-02 regression: an
// ordinary paired (non-admin) device must not be able to change global policy.
func TestPatchSettingsHandler_NonAdminForbidden(t *testing.T) {
	_, st := newTestDeps(t)
	body := `{"readOnly":false}`

	// No device / paired device (ViaLogin=false) → 403, and read-only unchanged.
	_ = st.SetReadOnly(true)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPatch, "/v1/settings", strings.NewReader(body))
	req = req.WithContext(withDevice(req.Context(), &store.Device{ViaLogin: false}))
	patchSettingsHandler(st)(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-admin, got %d", rr.Code)
	}
	if !st.IsReadOnly() {
		t.Fatalf("non-admin patch changed global read-only policy")
	}

	rr2 := httptest.NewRecorder()
	putBandwidthHandler(st)(rr2, httptest.NewRequest(http.MethodPut, "/v1/settings/bandwidth", strings.NewReader(`{"maxUploadBytesPerSec":1}`)))
	if rr2.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for non-admin bandwidth, got %d", rr2.Code)
	}
}

func TestPatchSettingsHandler_SetRoots(t *testing.T) {
	_, st := newTestDeps(t)
	root := t.TempDir()
	body := `{"roots":["` + root + `"]}`
	rr := httptest.NewRecorder()
	patchSettingsHandler(st)(rr, asAdmin(httptest.NewRequest(http.MethodPatch, "/v1/settings", strings.NewReader(body))))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	roots := st.Roots()
	if len(roots) != 1 || roots[0] != root {
		t.Fatalf("expected roots [%s], got %v", root, roots)
	}
}

func TestPutBandwidthHandler_InvalidJSON(t *testing.T) {
	_, st := newTestDeps(t)
	rr := httptest.NewRecorder()
	putBandwidthHandler(st)(rr, asAdmin(httptest.NewRequest(http.MethodPut, "/v1/settings/bandwidth", strings.NewReader("{bad"))))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestBandwidthHandler_GetAndPut(t *testing.T) {
	_, st := newTestDeps(t)

	// GET defaults to zero (unlimited).
	rr := httptest.NewRecorder()
	getBandwidthHandler(st)(rr, asAdmin(httptest.NewRequest(http.MethodGet, "/v1/settings/bandwidth", nil)))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET code = %d", rr.Code)
	}
	var got map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &got)
	if got["maxUploadBytesPerSec"] != float64(0) || got["maxDownloadBytesPerSec"] != float64(0) {
		t.Fatalf("unexpected GET body: %v", got)
	}

	// PUT sets limits.
	body := `{"maxUploadBytesPerSec":1000000,"maxDownloadBytesPerSec":5000000}`
	rr2 := httptest.NewRecorder()
	putBandwidthHandler(st)(rr2, asAdmin(httptest.NewRequest(http.MethodPut, "/v1/settings/bandwidth", strings.NewReader(body))))
	if rr2.Code != http.StatusOK {
		t.Fatalf("PUT code = %d: %s", rr2.Code, rr2.Body.String())
	}
	var got2 map[string]any
	_ = json.Unmarshal(rr2.Body.Bytes(), &got2)
	if got2["maxUploadBytesPerSec"] != float64(1000000) || got2["maxDownloadBytesPerSec"] != float64(5000000) {
		t.Fatalf("unexpected PUT response: %v", got2)
	}
	if st.MaxUploadBytesPerSec() != 1000000 || st.MaxDownloadBytesPerSec() != 5000000 {
		t.Fatalf("settings not applied")
	}

	// PUT with partial body only updates specified fields.
	body2 := `{"maxUploadBytesPerSec":0}`
	rr3 := httptest.NewRecorder()
	putBandwidthHandler(st)(rr3, asAdmin(httptest.NewRequest(http.MethodPut, "/v1/settings/bandwidth", strings.NewReader(body2))))
	if rr3.Code != http.StatusOK {
		t.Fatalf("PUT partial code = %d", rr3.Code)
	}
	if st.MaxUploadBytesPerSec() != 0 {
		t.Fatalf("expected upload reset to 0, got %d", st.MaxUploadBytesPerSec())
	}
	if st.MaxDownloadBytesPerSec() != 5000000 {
		t.Fatalf("expected download unchanged at 5000000, got %d", st.MaxDownloadBytesPerSec())
	}
}

func TestFileCapabilitiesJSON(t *testing.T) {
	got := fileCapabilitiesJSON(&store.Device{CanBrowse: true, CanUpload: true})
	want := map[string]bool{"browse": true, "download": false, "upload": true, "modify": false, "delete": false, "share": false}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("%s: got %v want %v (%v)", k, got[k], v, got)
		}
	}
	for k, v := range fileCapabilitiesJSON(&store.Device{ViaLogin: true}) {
		if !v {
			t.Fatalf("owner device must report %s true", k)
		}
	}
	for _, v := range fileCapabilitiesJSON(nil) {
		if v {
			t.Fatal("no device means nothing allowed")
		}
	}
}
