package server

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/pairing"
	"github.com/ZaidQamhieh/rfe/agent/internal/security"
	"github.com/ZaidQamhieh/rfe/agent/internal/store"
	"github.com/ZaidQamhieh/rfe/agent/internal/transfer"
)

// Fake, obviously non-secret certificate fingerprints (lowercase hex SHA-256 shape).
const (
	acctServerCertFp = "aa11aa11aa11aa11aa11aa11aa11aa11aa11aa11aa11aa11aa11aa11aa11aa11"
	acctMemberCertFp = "bb22bb22bb22bb22bb22bb22bb22bb22bb22bb22bb22bb22bb22bb22bb22bb22"
)

// newAccountRouter builds the real router (same wiring as production) with a certificate fingerprint.
func newAccountRouter(t *testing.T, certFp string) (http.Handler, *store.DB) {
	t.Helper()
	db, st := newTestDeps(t)
	tm, err := transfer.New(db, t.TempDir())
	if err != nil {
		t.Fatalf("transfer.New: %v", err)
	}
	h, err := New(Config{
		Name: "acct-test", CertFingerprint: certFp, Settings: st, TrashDir: t.TempDir(),
		ThumbCacheDir: t.TempDir(), UpdatesDir: t.TempDir(), StartTime: time.Now(), DataDir: t.TempDir(),
	}, db, pairing.New(db, "127.0.0.1", "", certFp), tm)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return h, db
}

func accCall(h http.Handler, method, path, token, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

func accJSON(t *testing.T, rr *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &m); err != nil {
		t.Fatalf("decode %q: %v", rr.Body.String(), err)
	}
	return m
}

func accCode(t *testing.T, rr *httptest.ResponseRecorder) string {
	t.Helper()
	code, _ := accJSON(t, rr)["code"].(string)
	return code
}

// federation is an account server (with account "alice" and an admin token) and a member agent that trusts it.
type federation struct {
	srv, member     http.Handler
	srvDB, memberDB *store.DB
	pubB64, keyFp   string
	adminToken      string
	// devPub/devPriv are the key of the account server's admin device and of the phone that logs into the
	// member agent: assertions are only issued for the key the calling device has pinned.
	devPub  string
	devPriv ed25519.PrivateKey
}

const srvAdminToken = "tok-srv-admin"

func newFederation(t *testing.T) *federation {
	t.Helper()
	f := &federation{adminToken: srvAdminToken}
	f.srv, f.srvDB = newAccountRouter(t, acctServerCertFp)
	f.member, f.memberDB = newAccountRouter(t, acctMemberCertFp)
	devPub, devPriv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	f.devPub, f.devPriv = base64.StdEncoding.EncodeToString(devPub), devPriv
	if _, err := f.srvDB.RegisterAccount("admin-dev", "admin", srvAdminToken, f.devPub, "alice", "hash"); err != nil {
		t.Fatalf("RegisterAccount: %v", err)
	}
	if err := f.srvDB.SetAccountServer(true, "home server"); err != nil {
		t.Fatalf("SetAccountServer: %v", err)
	}
	priv, err := f.srvDB.AccountSigningKey()
	if err != nil {
		t.Fatalf("AccountSigningKey: %v", err)
	}
	pub := priv.Public().(ed25519.PublicKey)
	f.pubB64 = base64.StdEncoding.EncodeToString(pub)
	f.keyFp = security.KeyFingerprint(pub)
	if _, err := f.memberDB.SetTrustedAccountServer("home server", f.pubB64); err != nil {
		t.Fatalf("SetTrustedAccountServer: %v", err)
	}
	return f
}

// proof is the member-side device-identity proof: the device key and a challenge nonce it signed.
type proof struct{ pub, nonce, sig string }

func (f *federation) memberProof(t *testing.T) proof {
	t.Helper()
	rr := accCall(f.member, http.MethodPost, "/v1/auth/challenge", "", "")
	nonce, _ := accJSON(t, rr)["nonce"].(string)
	if rr.Code != http.StatusOK || nonce == "" {
		t.Fatalf("challenge: %d %s", rr.Code, rr.Body.String())
	}
	sig := ed25519.Sign(f.devPriv, []byte(security.DeviceProofMessageV2(acctMemberCertFp, nonce)))
	return proof{f.devPub, nonce, base64.StdEncoding.EncodeToString(sig)}
}

