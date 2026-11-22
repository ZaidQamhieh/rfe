package api_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/ZaidQamhieh/rfe/agent/internal/next/api"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/apps"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/authz"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/pairing"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/system"
)

func TestMissingInvalidAndRevokedTokenAreUnauthorized(t *testing.T) {
	r := newRig(t, rigOptions{})
	adminTok, _ := r.registerAdmin(newPhone("admin-1", 1))
	tok, id := r.pairDevice(newPhone("phone-1", 2))

	r.do("GET", "/status", "", nil).wantError(t, http.StatusUnauthorized, "UNAUTHORIZED")
	r.do("GET", "/status", "not-a-real-token", nil).wantError(t, http.StatusUnauthorized, "UNAUTHORIZED")
	r.do("GET", "/status", tok, nil, withHeader("Authorization", "Basic "+tok)).wantError(t, http.StatusUnauthorized, "UNAUTHORIZED")
	if res := r.do("GET", "/status", tok, nil); res.Code != http.StatusOK {
		t.Fatalf("valid token = %d %s", res.Code, res.Body.String())
	}
	if res := r.do("DELETE", "/devices/"+id, adminTok, nil); res.Code != http.StatusNoContent {
		t.Fatalf("revoke = %d %s", res.Code, res.Body.String())
	}
	r.do("GET", "/status", tok, nil).wantError(t, http.StatusUnauthorized, "UNAUTHORIZED")
}

func TestGrantsAndAdminTierAreEnforcedBeforeTheHandler(t *testing.T) {
	r := newRig(t, rigOptions{})
	tok, _ := r.pairDevice(newPhone("phone-1", 2)) // browse-only
	r.registerAdmin(newPhone("admin-1", 1))

	r.do("GET", "/apps", tok, nil).wantError(t, http.StatusForbidden, authz.CodeCapabilityDenied)
	r.do("POST", "/apps/"+appID+"/launch", tok, nil).wantError(t, http.StatusForbidden, authz.CodeCapabilityDenied)
	r.do("POST", "/fs/folder", tok, nil).wantError(t, http.StatusForbidden, authz.CodeCapabilityDenied)
	r.do("GET", "/metrics", tok, nil).wantError(t, http.StatusForbidden, authz.CodeForbidden)
	r.do("POST", "/pairing/generate", tok, nil).wantError(t, http.StatusForbidden, authz.CodeForbidden)
	r.do("PATCH", "/devices/x", tok, `{}`).wantError(t, http.StatusForbidden, authz.CodeForbidden)
	// Browse is granted, so the guard lets it through to the not-yet-written handler.
	r.do("GET", "/fs", tok, nil).wantError(t, http.StatusNotImplemented, api.CodeNotImplemented)
}

func TestReadOnlyRefusesWritesGloballyAndPerDevice(t *testing.T) {
	r := newRig(t, rigOptions{})
	adminTok, _ := r.registerAdmin(newPhone("admin-1", 1))
	r.do("POST", "/fs/folder", adminTok, nil).wantError(t, http.StatusNotImplemented, api.CodeNotImplemented)

	r.set.setReadOnly(true)
	r.do("POST", "/fs/folder", adminTok, nil).wantError(t, http.StatusForbidden, authz.CodeReadOnly)
	r.do("GET", "/fs", adminTok, nil).wantError(t, http.StatusNotImplemented, api.CodeNotImplemented) // reads still pass
	r.set.setReadOnly(false)

	tok, id := r.pairDevice(newPhone("phone-1", 2))
	res := r.do("PATCH", "/devices/"+id, adminTok, map[string]any{"modify": true, "readOnly": true})
	if res.Code != http.StatusOK {
		t.Fatalf("patch = %d %s", res.Code, res.Body.String())
	}
	r.do("POST", "/fs/folder", tok, nil).wantError(t, http.StatusForbidden, authz.CodeReadOnly)
	if res := r.do("PATCH", "/devices/"+id, adminTok, map[string]any{"readOnly": false}); res.Code != http.StatusOK {
		t.Fatalf("patch = %d %s", res.Code, res.Body.String())
	}
	r.do("POST", "/fs/folder", tok, nil).wantError(t, http.StatusNotImplemented, api.CodeNotImplemented)
}

