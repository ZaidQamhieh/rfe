package pairing

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"math"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/next/audit"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/authz"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/store"
	oldpairing "github.com/ZaidQamhieh/rfe/agent/internal/pairing"
)

// t0 is the fixed start of every test clock.
var t0 = time.Date(2031, 3, 4, 5, 6, 7, 0, time.UTC)

// testFingerprint is a made-up certificate SHA-256 (64 hex digits).
var testFingerprint = hex.EncodeToString(make([]byte, sha256.Size))

// Values the tests send; none is a real credential.
const (
	srcA         = "192.0.2.10:5000"
	testPassword = "correct horse battery"
	fixedNonce   = "00112233445566778899aabbccddeeff" // a 16-byte client nonce, hex
)

// clock is a settable clock.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

// recorder collects audit events.
type recorder struct {
	mu sync.Mutex
	ev []audit.Event
}

func (r *recorder) Try(e audit.Event) { r.mu.Lock(); r.ev = append(r.ev, e); r.mu.Unlock() }
func (r *recorder) actions() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for _, e := range r.ev {
		out = append(out, e.Action)
	}
	return strings.Join(out, ",")
}

// env is a service over a real store in a temp dir with a fake clock.
type env struct {
	t       *testing.T
	svc     *Service
	db      *store.DB
	clk     *clock
	log     *recorder
	prompts []Prompt

	challenges int
}