func (f *federation) claims(p proof) security.AssertionClaims {
	return security.AssertionClaims{
		Iss: f.keyFp, Sub: "alice", Aud: acctMemberCertFp, DevicePublicKey: p.pub, Nonce: p.nonce,
		Exp: time.Now().Add(time.Minute).Unix(),
	}
}

func (f *federation) sign(t *testing.T, c security.AssertionClaims) string {
	t.Helper()
	priv, err := f.srvDB.AccountSigningKey()
	if err != nil {
		t.Fatal(err)
	}
	tok, err := security.SignAssertion(priv, c)
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

func (f *federation) loginBody(assertion string, p proof) string {
	b, _ := json.Marshal(map[string]string{
		"assertion": assertion, "deviceLabel": "fed-phone", "deviceId": "fed-client",
		"devicePublicKey": p.pub, "nonce": p.nonce, "signature": p.sig,
	})
	return string(b)
}

func (f *federation) login(assertion string, p proof) *httptest.ResponseRecorder {
	return accCall(f.member, http.MethodPost, "/v1/login", "", f.loginBody(assertion, p))
}

func (f *federation) expectRejected(t *testing.T, rr *httptest.ResponseRecorder) {
	t.Helper()
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401: %s", rr.Code, rr.Body.String())
	}
	if code := accCode(t, rr); code != "INVALID_CREDENTIALS" {
		t.Fatalf("code = %q, want INVALID_CREDENTIALS", code)
	}
	if strings.Contains(rr.Body.String(), "deviceToken") {
		t.Fatal("a token came back with a rejection")
	}
}

// ---- key endpoint ----

func TestAccountsKey_PublicWhenEnabled(t *testing.T) {
	f := newFederation(t)
	rr := accCall(f.srv, http.MethodGet, "/v1/accounts/key", "", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rr.Code, rr.Body.String())
	}
	m := accJSON(t, rr)
	if m["publicKey"] != f.pubB64 || m["keyFingerprint"] != f.keyFp || m["name"] != "home server" || m["algorithm"] != "Ed25519" {
		t.Fatalf("key response = %v", m)
	}
	if strings.Contains(rr.Body.String(), "BEGIN") || strings.Contains(strings.ToLower(rr.Body.String()), "private") {
		t.Fatal("key response mentions private key material")
	}
}

func TestAccountsKey_DisabledIs403(t *testing.T) {
	h, _ := newAccountRouter(t, acctServerCertFp)
	rr := accCall(h, http.MethodGet, "/v1/accounts/key", "", "")
	if rr.Code != http.StatusForbidden || accCode(t, rr) != "ACCOUNT_SERVER_DISABLED" {
		t.Fatalf("got %d %s", rr.Code, rr.Body.String())
	}
}

// ---- assertion issuing ----

func issueBody(aud, devPub, nonce string) string {
	b, _ := json.Marshal(map[string]string{"audience": aud, "devicePublicKey": devPub, "memberNonce": nonce})
	return string(b)
}

func TestAssertionIssue_SignedShortLivedClaims(t *testing.T) {
	f := newFederation(t)
	p := f.memberProof(t)
	before := time.Now()
	rr := accCall(f.srv, http.MethodPost, "/v1/accounts/assertion", f.adminToken, issueBody(acctMemberCertFp, p.pub, p.nonce))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rr.Code, rr.Body.String())
	}
	tok, _ := accJSON(t, rr)["assertion"].(string)
	priv, _ := f.srvDB.AccountSigningKey()
	got, err := security.VerifyAssertionSignature(priv.Public().(ed25519.PublicKey), tok)
	if err != nil {
		t.Fatalf("issued assertion does not verify: %v", err)
	}
	want := security.AssertionClaims{Iss: f.keyFp, Sub: "alice", Aud: acctMemberCertFp, DevicePublicKey: p.pub, Nonce: p.nonce, Exp: got.Exp}
	if got != want {
		t.Fatalf("claims = %+v, want %+v", got, want)
	}
	if got.Exp <= before.Unix() || got.Exp > before.Add(security.AssertionMaxTTL).Unix() {
		t.Fatalf("exp %d is not within (now, now+%v]", got.Exp, security.AssertionMaxTTL)
	}
}