func TestErrorsHaveTheContractShapeAndNoStoreHeaders(t *testing.T) {
	r := newRig(t, rigOptions{maxBody: 64})
	cases := []struct {
		name   string
		res    result
		status int
		code   string
	}{
		{"unknown route", r.do("GET", "/nope", "", nil), 404, api.CodeNotFound},
		{"wrong method", r.do("DELETE", "/health", "", nil), 405, api.CodeBadRequest},
		{"bad json", r.do("POST", "/pair", "", "{not json"), 400, api.CodeBadRequest},
		{"empty body", r.do("POST", "/login", "", nil), 400, api.CodeBadRequest},
		{"trailing data", r.do("POST", "/login", "", `{} {}`), 400, api.CodeBadRequest},
		{"too large", r.do("POST", "/pair", "", `{"pairingCode":"`+strings.Repeat("x", 200)+`"}`), 413, api.CodePayloadTooLarge},
		{"unauthorized", r.do("GET", "/status", "", nil), 401, authz.CodeUnauthorized},
		{"guard before not-implemented", r.do("GET", "/search", "", nil), 401, authz.CodeUnauthorized},
	}
	for _, c := range cases {
		c.res.wantError(t, c.status, c.code)
		if got := c.res.Header().Get("Content-Type"); got != "application/json" {
			t.Errorf("%s: Content-Type = %q", c.name, got)
		}
		if got := c.res.Header().Get("Cache-Control"); got != "no-store" {
			t.Errorf("%s: Cache-Control = %q", c.name, got)
		}
		if strings.Contains(c.res.Body.String(), "/") && c.name != "unknown route" {
			t.Errorf("%s: answer looks like it holds a path: %s", c.name, c.res.Body.String())
		}
	}
	if got := cases[1].res.Header().Get("Allow"); got != "GET" {
		t.Errorf("405 Allow = %q, want GET", got)
	}
}

func TestEveryUnimplementedRouteAnswers501AfterTheGuard(t *testing.T) {
	r := newRig(t, rigOptions{})
	adminTok, _ := r.registerAdmin(newPhone("admin-1", 1))
	count := 0
	for _, rt := range api.Routes {
		key := rt.Method + " " + rt.Pattern
		if _, ok := api.NotYetImplemented[key]; !ok {
			continue
		}
		count++
		path := strings.NewReplacer("{token}", "x", "{id}", "x", "{n}", "1", "{username}", "x", "{tokenHash}", "x").Replace(rt.Pattern)
		if rt.Tier != api.TierPublic {
			r.do(rt.Method, path, "", nil).wantError(t, http.StatusUnauthorized, authz.CodeUnauthorized)
		}
		res := r.do(rt.Method, path, adminTok, nil)
		res.wantError(t, http.StatusNotImplemented, api.CodeNotImplemented)
	}
	if count != len(api.NotYetImplemented) {
		t.Fatalf("exercised %d of %d unimplemented routes", count, len(api.NotYetImplemented))
	}
}

func TestAPanicIsRecoveredAndLeaksNothing(t *testing.T) {
	r := newRig(t, rigOptions{})
	res := r.do("GET", "/status", panicToken, nil)
	res.wantError(t, http.StatusInternalServerError, api.CodeInternal)
	if strings.Contains(res.Body.String(), "boom") || strings.Contains(res.Body.String(), hostPath) {
		t.Fatalf("panic text reached the client: %s", res.Body.String())
	}
	if !strings.Contains(r.logs.String(), "boom") {
		t.Fatalf("the panic was not logged: %q", r.logs.String())
	}
	if res := r.do("GET", "/health", "", nil); res.Code != http.StatusOK {
		t.Fatalf("the server is unusable after a panic: %d", res.Code)
	}
}