func newEnv(t *testing.T) *env {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "agent-v3.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	e := &env{t: t, db: db, clk: &clock{t: t0}, log: &recorder{}}
	svc, err := New(Options{
		Store: db, Fingerprint: testFingerprint, AgentName: "pc", Address: "192.0.2.1:8765",
		Now: e.clk.now, Audit: e.log, OnPairRequest: func(p Prompt) { e.prompts = append(e.prompts, p) },
		// A fast stand-in for the real password hash, which has its own tests.
		Passwords: Passwords{
			Hash:   func(p string) (string, error) { return "hash:" + p, nil },
			Verify: func(h, p string) bool { return h == "hash:"+p },
			Burn:   func(string) {},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	e.svc = svc
	return e
}

// phone is a device with its own Ed25519 identity.
type phone struct {
	id   string
	pub  string
	priv ed25519.PrivateKey
}

// newPhone makes a phone with a deterministic key derived from seedByte.
func newPhone(id string, seedByte byte) phone {
	seed := make([]byte, ed25519.SeedSize)
	seed[0] = seedByte
	priv := ed25519.NewKeyFromSeed(seed)
	return phone{id: id, pub: base64.StdEncoding.EncodeToString(priv.Public().(ed25519.PublicKey)), priv: priv}
}

// proof fetches a fresh challenge and signs it, as a phone does before pairing.
func (e *env) proof(p phone) Proof {
	e.t.Helper()
	e.challenges++ // each proof comes from its own address so the challenge limit is not what these tests exercise
	ch, err := e.svc.Challenge("10.1." + itoa(e.challenges/250) + "." + itoa(e.challenges%250+1) + ":1")
	if err != nil {
		e.t.Fatalf("Challenge: %v", err)
	}
	sig := ed25519.Sign(p.priv, []byte(defaultProofMessage(testFingerprint, ch.Nonce)))
	return Proof{DeviceID: p.id, PublicKey: p.pub, Nonce: ch.Nonce, Signature: base64.StdEncoding.EncodeToString(sig)}
}

// pairInput builds a code pairing for p with a fresh proof.
func (e *env) pairInput(p phone, code string) PairInput {
	pr := e.proof(p)
	return PairInput{Code: code, Label: "my phone", DeviceID: p.id, PublicKey: p.pub, Nonce: pr.Nonce, Signature: pr.Signature, RemoteIP: srcA}
}

// pairByCode mints an ordinary code and pairs p with it.
func (e *env) pairByCode(p phone) PairResult {
	e.t.Helper()
	m, err := e.svc.Mint(0)
	if err != nil {
		e.t.Fatal(err)
	}
	res, err := e.svc.PairWithCode(e.pairInput(p, m.Code))
	if err != nil {
		e.t.Fatalf("PairWithCode: %v", err)
	}
	return res
}

// wantCode fails unless err is a service error with the given OpenAPI code.
func wantCode(t *testing.T, err error, code string) {
	t.Helper()
	got, ok := CodeOf(err)
	if !ok || got != code {
		t.Fatalf("error = %v, want code %s", err, code)
	}
}

// commitOf is the commitment (hex SHA-256) of a hex nonce.
func commitOf(nonceHex string) string {
	raw, _ := hex.DecodeString(nonceHex)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// startRequest runs create and reveal for p and returns the request id.
func (e *env) startRequest(p phone, upperCommit bool) string {
	e.t.Helper()
	pr := e.proof(p)
	commit := commitOf(fixedNonce)
	if upperCommit {
		commit = strings.ToUpper(commit)
	}
	c, err := e.svc.CreateRequest(RequestInput{Label: "tablet", DeviceID: p.id, PublicKey: p.pub, Nonce: pr.Nonce, Signature: pr.Signature, NonceCommit: commit, RemoteIP: srcA})
	if err != nil {
		e.t.Fatalf("CreateRequest: %v", err)
	}
	if err := e.svc.Reveal(c.RequestID, fixedNonce, srcA); err != nil {
		e.t.Fatalf("Reveal: %v", err)
	}
	return c.RequestID
}

func TestApproveOnPCFlowIssuesBrowseOnlyTokenOnce(t *testing.T) {
	e := newEnv(t)
	p := newPhone("client-1", 1)
	id := e.startRequest(p, false)
	if len(e.prompts) != 1 || e.prompts[0].ID != id || e.prompts[0].Replaces != "" {
		t.Fatalf("prompts = %+v", e.prompts)
	}
	want, _ := oldpairing.SAS(testFingerprint, fixedNonce, id)
	if got := e.prompts[0].SAS; got != want || got == "" {
		t.Fatalf("match code %q differs from the old agent's %q", got, want)
	}
	if e.prompts[0].TTL <= 0 || e.prompts[0].TTL > PairRequestTTL {
		t.Fatalf("prompt TTL = %v", e.prompts[0].TTL)
	}
	if r, err := e.svc.Poll(id, PollNonce("", fixedNonce), srcA); err != nil || r.Status != StatusPending {
		t.Fatalf("poll before the decision = %+v %v", r, err)
	}
	if _, err := e.svc.ListPending(Actor{}); err == nil {
		t.Fatal("a non-admin listed pending requests")
	}
	list, err := e.svc.ListPending(PC)
	if err != nil || len(list) != 1 || list[0].MatchCode != want {
		t.Fatalf("ListPending = %+v %v", list, err)
	}
	if err := e.svc.Decide(Actor{DeviceID: "x"}, id, true); err == nil {
		t.Fatal("a non-admin approved a request")
	}
	if err := e.svc.Decide(PC, id, true); err != nil {
		t.Fatal(err)
	}
	// The header form and the query form of the nonce are both accepted (compat C3).
	r, err := e.svc.Poll(id, PollNonce(fixedNonce, "ignored"), srcA)
	if err != nil || r.Status != StatusApproved || r.Pair == nil || r.Pair.DeviceToken == "" {
		t.Fatalf("approved poll = %+v %v", r, err)
	}
	d, err := e.svc.Authenticate(r.Pair.DeviceToken)
	if err != nil || d.Origin != store.OriginApprove {
		t.Fatalf("Authenticate = %+v %v", d, err)
	}
	pr := PrincipalOf(d)
	if pr.Admin || !pr.Grants.Browse || pr.Grants.Download || pr.Grants.Upload || pr.Grants.Modify || pr.Grants.Delete || pr.Grants.Share {
		t.Fatalf("an approved device must be browse-only: %+v", pr)
	}
	if _, err := e.svc.Poll(id, fixedNonce, srcA); err == nil {
		t.Fatal("the token was handed out twice")
	} else {
		wantCode(t, err, "NOT_FOUND")
	}
	if got := e.log.actions(); got != "pair" {
		t.Fatalf("audit = %q", got)
	}
}

func TestRejectedRequestReportsRejectedAndIssuesNothing(t *testing.T) {
	e := newEnv(t)
	id := e.startRequest(newPhone("client-1", 1), false)
	if err := e.svc.Decide(PC, id, false); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if r, err := e.svc.Poll(id, fixedNonce, srcA); err != nil || r.Status != StatusRejected || r.Pair != nil {
			t.Fatalf("poll %d = %+v %v", i, r, err)
		}
	}
	wantCode(t, e.svc.Decide(PC, id, true), "NOT_FOUND")
}

func TestRequestExpiresBeforeAndAfterApproval(t *testing.T) {
	e := newEnv(t)
	p := newPhone("client-1", 1)
	pr := e.proof(p)
	c, err := e.svc.CreateRequest(RequestInput{DeviceID: p.id, PublicKey: p.pub, Nonce: pr.Nonce, Signature: pr.Signature, NonceCommit: commitOf(fixedNonce), RemoteIP: srcA})
	if err != nil {
		t.Fatal(err)
	}
	e.clk.advance(PairRequestTTL + time.Second)
	wantCode(t, e.svc.Reveal(c.RequestID, fixedNonce, srcA), "NOT_FOUND")

	id := e.startRequest(newPhone("client-2", 2), false)
	if err := e.svc.Decide(PC, id, true); err != nil {
		t.Fatal(err)
	}
	e.clk.advance(PairRequestTTL + time.Second)
	_, err = e.svc.Poll(id, fixedNonce, srcA)
	wantCode(t, err, "NOT_FOUND")
}

func TestWrongOrMissingNonceCannotPollOrReveal(t *testing.T) {
	e := newEnv(t)
	p := newPhone("client-1", 1)
	pr := e.proof(p)
	c, _ := e.svc.CreateRequest(RequestInput{DeviceID: p.id, PublicKey: p.pub, Nonce: pr.Nonce, Signature: pr.Signature, NonceCommit: commitOf(fixedNonce), RemoteIP: srcA})
	other := strings.Repeat("ab", clientNonceBytes)
	wantCode(t, e.svc.Reveal(c.RequestID, other, srcA), "NOT_FOUND")
	wantCode(t, e.svc.Reveal(c.RequestID, "nothex", srcA), "BAD_REQUEST")
	if err := e.svc.Reveal(c.RequestID, fixedNonce, srcA); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.Decide(PC, c.RequestID, true); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"", other, fixedNonce[:30]} {
		_, err := e.svc.Poll(c.RequestID, bad, srcA)
		wantCode(t, err, "NOT_FOUND")
	}
	_, err := e.svc.Poll("no-such-request", fixedNonce, srcA)
	wantCode(t, err, "NOT_FOUND")
	// A second reveal of the same request is refused: it works once.
	wantCode(t, e.svc.Reveal(c.RequestID, fixedNonce, srcA), "NOT_FOUND")
}

func TestUppercaseNonceCommitmentStillCompletes(t *testing.T) {
	e := newEnv(t)
	id := e.startRequest(newPhone("client-1", 1), true) // Reveal inside fails the test if the commit case mismatches
	if err := e.svc.Decide(PC, id, true); err != nil {
		t.Fatal(err)
	}
	r, err := e.svc.Poll(id, strings.ToUpper(fixedNonce), srcA)
	if err != nil || r.Status != StatusApproved {
		t.Fatalf("poll = %+v %v", r, err)
	}
}

func TestUsedCodeCannotBeReplayedAndBadProofDoesNotSpendIt(t *testing.T) {
	e := newEnv(t)
	m, err := e.svc.Mint(0)
	if err != nil {
		t.Fatal(err)
	}
	p := newPhone("client-1", 1)
	forged := e.pairInput(p, m.Code)
	forged.PublicKey = newPhone("client-1", 9).pub // a key that did not sign the nonce
	_, err = e.svc.PairWithCode(forged)
	wantCode(t, err, "INVALID_SIGNATURE")
	in := e.pairInput(p, m.Code)
	if _, err := e.svc.PairWithCode(in); err != nil {
		t.Fatalf("first real use failed: %v", err)
	}
	_, err = e.svc.PairWithCode(e.pairInput(newPhone("client-2", 2), m.Code))
	wantCode(t, err, "INVALID_CODE")
	// The same proof cannot be replayed either.
	_, err = e.svc.PairWithCode(in)
	wantCode(t, err, "INVALID_NONCE")
	m2, _ := e.svc.Mint(0)
	_, err = e.svc.PairWithCode(PairInput{Code: m2.Code, DeviceID: "x", RemoteIP: srcA})
	wantCode(t, err, "DEVICE_KEY_REQUIRED")
	if _, err := e.svc.PairWithCode(e.pairInput(newPhone("client-3", 3), m2.Code)); err != nil {
		t.Fatalf("a rejected attempt spent the code: %v", err)
	}
}

func TestExpiredCodeIsRefused(t *testing.T) {
	e := newEnv(t)
	m, _ := e.svc.Mint(90 * time.Second)
	if m.ExpiresInSeconds != 90 {
		t.Fatalf("ExpiresInSeconds = %d", m.ExpiresInSeconds)
	}
	e.clk.advance(91 * time.Second)
	_, err := e.svc.PairWithCode(e.pairInput(newPhone("client-1", 1), m.Code))
	wantCode(t, err, "INVALID_CODE")
}

func TestNewCodeDeviceIsBrowseOnlyAndAuthenticates(t *testing.T) {
	e := newEnv(t)
	res := e.pairByCode(newPhone("client-1", 1))
	d, err := e.svc.Authenticate(res.DeviceToken)
	if err != nil || d.ID != res.DeviceID || d.Origin != store.OriginCode {
		t.Fatalf("Authenticate = %+v %v", d, err)
	}
	pr := PrincipalOf(d)
	if pr.Admin || pr.Grants != (authz.Grants{Browse: true}) || pr.JailRoot != "" || pr.ReadOnly {
		t.Fatalf("principal = %+v", pr)
	}
	if len(res.DeviceToken) != 2*tokenBytes || res.CertFingerprint != testFingerprint || res.AgentName != "pc" {
		t.Fatalf("result = %+v", res)
	}
	_, err = e.svc.Authenticate("")
	wantCode(t, err, "UNAUTHORIZED")
	_, err = e.svc.Authenticate("not-a-token")
	wantCode(t, err, "UNAUTHORIZED")
	if BearerToken("Bearer abc ") != "abc" || BearerToken("bearer abc") != "abc" || BearerToken("Basic abc") != "" || BearerToken("Bearer") != "" {
		t.Fatal("BearerToken parsing")
	}
}

func TestImportedLegacyTokenHashKeepsAuthenticating(t *testing.T) {
	e := newEnv(t)
	const legacyToken = "legacy-token-made-by-the-old-agent"
	err := e.db.ImportAll(store.ImportData{Devices: []store.Device{{
		ID: "old-1", Label: "old phone", TokenHash: store.HashToken(legacyToken), Origin: store.OriginCode,
		ClientID: "old-client", PublicKey: "k", Created: 1, LastSeen: 1, CanBrowse: true, CanDownload: true,
	}}})
	if err != nil {
		t.Fatal(err)
	}
	d, err := e.svc.Authenticate(legacyToken)
	if err != nil || d.ID != "old-1" || !d.CanDownload {
		t.Fatalf("Authenticate(legacy) = %+v %v", d, err)
	}
	if err := e.svc.Revoke(PC, "old-1"); err != nil {
		t.Fatal(err)
	}
	_, err = e.svc.Authenticate(legacyToken)
	wantCode(t, err, "UNAUTHORIZED")
}

func TestRevokeAndRemoveEndTheTokenAndFollowRights(t *testing.T) {
	e := newEnv(t)
	a, b := e.pairByCode(newPhone("client-a", 1)), e.pairByCode(newPhone("client-b", 2))
	da, _ := e.svc.Authenticate(a.DeviceToken)
	// A plain device may not touch another device, nor change limits.
	wantCode(t, e.svc.Revoke(ActorOf(da), b.DeviceID), "FORBIDDEN")
	wantCode(t, e.svc.SetJail(ActorOf(da), a.DeviceID, "/x"), "FORBIDDEN")
	wantCode(t, e.svc.SetFileGrants(ActorOf(da), a.DeviceID, authz.Grants{Browse: true, Delete: true}), "FORBIDDEN")
	if err := e.svc.Revoke(ActorOf(da), a.DeviceID); err != nil { // itself
		t.Fatal(err)
	}
	_, err := e.svc.Authenticate(a.DeviceToken)
	wantCode(t, err, "UNAUTHORIZED")
	if got := ActorOf(da); got.Admin || got.DeviceID != a.DeviceID {
		t.Fatalf("actor = %+v", got)
	}
	if err := e.svc.Remove(PC, b.DeviceID); err != nil {
		t.Fatal(err)
	}
	_, err = e.svc.Authenticate(b.DeviceToken)
	wantCode(t, err, "UNAUTHORIZED")
	wantCode(t, e.svc.Remove(PC, b.DeviceID), "NOT_FOUND")
	if got := e.log.actions(); got != "pair,pair,device_revoked,device_removed" {
		t.Fatalf("audit = %q", got)
	}
}

func TestAdminChangesLimitsAndGrants(t *testing.T) {
	e := newEnv(t)
	res := e.pairByCode(newPhone("client-a", 1))
	if err := e.svc.SetFileGrants(PC, res.DeviceID, authz.Grants{Browse: true, Download: true, Share: true}); err != nil {
		t.Fatal(err)
	}
	wantCode(t, e.svc.SetAppGrants(PC, res.DeviceID, authz.Grants{LaunchApps: true}), "BAD_REQUEST")
	if err := e.svc.SetAppGrants(PC, res.DeviceID, authz.Grants{ViewApps: true, LaunchApps: true}); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.SetJail(PC, res.DeviceID, "/srv/a"); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.SetReadOnly(PC, res.DeviceID, true); err != nil {
		t.Fatal(err)
	}
	d, _ := e.svc.Authenticate(res.DeviceToken)
	want := authz.Grants{Browse: true, Download: true, Share: true, ViewApps: true, LaunchApps: true}
	if p := PrincipalOf(d); p.Grants != want || p.JailRoot != "/srv/a" || !p.ReadOnly {
		t.Fatalf("principal = %+v", p)
	}
	wantCode(t, e.svc.SetJail(PC, "missing", "/x"), "NOT_FOUND")
}

func TestRepairingUnderAnotherKeyResetsTheTakenOverDevice(t *testing.T) {
	e := newEnv(t)
	first := e.pairByCode(newPhone("client-a", 1))
	if err := e.svc.SetFileGrants(PC, first.DeviceID, authz.Grants{Browse: true, Delete: true}); err != nil {
		t.Fatal(err)
	}
	second := e.pairByCode(newPhone("client-a", 2)) // same client id, a different key
	if second.DeviceID != first.DeviceID {
		t.Fatalf("expected the row to be reused: %s vs %s", second.DeviceID, first.DeviceID)
	}
	_, err := e.svc.Authenticate(first.DeviceToken)
	wantCode(t, err, "UNAUTHORIZED")
	d, _ := e.svc.Authenticate(second.DeviceToken)
	if PrincipalOf(d).Grants != (authz.Grants{Browse: true}) {
		t.Fatalf("takeover kept grants: %+v", PrincipalOf(d))
	}
}

func TestGuestCodeIsJailedAndReadOnlyAndFailsClosed(t *testing.T) {
	e := newEnv(t)
	ok := func(root string) (string, error) { return filepath.Clean(root), nil }
	m, err := e.svc.Generate(PC, GenerateRequest{Guest: true, JailRoot: "/srv/guest"}, ok)
	if err != nil {
		t.Fatal(err)
	}
	res, err := e.svc.PairWithCode(e.pairInput(newPhone("guest-1", 4), m.Code))
	if err != nil {
		t.Fatal(err)
	}
	d, _ := e.svc.Authenticate(res.DeviceToken)
	if d.JailRoot != "/srv/guest" || !d.ReadOnly {
		t.Fatalf("guest device = %+v", d)
	}
	// Every way a guest request can fail mints nothing and never falls back to an ordinary code.
	for name, body := range map[string]string{
		"trailing comma": `{"guest":true,"jailRoot":"/srv/x",}`,
		"truncated":      `{"guest":true,"jailRoot":"/srv/x"`,
		"wrong type":     `{"guest":"yes","jailRoot":"/srv/x"}`,
		"huge ttl":       `{"ttlSeconds":99999999999999999999999}`,
		"too large":      `{"jailRoot":"` + strings.Repeat("a", MaxGenerateBodyBytes) + `"}`,
	} {
		if _, err := ParseGenerateRequest(strings.NewReader(body)); err == nil {
			t.Fatalf("%s: body decoded", name)
		} else {
			wantCode(t, err, "BAD_REQUEST")
		}
	}
	if req, err := ParseGenerateRequest(strings.NewReader("")); err != nil || req != (GenerateRequest{}) {
		t.Fatalf("empty body = %+v %v", req, err)
	}
	_, err = e.svc.Generate(PC, GenerateRequest{Guest: true}, func(string) (string, error) { return "", nil })
	wantCode(t, err, "BAD_REQUEST")
	_, err = e.svc.Generate(PC, GenerateRequest{Guest: true, JailRoot: "/srv/x"}, nil)
	wantCode(t, err, "BAD_REQUEST")
	_, err = e.svc.Generate(PC, GenerateRequest{Guest: true, JailRoot: "/srv/x"}, func(string) (string, error) { return "", errors.New("outside roots") })
	wantCode(t, err, "BAD_REQUEST")
	_, err = e.svc.Generate(Actor{DeviceID: "x"}, GenerateRequest{}, ok)
	wantCode(t, err, "FORBIDDEN")
	_, err = e.svc.MintGuest(time.Hour, "")
	wantCode(t, err, "BAD_REQUEST")
}

func TestHugeTTLIsCappedNotWrappedAndReportedTruthfully(t *testing.T) {
	e := newEnv(t)
	maxSecs := int64(MaxCodeTTL / time.Second)
	for _, secs := range []int64{10_000_000_000, math.MaxInt64, maxSecs + 1} {
		m, err := e.svc.Generate(PC, GenerateRequest{TTLSeconds: secs}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if m.ExpiresInSeconds != maxSecs || !m.ExpiresAt.Equal(t0.Add(MaxCodeTTL)) {
			t.Fatalf("ttl %d: reported %d expires %v", secs, m.ExpiresInSeconds, m.ExpiresAt)
		}
	}
	for secs, want := range map[int64]int64{0: int64(DefaultCodeTTL / time.Second), -5: int64(DefaultCodeTTL / time.Second), 120: 120} {
		m, _ := e.svc.Generate(PC, GenerateRequest{TTLSeconds: secs}, nil)
		if m.ExpiresInSeconds != want {
			t.Fatalf("ttl %d: reported %d, want %d", secs, m.ExpiresInSeconds, want)
		}
	}
	m, _ := e.svc.Mint(0)
	if len(m.Code) != CodeLength || strings.Trim(m.Code, CodeAlphabet) != "" || m.QR.PairingCode != m.Code || m.QR.CertFingerprint != testFingerprint {
		t.Fatalf("code = %+v", m)
	}
}

func TestPerSourceAndGlobalRateLimits(t *testing.T) {
	e := newEnv(t)
	attempt := func(src string) error {
		_, err := e.svc.PairWithCode(PairInput{Code: "AAAAAAAA", RemoteIP: src})
		return err
	}
	for i := 0; i < PairAttempts; i++ {
		wantCode(t, attempt(srcA), "DEVICE_KEY_REQUIRED") // counted, then refused for the missing proof
	}
	wantCode(t, attempt(srcA), "RATE_LIMITED")
	wantCode(t, attempt("192.0.2.10:9999"), "RATE_LIMITED") // the port is not part of the source
	e.clk.advance(RateWindow)
	wantCode(t, attempt(srcA), "DEVICE_KEY_REQUIRED")

	// Many addresses of one IPv6 /64 share one allowance.
	for i := 0; i < PairAttempts; i++ {
		wantCode(t, attempt("[2001:db8:1:2::"+string("0123456789"[i])+"]:1"), "DEVICE_KEY_REQUIRED")
	}
	wantCode(t, attempt("[2001:db8:1:2:ffff::1]:1"), "RATE_LIMITED")

	// Many sources together hit the global cap.
	e.clk.advance(RateWindow)
	limited := 0
	for i := 0; i < PairAttempts*GlobalRateFactor+5; i++ {
		src := "203.0.113." + itoa(i+1) + ":1"
		if err := attempt(src); errors.Is(err, ErrRateLimited) {
			limited++
		}
	}
	if limited != 5 {
		t.Fatalf("global limit refused %d attempts, want 5", limited)
	}
}

// itoa formats a small non-negative number.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for ; n > 0; n /= 10 {
		b = append([]byte{byte('0' + n%10)}, b...)
	}
	return string(b)
}

func TestRegisterWithOwnerCodeMakesAnAdminDeviceThatBypassesFileGrants(t *testing.T) {
	e := newEnv(t)
	p := newPhone("client-1", 1)
	plain, _ := e.svc.Mint(0)
	reg := func(code string) RegisterInput {
		pr := e.proof(p)
		return RegisterInput{Code: code, Username: "owner", Password: testPassword, Label: "my phone", DeviceID: p.id, PublicKey: p.pub, Nonce: pr.Nonce, Signature: pr.Signature, RemoteIP: srcA}
	}
	_, err := e.svc.Register(reg(plain.Code))
	wantCode(t, err, "CODE_CANNOT_REGISTER")
	weak := reg(plain.Code)
	weak.Password = "short"
	_, err = e.svc.Register(weak)
	wantCode(t, err, "BAD_REQUEST")
	owner, _ := e.svc.MintOwner(0)
	res, err := e.svc.Register(reg(owner.Code))
	if err != nil {
		t.Fatal(err)
	}
	d, err := e.svc.Authenticate(res.DeviceToken)
	if err != nil || !d.IsAdmin() || d.Username != "owner" {
		t.Fatalf("Authenticate = %+v %v", d, err)
	}
	pr := PrincipalOf(d)
	if !pr.Admin || !pr.Grants.Download || !pr.Grants.Delete || pr.Grants.ViewApps {
		t.Fatalf("principal = %+v", pr)
	}
	if !ActorOf(d).Admin {
		t.Fatal("an account device must be an admin actor")
	}
	// The ordinary code was not spent by the refused attempts above.
	if _, err := e.svc.PairWithCode(e.pairInput(newPhone("client-2", 2), plain.Code)); err != nil {
		t.Fatalf("ordinary code was spent: %v", err)
	}
	// One account per computer.
	owner2, _ := e.svc.MintOwner(0)
	_, err = e.svc.Register(reg(owner2.Code))
	wantCode(t, err, "ACCOUNT_ALREADY_EXISTS")
	if got := e.log.actions(); got != "register,pair" {
		t.Fatalf("audit = %q", got)
	}
}

func TestLoginChecksPasswordKeyAndLocksAnAccountAfterFailures(t *testing.T) {
	e := newEnv(t)
	p := newPhone("client-1", 1)
	owner, _ := e.svc.MintOwner(0)
	pr := e.proof(p)
	if _, err := e.svc.Register(RegisterInput{Code: owner.Code, Username: "Owner", Password: testPassword, DeviceID: p.id, PublicKey: p.pub, Nonce: pr.Nonce, Signature: pr.Signature, RemoteIP: srcA}); err != nil {
		t.Fatal(err)
	}
	login := func(user, pw string, ph phone, src string) (PairResult, error) {
		pf := e.proof(ph)
		return e.svc.Login(LoginInput{Username: user, Password: pw, Label: "x", DeviceID: ph.id, PublicKey: ph.pub, Nonce: pf.Nonce, Signature: pf.Signature, RemoteIP: src})
	}
	res, err := login("Owner", testPassword, p, srcA)
	if err != nil {
		t.Fatal(err)
	}
	if d, _ := e.svc.Authenticate(res.DeviceToken); d == nil || !d.IsAdmin() {
		t.Fatalf("login device = %+v", d)
	}
	// The same device id with another key is refused: password alone is not enough.
	_, err = login("Owner", testPassword, newPhone("client-1", 7), srcA)
	wantCode(t, err, "DEVICE_KEY_MISMATCH")
	_, err = login("Owner", "wrong password", p, srcA)
	wantCode(t, err, "INVALID_CREDENTIALS")
	_, err = login("nobody", "whatever", p, srcA)
	wantCode(t, err, "INVALID_CREDENTIALS")
	_, err = login("Owner", strings.Repeat("x", 500), p, srcA)
	wantCode(t, err, "INVALID_CREDENTIALS")
	// Failures from many addresses lock the account; success is refused while locked.
	for i := 0; i < AccountFailures; i++ {
		_, _ = login("owner", "bad", p, "198.51.100."+itoa(i+1)+":1")
	}
	_, err = login("OWNER", testPassword, p, "198.51.100.200:1")
	wantCode(t, err, "RATE_LIMITED")
	e.clk.advance(AccountFailureWindow + time.Second)
	if _, err := login("Owner", testPassword, p, "198.51.100.201:1"); err != nil {
		t.Fatalf("login after the lock window: %v", err)
	}
	if !strings.Contains(e.log.actions(), "login_failed") || !strings.Contains(e.log.actions(), "login") {
		t.Fatalf("audit = %q", e.log.actions())
	}
}

func TestChallengeNonceIsSingleUseAndExpires(t *testing.T) {
	e := newEnv(t)
	p := newPhone("client-1", 1)
	m, _ := e.svc.Mint(0)
	in := e.pairInput(p, m.Code)
	e.clk.advance(NonceTTL + time.Second)
	_, err := e.svc.PairWithCode(in)
	wantCode(t, err, "INVALID_NONCE")
	ch, _ := e.svc.Challenge(srcA)
	if ch.Proof != "v2" || ch.CertFingerprint != testFingerprint || len(ch.Nonce) != 2*challengeNonceBytes {
		t.Fatalf("challenge = %+v", ch)
	}
}

func TestSASMatchesTheOldAgentAndRejectsBadInput(t *testing.T) {
	for _, id := range []string{"a", "request-2", strings.Repeat("f", 32)} {
		want, err1 := oldpairing.SAS(testFingerprint, fixedNonce, id)
		got, err2 := SAS(testFingerprint, fixedNonce, id)
		if err1 != nil || err2 != nil || got != want {
			t.Fatalf("SAS(%q) = %q %v, old %q %v", id, got, err2, want, err1)
		}
	}
	if _, err := SAS("zz", fixedNonce, "a"); err == nil {
		t.Fatal("bad fingerprint accepted")
	}
	if _, err := SAS(testFingerprint, "00", "a"); err == nil {
		t.Fatal("short nonce accepted")
	}
}

func TestSanitizeLabelDropsControlsAndBounds(t *testing.T) {
	if got := sanitizeLabel("a\x00b\nc"); got != "abc" {
		t.Fatalf("got %q", got)
	}
	if got := sanitizeLabel("\x01\x02"); got != unnamedDeviceLabel {
		t.Fatalf("got %q", got)
	}
	if got := []rune(sanitizeLabel(strings.Repeat("é", 100))); len(got) != maxLabelRunes {
		t.Fatalf("len %d", len(got))
	}
}