func TestAssertionIssue_DisabledIs403(t *testing.T) {
	f := newFederation(t)
	if err := f.srvDB.SetAccountServer(false, ""); err != nil {
		t.Fatal(err)
	}
	rr := accCall(f.srv, http.MethodPost, "/v1/accounts/assertion", f.adminToken, issueBody(acctMemberCertFp, "x", "n"))
	if rr.Code != http.StatusForbidden || accCode(t, rr) != "ACCOUNT_SERVER_DISABLED" {
		t.Fatalf("got %d %s", rr.Code, rr.Body.String())
	}
}

func TestAssertionIssue_CodePairedDeviceForbidden(t *testing.T) {
	f := newFederation(t)
	if _, err := f.srvDB.UpsertDevice("plain", "plain", "tok-plain", "", false); err != nil {
		t.Fatal(err)
	}
	p := f.memberProof(t)
	rr := accCall(f.srv, http.MethodPost, "/v1/accounts/assertion", "tok-plain", issueBody(acctMemberCertFp, p.pub, p.nonce))
	if rr.Code != http.StatusForbidden || accCode(t, rr) != "FORBIDDEN" {
		t.Fatalf("got %d %s", rr.Code, rr.Body.String())
	}
}

func TestAssertionIssue_RequiresToken(t *testing.T) {
	f := newFederation(t)
	if rr := accCall(f.srv, http.MethodPost, "/v1/accounts/assertion", "", issueBody(acctMemberCertFp, "x", "n")); rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d", rr.Code)
	}
}

// A device that itself came from another account server holds no account here, so it cannot vouch for one.
func TestAssertionIssue_FederatedDeviceForbidden(t *testing.T) {
	f := newFederation(t)
	otherKey, otherFp := func() (string, string) {
		pub, _, _ := ed25519.GenerateKey(nil)
		return base64.StdEncoding.EncodeToString(pub), security.KeyFingerprint(pub)
	}()
	if _, err := f.srvDB.SetTrustedAccountServer("other", otherKey); err != nil {
		t.Fatal(err)
	}
	if _, err := f.srvDB.LoginDeviceFederated("fed", "fed", "tok-fed", "pk", "mallory", otherFp); err != nil {
		t.Fatal(err)
	}
	p := f.memberProof(t)
	rr := accCall(f.srv, http.MethodPost, "/v1/accounts/assertion", "tok-fed", issueBody(acctMemberCertFp, p.pub, p.nonce))
	if rr.Code != http.StatusForbidden || accCode(t, rr) != "FORBIDDEN" {
		t.Fatalf("got %d %s", rr.Code, rr.Body.String())
	}
}

func TestAssertionIssue_RejectsBadBodies(t *testing.T) {
	f := newFederation(t)
	p := f.memberProof(t)
	for name, body := range map[string]string{
		"no audience":        issueBody("", p.pub, p.nonce),
		"audience not hex":   issueBody("not a fingerprint", p.pub, p.nonce),
		"no device key":      issueBody(acctMemberCertFp, "", p.nonce),
		"device key not b64": issueBody(acctMemberCertFp, "***", p.nonce),
		"short device key":   issueBody(acctMemberCertFp, base64.StdEncoding.EncodeToString([]byte("short")), p.nonce),
		"no nonce":           issueBody(acctMemberCertFp, p.pub, ""),
		"not json":           "nope",
	} {
		rr := accCall(f.srv, http.MethodPost, "/v1/accounts/assertion", f.adminToken, body)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400: %s", name, rr.Code, rr.Body.String())
		}
	}
}

// ---- federated login ----