func TestHealthLeaksNothingToAnUnpairedCaller(t *testing.T) {
	r := newRig(t, rigOptions{})
	tok, _ := r.pairDevice(newPhone("phone-1", 2))
	for _, token := range []string{"", "wrong-token"} {
		res := r.do("GET", "/health", token, nil)
		if res.Code != http.StatusOK {
			t.Fatalf("health = %d", res.Code)
		}
		var m map[string]any
		res.json(t, &m)
		if len(m) != 1 || m["status"] != system.HealthOK {
			t.Fatalf("an unpaired caller got %s", res.Body.String())
		}
		for _, secret := range []string{testFingerprint, testName, testVersion, "192.0.2", "aa:bb"} {
			if strings.Contains(res.Body.String(), secret) {
				t.Fatalf("health leaked %q: %s", secret, res.Body.String())
			}
		}
	}
	var m map[string]any
	r.do("GET", "/health", tok, nil).json(t, &m)
	if m["name"] != testName || m["version"] != testVersion || m["apiVersion"] != float64(testAPIVer) || m["address"] != testAddress {
		t.Fatalf("an authenticated caller got %v", m)
	}
	if _, leaked := m["fingerprint"]; leaked {
		t.Fatalf("health must not carry the fingerprint: %v", m)
	}
}

func TestStatusMetricsAndDeviceList(t *testing.T) {
	r := newRig(t, rigOptions{})
	adminTok, adminID := r.registerAdmin(newPhone("admin-1", 1))
	tok, id := r.pairDevice(newPhone("phone-1", 2))

	var st system.Status
	res := r.do("GET", "/status", tok, nil)
	res.json(t, &st)
	if res.Code != 200 || st.Version != testVersion || st.Fingerprint != testFingerprint || st.APIVersion != testAPIVer || st.TotalBytes != 100 || st.Counts["devices"] != 2 {
		t.Fatalf("status = %d %+v", res.Code, st)
	}
	var m system.Metrics
	if res := r.do("GET", "/metrics", adminTok, nil); res.Code != 200 {
		t.Fatalf("metrics = %d %s", res.Code, res.Body.String())
	} else {
		res.json(t, &m)
	}

	var all []api.DeviceJSON
	r.do("GET", "/devices", adminTok, nil).json(t, &all)
	if len(all) != 2 {
		t.Fatalf("admin list = %+v", all)
	}
	for _, d := range all {
		if d.Current != (d.ID == adminID) || d.ViaLogin != (d.ID == adminID) {
			t.Errorf("device %+v has the wrong current/viaLogin flags", d)
		}
	}
	res = r.do("GET", "/devices", tok, nil)
	var own []map[string]any
	res.json(t, &own)
	if len(own) != 1 || own[0]["id"] != id || own[0]["current"] != true {
		t.Fatalf("ordinary list = %s", res.Body.String())
	}
	for _, hidden := range []string{"lastAddress", "lastVersion", "jailRoot", "revoked", "viaLogin"} {
		if _, ok := own[0][hidden]; ok {
			t.Errorf("an ordinary device sees %q", hidden)
		}
	}
}

func TestDeviceDeleteRules(t *testing.T) {
	r := newRig(t, rigOptions{})
	adminTok, _ := r.registerAdmin(newPhone("admin-1", 1))
	aTok, aID := r.pairDevice(newPhone("phone-a", 2))
	_, bID := r.pairDevice(newPhone("phone-b", 3))

	r.do("DELETE", "/devices/"+bID, aTok, nil).wantError(t, http.StatusForbidden, authz.CodeForbidden)
	r.do("DELETE", "/devices/"+bID+"?purge=maybe", adminTok, nil).wantError(t, http.StatusBadRequest, api.CodeBadRequest)
	r.do("DELETE", "/devices/nope", adminTok, nil).wantError(t, http.StatusNotFound, api.CodeNotFound)
	if res := r.do("DELETE", "/devices/"+bID+"?purge=true", adminTok, nil); res.Code != 204 {
		t.Fatalf("purge = %d %s", res.Code, res.Body.String())
	}
	if res := r.do("DELETE", "/devices/"+aID, aTok, nil); res.Code != 204 { // a device may revoke itself
		t.Fatalf("self revoke = %d %s", res.Code, res.Body.String())
	}
	r.do("GET", "/status", aTok, nil).wantError(t, http.StatusUnauthorized, authz.CodeUnauthorized)
	var all []api.DeviceJSON
	r.do("GET", "/devices", adminTok, nil).json(t, &all)
	if len(all) != 2 {
		t.Fatalf("after purge and revoke the list is %+v", all)
	}
}

