package server

import (
	"crypto/ed25519"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ZaidQamhieh/rfe/agent/internal/store"
)

// Regression tests for the 2026-11-15 account-server security review (F1 share serving, F2 federated
// sessions, F3 assertion device key, F6 audit).

// fedSession logs a device in through an account-server assertion and returns its bearer token.
func fedSession(t *testing.T, f *federation) string {
	t.Helper()
	p := f.memberProof(t)
	rr := f.login(f.sign(t, f.claims(p)), p)
	if rr.Code != http.StatusOK {
		t.Fatalf("federated login = %d: %s", rr.Code, rr.Body.String())
	}
	tok, _ := accJSON(t, rr)["deviceToken"].(string)
	if tok == "" {
		t.Fatal("no deviceToken")
	}
	return tok
}

// ---- F1: a share link whose minting device is gone or revoked is not served ----

func TestServeShare_RefusesTokenOfMissingDevice(t *testing.T) {
	ops, root := newFsFixture(t)
	db, _ := newTestDeps(t)
	live, err := db.UpsertDevice("c-live", "live", "tok-live", "", true)
	if err != nil {
		t.Fatal(err)
	}
	exp := time.Now().Add(time.Hour)
	file := filepath.Join(root, "a.txt")
	if err := db.CreateShareToken(hashShareToken("live-token"), file, live, exp); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateShareToken(hashShareToken("ghost-token"), file, "no-such-device", exp); err != nil {
		t.Fatal(err)
	}
	r := chi.NewRouter()
	r.Get("/v1/share/{token}", serveShareHandler(db, ops, func() bool { return true }))
	get := func(tok string) int {
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/v1/share/"+tok, nil))
		return rr.Code
	}
	if code := get("ghost-token"); code != http.StatusNotFound {
		t.Fatalf("token of a missing device served: %d", code)
	}
	if code := get("live-token"); code != http.StatusOK {
		t.Fatalf("control: token of a live device = %d, want 200", code)
	}
}

// ---- F2: a federated session cannot administer the host ----

func TestFederatedSession_CannotAdministerTheHost(t *testing.T) {
	f := newFederation(t)
	tok := fedSession(t, f)
	otherID, err := f.memberDB.UpsertDevice("other", "other", "tok-other", "", false)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ method, path, body string }{
		{http.MethodPost, "/v1/pairing/generate", "{}"},
		{http.MethodGet, "/v1/pair/requests", ""},
		{http.MethodPost, "/v1/pair/requests/x/approve", "{}"},
		{http.MethodPost, "/v1/pair/requests/x/reject", "{}"},
		{http.MethodPatch, "/v1/devices/" + otherID, `{"jailRoot":"/tmp","readOnly":true,"upload":true}`},
		{http.MethodDelete, "/v1/devices/" + otherID, ""},
		{http.MethodDelete, "/v1/devices/" + otherID + "?purge=true", ""},
		{http.MethodPatch, "/v1/settings", `{"readOnly":true}`},
		{http.MethodGet, "/v1/users", ""},
		{http.MethodDelete, "/v1/users/alice", ""},
		{http.MethodPut, "/v1/settings/account-server", `{"enabled":true}`},
		{http.MethodDelete, "/v1/settings/account-server", ""},
	} {
		rr := accCall(f.member, c.method, c.path, tok, c.body)
		if rr.Code != http.StatusForbidden {
			t.Errorf("%s %s as a federated session = %d, want 403: %s", c.method, c.path, rr.Code, rr.Body.String())
		}
	}
	if d, _ := f.memberDB.GetDeviceByID(otherID); d == nil || d.Revoked || d.ReadOnly {
		t.Fatalf("the other device was changed: %+v", d)
	}
}