func TestFederatedLogin_EndToEndMintsOwnerDevice(t *testing.T) {
	f := newFederation(t)
	p := f.memberProof(t)
	ir := accCall(f.srv, http.MethodPost, "/v1/accounts/assertion", f.adminToken, issueBody(acctMemberCertFp, p.pub, p.nonce))
	tok, _ := accJSON(t, ir)["assertion"].(string)
	if tok == "" {
		t.Fatalf("no assertion: %s", ir.Body.String())
	}
	rr := f.login(tok, p)
	if rr.Code != http.StatusOK {
		t.Fatalf("login = %d: %s", rr.Code, rr.Body.String())
	}
	m := accJSON(t, rr)
	devTok, _ := m["deviceToken"].(string)
	devID, _ := m["deviceId"].(string)
	if devTok == "" || devID == "" {
		t.Fatalf("response = %v", m)
	}
	dev, err := f.memberDB.DeviceByToken(devTok)
	if err != nil || dev == nil || dev.ViaLogin || dev.CanModify || dev.CanShare || dev.PublicKey != p.pub {
		t.Fatalf("device = %+v, %v", dev, err)
	}
	if user, issuer, _ := f.memberDB.DeviceAccount(devID); user != "alice" || issuer != f.keyFp {
		t.Fatalf("DeviceAccount = %q %q", user, issuer)
	}
	if has, _ := f.memberDB.HasAnyUser(); has {
		t.Fatal("member gained a local account")
	}
	// The token is not an owner token: an admin-only route refuses it, a browse route accepts it.
	if ar := accCall(f.member, http.MethodGet, "/v1/audit", devTok, ""); ar.Code != http.StatusForbidden {
		t.Fatalf("audit with federated token = %d, want 403", ar.Code)
	}
	if ar := accCall(f.member, http.MethodGet, "/v1/transfers/list", devTok, ""); ar.Code != http.StatusOK {
		t.Fatalf("transfers list with federated token = %d", ar.Code)
	}
}

func TestFederatedLogin_WritesAuditWithoutTheAssertion(t *testing.T) {
	f := newFederation(t)
	p := f.memberProof(t)
	tok := f.sign(t, f.claims(p))
	if rr := f.login(tok, p); rr.Code != http.StatusOK {
		t.Fatalf("login = %d: %s", rr.Code, rr.Body.String())
	}
	entries, err := f.memberDB.AuditEntries(100, 0)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, e := range entries {
		raw, _ := json.Marshal(e)
		if strings.Contains(string(raw), tok) {
			t.Fatalf("audit entry contains the assertion: %s", raw)
		}
		if e.Action == store.AuditLogin && e.Actor == "alice@"+f.keyFp[:12] && strings.Contains(e.Detail, "account server") {
			found = true
		}
	}
	if !found {
		t.Fatalf("no login audit entry naming the account server: %+v", entries)
	}
}

func TestFederatedLogin_Rejections(t *testing.T) {
	cases := map[string]func(f *federation, c *security.AssertionClaims){
		"wrong audience":          func(f *federation, c *security.AssertionClaims) { c.Aud = acctServerCertFp },
		"empty audience":          func(f *federation, c *security.AssertionClaims) { c.Aud = "" },
		"expired beyond skew":     func(f *federation, c *security.AssertionClaims) { c.Exp = time.Now().Add(-3 * time.Minute).Unix() },
		"exp too far ahead":       func(f *federation, c *security.AssertionClaims) { c.Exp = time.Now().Add(15 * time.Minute).Unix() },
		"device key mismatch":     func(f *federation, c *security.AssertionClaims) { c.DevicePublicKey = "AAAA" },
		"nonce mismatch":          func(f *federation, c *security.AssertionClaims) { c.Nonce = "some-other-nonce" },
		"wrong issuer":            func(f *federation, c *security.AssertionClaims) { c.Iss = strings.Repeat("0", 64) },
		"empty subject":           func(f *federation, c *security.AssertionClaims) { c.Sub = "" },
		"oversized subject":       func(f *federation, c *security.AssertionClaims) { c.Sub = strings.Repeat("a", 500) },
		"control char in subject": func(f *federation, c *security.AssertionClaims) { c.Sub = "al\nice" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFederation(t)
			p := f.memberProof(t)
			c := f.claims(p)
			mutate(f, &c)
			f.expectRejected(t, f.login(f.sign(t, c), p))
			if devs, _ := f.memberDB.ListDevices(); len(devs) != 0 {
				t.Fatalf("rejected login left %d device rows", len(devs))
			}
		})
	}
}