func TestAppsListLaunchAndPatchValidation(t *testing.T) {
	r := newRig(t, rigOptions{})
	adminTok, _ := r.registerAdmin(newPhone("admin-1", 1))
	tok, id := r.pairDevice(newPhone("phone-1", 2))

	// Launching without viewing is refused as a whole: nothing is changed.
	r.do("PATCH", "/devices/"+id, adminTok, map[string]any{"launchApps": true, "download": true}).wantError(t, http.StatusBadRequest, api.CodeBadRequest)
	r.do("GET", "/apps", tok, nil).wantError(t, http.StatusForbidden, authz.CodeCapabilityDenied)
	var d api.DeviceJSON
	if res := r.do("PATCH", "/devices/"+id, adminTok, map[string]any{"viewApps": true}); res.Code != 200 {
		t.Fatalf("patch = %d %s", res.Code, res.Body.String())
	} else {
		res.json(t, &d)
	}
	if d.Download || !d.ViewApps || d.LaunchApps || !d.Browse {
		t.Fatalf("a rejected patch changed something, or the accepted one set the wrong fields: %+v", d)
	}

	var cat apps.Catalog
	res := r.do("GET", "/apps", tok, nil)
	res.json(t, &cat)
	if res.Code != 200 || len(cat.Apps) != 2 || cat.LaunchAllowed || cat.Platform != apps.PlatformLinux {
		t.Fatalf("catalog = %d %s", res.Code, res.Body.String())
	}
	r.do("POST", "/apps/"+appID+"/launch", tok, nil).wantError(t, http.StatusForbidden, authz.CodeCapabilityDenied)

	if res := r.do("PATCH", "/devices/"+id, adminTok, map[string]any{"launchApps": true}); res.Code != 200 {
		t.Fatalf("patch = %d %s", res.Code, res.Body.String())
	}
	res = r.do("POST", "/apps/"+appID+"/launch", tok, nil)
	if res.Code != http.StatusAccepted || strings.TrimSpace(res.Body.String()) != `{"status":"started"}` {
		t.Fatalf("launch = %d %s", res.Code, res.Body.String())
	}
	if got := r.launcher.launched(); len(got) != 1 || got[0] != "Editor" {
		t.Fatalf("launched %v", got)
	}
	r.do("POST", "/apps/not-an-id/launch", tok, nil).wantError(t, http.StatusBadRequest, apps.CodeBadAppID)
	r.do("POST", "/apps/"+goneAppID+"/launch", tok, nil).wantError(t, http.StatusNotFound, apps.CodeNotFound)
	r.do("POST", "/apps/"+viewOnlyAppID+"/launch", tok, nil).wantError(t, http.StatusConflict, apps.CodeNotLaunchable)
}

func TestPairingGenerateChecksTheJailAndHidesHostPaths(t *testing.T) {
	var asked []string
	r := newRig(t, rigOptions{validateJail: func(root string) (string, error) {
		asked = append(asked, root)
		if root != "/ok" {
			return "", &pathError{hostPath + "/nested"}
		}
		return root, nil
	}})
	adminTok, _ := r.registerAdmin(newPhone("admin-1", 1))
	_, id := r.pairDevice(newPhone("phone-1", 2))

	res := r.do("POST", "/pairing/generate", adminTok, nil)
	var out struct {
		PairingCode      string `json:"pairingCode"`
		ExpiresInSeconds int64  `json:"expiresInSeconds"`
		QRPayload        struct {
			CertFingerprint string `json:"certFingerprint"`
			PairingCode     string `json:"pairingCode"`
		} `json:"qrPayload"`
	}
	res.json(t, &out)
	if res.Code != 200 || out.PairingCode == "" || out.ExpiresInSeconds <= 0 || out.QRPayload.PairingCode != out.PairingCode || out.QRPayload.CertFingerprint != testFingerprint {
		t.Fatalf("generate = %d %s", res.Code, res.Body.String())
	}
	if res := r.do("POST", "/pairing/generate", adminTok, map[string]any{"guest": true, "jailRoot": "/ok"}); res.Code != 200 {
		t.Fatalf("guest generate = %d %s", res.Code, res.Body.String())
	}
	bad := r.do("POST", "/pairing/generate", adminTok, map[string]any{"guest": true, "jailRoot": "/etc"})
	bad.wantError(t, http.StatusBadRequest, api.CodeBadRequest)
	patch := r.do("PATCH", "/devices/"+id, adminTok, map[string]any{"jailRoot": "/etc"})
	patch.wantError(t, http.StatusBadRequest, api.CodeBadRequest)
	for _, res := range []result{bad, patch} {
		if strings.Contains(res.Body.String(), hostPath) {
			t.Fatalf("a host path reached the client: %s", res.Body.String())
		}
	}
	if res := r.do("PATCH", "/devices/"+id, adminTok, map[string]any{"jailRoot": "/ok"}); res.Code != 200 {
		t.Fatalf("jail patch = %d %s", res.Code, res.Body.String())
	}
	if res := r.do("PATCH", "/devices/"+id, adminTok, map[string]any{"jailRoot": ""}); res.Code != 200 {
		t.Fatalf("jail clear = %d %s", res.Code, res.Body.String())
	}
	if len(asked) != 4 { // the empty jail is a clear, never validated
		t.Fatalf("validator calls = %v", asked)
	}
}