// The restriction is only for federated sessions: a password session still administers, and a federated
// session keeps using the files its grants allow.
func TestFederatedSession_KeepsFileAccessAndPasswordAdminUnchanged(t *testing.T) {
	f := newFederation(t)
	tok := fedSession(t, f)
	if rr := accCall(f.member, http.MethodGet, "/v1/settings", tok, ""); rr.Code != http.StatusOK {
		t.Fatalf("federated GET /settings = %d", rr.Code)
	}
	if rr := accCall(f.member, http.MethodGet, "/v1/devices", tok, ""); rr.Code != http.StatusOK {
		t.Fatalf("federated GET /devices = %d", rr.Code)
	}
	if _, err := f.memberDB.RegisterAccount("pw-dev", "pw", "tok-pw", "", "bob", "hash"); err != nil {
		t.Fatal(err)
	}
	if rr := accCall(f.member, http.MethodPost, "/v1/pairing/generate", "tok-pw", "{}"); rr.Code != http.StatusOK {
		t.Fatalf("password-session pairing/generate = %d: %s", rr.Code, rr.Body.String())
	}
	if rr := accCall(f.member, http.MethodGet, "/v1/users", "tok-pw", ""); rr.Code != http.StatusOK {
		t.Fatalf("password-session GET /users = %d", rr.Code)
	}
}

// ---- F3: an assertion is only issued for the calling device's own key ----

func TestAssertionIssue_RefusesAnotherDevicesKey(t *testing.T) {
	f := newFederation(t)
	p := f.memberProof(t)
	otherPub, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	other := base64.StdEncoding.EncodeToString(otherPub)
	rr := accCall(f.srv, http.MethodPost, "/v1/accounts/assertion", f.adminToken, issueBody(acctMemberCertFp, other, p.nonce))
	if rr.Code != http.StatusForbidden || accCode(t, rr) != "FORBIDDEN" {
		t.Fatalf("assertion for another device's key = %d %s", rr.Code, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), "assertion\":") {
		t.Fatal("an assertion came back for a key the caller does not hold")
	}
}

func TestAssertionIssue_RefusesDeviceWithoutPinnedKey(t *testing.T) {
	f := newFederation(t)
	if _, err := f.srvDB.LoginDevice("nokey-dev", "nokey", "tok-nokey", "", "alice", "hash"); err != nil {
		t.Fatal(err)
	}
	p := f.memberProof(t)
	rr := accCall(f.srv, http.MethodPost, "/v1/accounts/assertion", "tok-nokey", issueBody(acctMemberCertFp, p.pub, p.nonce))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("device with no pinned key got %d: %s", rr.Code, rr.Body.String())
	}
}

// ---- F6: audit ----

func auditEntries(t *testing.T, db *store.DB) []store.AuditEntry {
	t.Helper()
	entries, err := db.AuditEntries(200, 0)
	if err != nil {
		t.Fatal(err)
	}
	return entries
}

func TestFederatedLogin_BadDeviceProofIsAudited(t *testing.T) {
	f := newFederation(t)
	p := f.memberProof(t)
	tok := f.sign(t, f.claims(p))
	p.sig = base64.StdEncoding.EncodeToString(make([]byte, ed25519.SignatureSize))
	if rr := f.login(tok, p); rr.Code != http.StatusUnauthorized {
		t.Fatalf("login = %d", rr.Code)
	}
	want := "alice@" + f.keyFp[:12]
	for _, e := range auditEntries(t, f.memberDB) {
		if e.Action == store.AuditLoginFailed && e.Actor == want && strings.Contains(e.Detail, "device proof") {
			return
		}
	}
	t.Fatalf("no login_failed audit row from %s for the failed device proof", want)
}

func TestFederatedLogin_AuditActorNamesIssuer(t *testing.T) {
	f := newFederation(t)
	fedSession(t, f)
	want := "alice@" + f.keyFp[:12]
	for _, e := range auditEntries(t, f.memberDB) {
		if e.Action == store.AuditLogin && e.Actor == want {
			return
		}
	}
	t.Fatalf("no login audit row with actor %q", want)
}