func TestFederatedLogin_AcceptsExpiryInsideClockSkew(t *testing.T) {
	f := newFederation(t)
	p := f.memberProof(t)
	c := f.claims(p)
	c.Exp = time.Now().Add(-time.Minute).Unix()
	if rr := f.login(f.sign(t, c), p); rr.Code != http.StatusOK {
		t.Fatalf("login = %d: %s", rr.Code, rr.Body.String())
	}
}

func TestFederatedLogin_ReplayedNonceRejected(t *testing.T) {
	f := newFederation(t)
	p := f.memberProof(t)
	tok := f.sign(t, f.claims(p))
	if rr := f.login(tok, p); rr.Code != http.StatusOK {
		t.Fatalf("first login = %d: %s", rr.Code, rr.Body.String())
	}
	rr := f.login(tok, p)
	if rr.Code != http.StatusUnauthorized || strings.Contains(rr.Body.String(), "deviceToken") {
		t.Fatalf("replay = %d: %s", rr.Code, rr.Body.String())
	}
}

func TestFederatedLogin_AssertionForAnotherNonceCannotUseAFreshOne(t *testing.T) {
	f := newFederation(t)
	p1 := f.memberProof(t)
	p2 := f.memberProof(t)
	// Assertion bound to p1's nonce, presented with p2's (valid) proof.
	c := f.claims(p1)
	c.DevicePublicKey = p2.pub
	f.expectRejected(t, f.login(f.sign(t, c), p2))
}

func TestFederatedLogin_UntrustedOrRotatedKeyRejected(t *testing.T) {
	f := newFederation(t)
	p := f.memberProof(t)
	tok := f.sign(t, f.claims(p))
	// Rotated: the member pinned a different key than the one that signed.
	otherPub, _, _ := ed25519.GenerateKey(nil)
	if _, err := f.memberDB.SetTrustedAccountServer("home server", base64.StdEncoding.EncodeToString(otherPub)); err != nil {
		t.Fatal(err)
	}
	f.expectRejected(t, f.login(tok, p))
	// Untrusted: nothing pinned at all.
	if err := f.memberDB.RemoveTrustedAccountServer(); err != nil {
		t.Fatal(err)
	}
	p2 := f.memberProof(t)
	f.expectRejected(t, f.login(f.sign(t, f.claims(p2)), p2))
}

func TestFederatedLogin_SignedByAnAttackersKeyRejected(t *testing.T) {
	f := newFederation(t)
	p := f.memberProof(t)
	_, evil, _ := ed25519.GenerateKey(nil)
	c := f.claims(p) // claims the trusted issuer fingerprint, but is signed by another key
	tok, err := security.SignAssertion(evil, c)
	if err != nil {
		t.Fatal(err)
	}
	f.expectRejected(t, f.login(tok, p))
}

func TestFederatedLogin_BadDeviceProofRejectedAndNoToken(t *testing.T) {
	f := newFederation(t)
	p := f.memberProof(t)
	tok := f.sign(t, f.claims(p))
	p.sig = base64.StdEncoding.EncodeToString(make([]byte, ed25519.SignatureSize))
	rr := f.login(tok, p)
	if rr.Code != http.StatusUnauthorized || strings.Contains(rr.Body.String(), "deviceToken") {
		t.Fatalf("login = %d: %s", rr.Code, rr.Body.String())
	}
}

func TestFederatedLogin_RejectsPasswordAndAssertionTogether(t *testing.T) {
	f := newFederation(t)
	p := f.memberProof(t)
	body, _ := json.Marshal(map[string]string{
		"assertion": f.sign(t, f.claims(p)), "username": "alice", "password": "whatever-123",
		"devicePublicKey": p.pub, "nonce": p.nonce, "signature": p.sig,
	})
	rr := accCall(f.member, http.MethodPost, "/v1/login", "", string(body))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d: %s", rr.Code, rr.Body.String())
	}
}

