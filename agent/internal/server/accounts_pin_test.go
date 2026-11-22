package server

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/ZaidQamhieh/rfe/agent/internal/security"
	"github.com/ZaidQamhieh/rfe/agent/internal/store"
)

// Tests for rfe-7np.2 (S3): pinning an account server over the API needs the owner's password again, so a
// stolen session token alone cannot pin an attacker's key.

const (
	pinTestPassword = "correct horse battery staple" // fake test value
	pinTestToken    = "tok-pin-owner"
	pinSettingsPath = "/v1/settings/account-server"
)

// pinFixture is a router whose only account, "owner", has a real password hash and one logged-in device.
func pinFixture(t *testing.T) (http.Handler, *store.DB) {
	t.Helper()
	h, db := newAccountRouter(t, acctMemberCertFp)
	hash, err := security.HashPassword(pinTestPassword)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.RegisterAccount("owner-dev", "owner phone", pinTestToken, "", "owner", hash); err != nil {
		t.Fatal(err)
	}
	return h, db
}

// pinBody builds a PUT body that pins a fresh key; password is omitted when empty. It returns the body and the
// key's fingerprint.
func pinBody(t *testing.T, password string) (string, string) {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	m := map[string]any{"trustedServer": map[string]string{"name": "srv", "publicKey": base64.StdEncoding.EncodeToString(pub)}}
	if password != "" {
		m["password"] = password
	}
	b, _ := json.Marshal(m)
	return string(b), security.KeyFingerprint(pub)
}

func pinnedFingerprint(t *testing.T, db *store.DB) string {
	t.Helper()
	ts, err := db.TrustedAccountServer()
	if err != nil {
		t.Fatal(err)
	}
	if ts == nil {
		return ""
	}
	return ts.Fingerprint
}

func auditRows(t *testing.T, db *store.DB, action string) []store.AuditEntry {
	t.Helper()
	all, err := db.AuditEntries(100, 0)
	if err != nil {
		t.Fatal(err)
	}
	var out []store.AuditEntry
	for _, e := range all {
		if e.Action == action {
			out = append(out, e)
		}
	}
	return out
}

func TestPinOverAPI_WithoutPasswordIsRefused(t *testing.T) {
	h, db := pinFixture(t)
	body, _ := pinBody(t, "")
	rr := accCall(h, http.MethodPut, pinSettingsPath, pinTestToken, body)
	if rr.Code != http.StatusForbidden || accCode(t, rr) != "CAPABILITY_DENIED" {
		t.Fatalf("pin without password = %d %s, want 403 CAPABILITY_DENIED", rr.Code, rr.Body.String())
	}
	if fp := pinnedFingerprint(t, db); fp != "" {
		t.Fatalf("pin changed to %s without a password", fp)
	}
	if n := len(auditRows(t, db, store.AuditAccountServerPinChanged)); n != 0 {
		t.Fatalf("%d pin_changed audit rows for a refused request", n)
	}
}

func TestPinOverAPI_RefusedRequestChangesNothingElse(t *testing.T) {
	h, db := pinFixture(t)
	pub, _, _ := ed25519.GenerateKey(nil)
	body, _ := json.Marshal(map[string]any{
		"enabled":       true,
		"trustedServer": map[string]string{"name": "srv", "publicKey": base64.StdEncoding.EncodeToString(pub)},
	})
	if rr := accCall(h, http.MethodPut, pinSettingsPath, pinTestToken, string(body)); rr.Code != http.StatusForbidden {
		t.Fatalf("enable+pin without password = %d, want 403", rr.Code)
	}
	if cur, err := db.AccountServer(); err != nil || cur.Enabled {
		t.Fatalf("role was enabled by a refused request: %+v %v", cur, err)
	}
}

func TestPinOverAPI_WrongPasswordIsRefusedAndCounted(t *testing.T) {
	h, db := pinFixture(t)
	body, _ := pinBody(t, "not the password")
	rr := accCall(h, http.MethodPut, pinSettingsPath, pinTestToken, body)
	if rr.Code != http.StatusUnauthorized || accCode(t, rr) != "INVALID_CREDENTIALS" {
		t.Fatalf("pin with wrong password = %d %s, want 401 INVALID_CREDENTIALS", rr.Code, rr.Body.String())
	}
	if fp := pinnedFingerprint(t, db); fp != "" {
		t.Fatalf("pin changed to %s with a wrong password", fp)
	}
	if n := len(auditRows(t, db, store.AuditLoginFailed)); n != 1 {
		t.Fatalf("login_failed rows = %d, want 1", n)
	}

	// The failures share the login budget: once it is used up, even the right password is refused here
	// and at POST /v1/login.
	for i := 1; i < loginAccountFailures; i++ {
		if rr := accCall(h, http.MethodPut, pinSettingsPath, pinTestToken, body); rr.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d = %d, want 401", i+1, rr.Code)
		}
	}
	good, _ := pinBody(t, pinTestPassword)
	if rr := accCall(h, http.MethodPut, pinSettingsPath, pinTestToken, good); rr.Code != http.StatusTooManyRequests {
		t.Fatalf("right password after the budget = %d %s, want 429", rr.Code, rr.Body.String())
	}
	login := accCall(h, http.MethodPost, "/v1/login", "", `{"username":"owner","password":"`+pinTestPassword+`"}`)
	if login.Code != http.StatusTooManyRequests {
		t.Fatalf("login after pin-endpoint failures = %d, want 429 (shared budget)", login.Code)
	}
	if fp := pinnedFingerprint(t, db); fp != "" {
		t.Fatalf("pin changed to %s while locked out", fp)
	}
}

func TestPinOverAPI_RightPasswordPinsAndAudits(t *testing.T) {
	h, db := pinFixture(t)
	body1, fp1 := pinBody(t, pinTestPassword)
	if rr := accCall(h, http.MethodPut, pinSettingsPath, pinTestToken, body1); rr.Code != http.StatusOK {
		t.Fatalf("pin with password = %d %s", rr.Code, rr.Body.String())
	}
	if got := pinnedFingerprint(t, db); got != fp1 {
		t.Fatalf("pinned %q, want %q", got, fp1)
	}
	body2, fp2 := pinBody(t, pinTestPassword)
	if rr := accCall(h, http.MethodPut, pinSettingsPath, pinTestToken, body2); rr.Code != http.StatusOK {
		t.Fatalf("re-pin with password = %d %s", rr.Code, rr.Body.String())
	}
	rows := auditRows(t, db, store.AuditAccountServerPinChanged)
	if len(rows) != 2 {
		t.Fatalf("pin_changed rows = %d, want 2", len(rows))
	}
	// AuditEntries is newest first.
	if d := rows[1].Detail; !strings.Contains(d, fp1) {
		t.Errorf("first row detail %q lacks the new fingerprint %s", d, fp1)
	}
	if d := rows[0].Detail; !strings.Contains(d, fp1) || !strings.Contains(d, fp2) {
		t.Errorf("second row detail %q must carry old %s and new %s", d, fp1, fp2)
	}
	for _, r := range rows {
		if strings.Contains(r.Detail+r.Target+r.Actor, pinTestPassword) {
			t.Fatalf("audit row leaks the password: %+v", r)
		}
	}
}

func TestPinOverAPI_OtherChangesNeedNoPassword(t *testing.T) {
	h, _ := pinFixture(t)
	if rr := accCall(h, http.MethodPut, pinSettingsPath, pinTestToken, `{"enabled":true,"name":"mine"}`); rr.Code != http.StatusOK {
		t.Fatalf("enable without password = %d %s", rr.Code, rr.Body.String())
	}
}