// pathError is a validator failure whose own text names a host folder.
type pathError struct{ path string }

// Error returns the host path, as a real filesystem error would.
func (e *pathError) Error() string { return "stat " + e.path + ": no such file" }

func TestGuestJailWithoutAValidatorIsRefused(t *testing.T) {
	r := newRig(t, rigOptions{})
	adminTok, _ := r.registerAdmin(newPhone("admin-1", 1))
	r.do("POST", "/pairing/generate", adminTok, map[string]any{"guest": true, "jailRoot": "/x"}).wantError(t, http.StatusBadRequest, api.CodeBadRequest)
	r.do("POST", "/pairing/generate", adminTok, "{broken").wantError(t, http.StatusBadRequest, api.CodeBadRequest)
}

func TestLogoutIsAnEmpty204(t *testing.T) {
	r := newRig(t, rigOptions{})
	if res := r.do("POST", "/auth/logout", "", nil); res.Code != http.StatusNoContent || res.Body.Len() != 0 {
		t.Fatalf("logout = %d %q", res.Code, res.Body.String())
	}
}

func TestTypedServiceErrorsKeepTheirOwnCodeAndStatus(t *testing.T) {
	for _, e := range []*pairing.Error{pairing.ErrPairBusy, pairing.ErrInvalidSignature, pairing.ErrAccountExists, pairing.ErrDeviceKeyRequired, pairing.ErrDeviceNotFound} {
		if m := api.Map(e); m.Code != e.Code || m.Status != e.Status || m.Message != e.Message || m.Internal {
			t.Errorf("%s: Map = %+v", e.Code, m)
		}
	}
	for _, e := range []*apps.Error{apps.ErrAppNotFound, apps.ErrAppNotLaunchable, apps.ErrLaunchBusy, apps.ErrNoInteractiveUser, apps.ErrBadAppID} {
		wrapped := wrap(e)
		if m := api.Map(wrapped); m.Code != e.Code || m.Status != e.Status || m.Message != e.Message || m.Internal {
			t.Errorf("%s: Map(wrapped) = %+v", e.Code, m)
		}
	}
	// An error with a nonsense status or no code is not trusted.
	for _, e := range []error{&apps.Error{Code: "X", Status: 200, Message: "m"}, &pairing.Error{Status: 400, Message: "m"}} {
		if m := api.Map(e); !m.Internal || m.Code != api.CodeInternal || m.Message != api.InternalMessage {
			t.Errorf("untrusted %v mapped to %+v", e, m)
		}
	}
	if got := api.TypedCodes(); len(got) < 20 {
		t.Errorf("TypedCodes lists only %d codes", len(got))
	}
}

// wrap adds context around err, as a service layer would.
func wrap(err error) error {
	return &wrapped{err}
}

type wrapped struct{ err error }

// Error returns the context text and the inner error's text.
func (w *wrapped) Error() string { return "context: " + w.err.Error() }

// Unwrap returns the inner error.
func (w *wrapped) Unwrap() error { return w.err }