func TestFederatedLogin_FailuresShareTheLoginRateLimit(t *testing.T) {
	f := newFederation(t)
	for i := 0; i < loginRateLimitAttempts; i++ {
		f.expectRejected(t, f.login("not.anassertion", proof{pub: "k", nonce: "n", sig: "s"}))
	}
	p := f.memberProof(t)
	rr := f.login(f.sign(t, f.claims(p)), p)
	if rr.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429: %s", rr.Code, rr.Body.String())
	}
}

func TestFederatedLogin_FailureIsAuditedWithoutAssertion(t *testing.T) {
	f := newFederation(t)
	p := f.memberProof(t)
	c := f.claims(p)
	c.Aud = acctServerCertFp
	tok := f.sign(t, c)
	f.expectRejected(t, f.login(tok, p))
	entries, _ := f.memberDB.AuditEntries(100, 0)
	var found bool
	for _, e := range entries {
		raw, _ := json.Marshal(e)
		if strings.Contains(string(raw), tok) {
			t.Fatalf("audit entry contains the assertion: %s", raw)
		}
		found = found || e.Action == store.AuditLoginFailed
	}
	if !found {
		t.Fatal("failed federated login was not audited")
	}
}

func TestPasswordLogin_UnchangedWithATrustedServer(t *testing.T) {
	f := newFederation(t)
	hash, err := security.HashPassword("correct-horse-battery")
	if err != nil {
		t.Fatal(err)
	}
	if err := f.memberDB.CreateUser("owner", hash); err != nil {
		t.Fatal(err)
	}
	p := f.memberProof(t)
	body, _ := json.Marshal(map[string]string{
		"username": "owner", "password": "correct-horse-battery", "deviceLabel": "pw-phone",
		"devicePublicKey": p.pub, "nonce": p.nonce, "signature": p.sig,
	})
	rr := accCall(f.member, http.MethodPost, "/v1/login", "", string(body))
	if rr.Code != http.StatusOK {
		t.Fatalf("password login = %d: %s", rr.Code, rr.Body.String())
	}
	devID, _ := accJSON(t, rr)["deviceId"].(string)
	if _, issuer, _ := f.memberDB.DeviceAccount(devID); issuer != "" {
		t.Fatalf("password login marked as federated (issuer %q)", issuer)
	}
	// Still demanded: a login with neither a password nor an assertion.
	if rr := accCall(f.member, http.MethodPost, "/v1/login", "", `{"username":"owner"}`); rr.Code != http.StatusBadRequest {
		t.Fatalf("no password, no assertion = %d", rr.Code)
	}
}

// ---- health ----

func TestHealth_AdvertisesAccountServerToAuthorizedCallersOnly(t *testing.T) {
	f := newFederation(t)
	rr := accCall(f.srv, http.MethodGet, "/v1/health", f.adminToken, "")
	acct, _ := accJSON(t, rr)["accountServer"].(map[string]any)
	if acct["name"] != "home server" || acct["keyFingerprint"] != f.keyFp {
		t.Fatalf("health accountServer = %v", acct)
	}
	if _, ok := accJSON(t, accCall(f.srv, http.MethodGet, "/v1/health", "", ""))["accountServer"]; ok {
		t.Fatal("unauthenticated health leaks accountServer")
	}
	if err := f.srvDB.SetAccountServer(false, ""); err != nil {
		t.Fatal(err)
	}
	if _, ok := accJSON(t, accCall(f.srv, http.MethodGet, "/v1/health", f.adminToken, ""))["accountServer"]; ok {
		t.Fatal("health shows accountServer while the role is off")
	}
}

// ---- settings ----

func TestAccountServerSettings_AdminOnly(t *testing.T) {
	f := newFederation(t)
	if _, err := f.srvDB.UpsertDevice("plain", "plain", "tok-plain", "", false); err != nil {
		t.Fatal(err)
	}
	for _, m := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
		if rr := accCall(f.srv, m, "/v1/settings/account-server", "tok-plain", `{"enabled":false}`); rr.Code != http.StatusForbidden {
			t.Errorf("%s as code-paired device = %d", m, rr.Code)
		}
		if rr := accCall(f.srv, m, "/v1/settings/account-server", "", `{}`); rr.Code != http.StatusUnauthorized {
			t.Errorf("%s without token = %d", m, rr.Code)
		}
	}
}

func TestAccountServerSettings_GetPutDelete(t *testing.T) {
	h, db := newAccountRouter(t, acctMemberCertFp)
	hash, err := security.HashPassword(pinTestPassword)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.RegisterAccount("a", "a", "tok-admin", "", "owner", hash); err != nil {
		t.Fatal(err)
	}
	get := func() map[string]any {
		rr := accCall(h, http.MethodGet, "/v1/settings/account-server", "tok-admin", "")
		if rr.Code != http.StatusOK {
			t.Fatalf("GET = %d: %s", rr.Code, rr.Body.String())
		}
		return accJSON(t, rr)
	}
	if m := get(); m["enabled"] != false || m["trustedServer"] != nil {
		t.Fatalf("default = %v", m)
	}
	// Enable with a name: key appears.
	rr := accCall(h, http.MethodPut, "/v1/settings/account-server", "tok-admin", `{"enabled":true,"name":"my server"}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT enable = %d: %s", rr.Code, rr.Body.String())
	}
	m := get()
	if m["enabled"] != true || m["name"] != "my server" || m["keyFingerprint"] == "" || m["keyFingerprint"] == nil {
		t.Fatalf("after enable = %v", m)
	}
	// Pin a server; a fingerprint typo is refused, the right one accepted.
	pub, _, _ := ed25519.GenerateKey(nil)
	key := base64.StdEncoding.EncodeToString(pub)
	bad, _ := json.Marshal(map[string]any{"trustedServer": map[string]string{"name": "x", "publicKey": key, "keyFingerprint": strings.Repeat("0", 64)}})
	if rr := accCall(h, http.MethodPut, "/v1/settings/account-server", "tok-admin", string(bad)); rr.Code != http.StatusBadRequest {
		t.Fatalf("PUT with wrong fingerprint = %d", rr.Code)
	}
	good, _ := json.Marshal(map[string]any{"trustedServer": map[string]string{"name": "x", "publicKey": key, "keyFingerprint": security.KeyFingerprint(pub)}, "password": pinTestPassword})
	if rr := accCall(h, http.MethodPut, "/v1/settings/account-server", "tok-admin", string(good)); rr.Code != http.StatusOK {
		t.Fatalf("PUT trust = %d: %s", rr.Code, rr.Body.String())
	}
	ts, _ := get()["trustedServer"].(map[string]any)
	if ts["name"] != "x" || ts["keyFingerprint"] != security.KeyFingerprint(pub) {
		t.Fatalf("trustedServer = %v", ts)
	}
	if rr := accCall(h, http.MethodPut, "/v1/settings/account-server", "tok-admin", `{"trustedServer":{"name":"x","publicKey":"***"}}`); rr.Code != http.StatusBadRequest {
		t.Fatalf("PUT malformed key = %d", rr.Code)
	}
	if rr := accCall(h, http.MethodPut, "/v1/settings/account-server", "tok-admin", `{}`); rr.Code != http.StatusBadRequest {
		t.Fatalf("PUT empty = %d", rr.Code)
	}
	// Disable keeps the pin; DELETE removes it.
	if rr := accCall(h, http.MethodPut, "/v1/settings/account-server", "tok-admin", `{"enabled":false}`); rr.Code != http.StatusOK {
		t.Fatalf("PUT disable = %d", rr.Code)
	}
	if m := get(); m["enabled"] != false || m["trustedServer"] == nil {
		t.Fatalf("after disable = %v", m)
	}
	if rr := accCall(h, http.MethodDelete, "/v1/settings/account-server", "tok-admin", ""); rr.Code != http.StatusNoContent {
		t.Fatalf("DELETE = %d", rr.Code)
	}
	if m := get(); m["trustedServer"] != nil {
		t.Fatalf("still trusted after DELETE: %v", m)
	}
	// Every change is in the audit trail.
	entries, _ := db.AuditEntries(100, 0)
	var n int
	for _, e := range entries {
		if e.Action == store.AuditAccountServer {
			n++
		}
	}
	if n < 4 {
		t.Fatalf("audited %d account-server changes, want at least 4 (enable, trust, disable, untrust)", n)
	}
}
