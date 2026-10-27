package server

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ZaidQamhieh/rfe/agent/internal/pairing"
	"github.com/ZaidQamhieh/rfe/agent/internal/security"
	"github.com/ZaidQamhieh/rfe/agent/internal/store"
	"github.com/go-chi/chi/v5"
)

const (
	testFp    = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	testCNonc = "00112233445566778899aabbccddeeff"
)

type pairReqEnv struct {
	t       *testing.T
	db      *store.DB
	cfg     Config
	nonces  *nonceStore
	prompt  *PairPrompt
	create  http.HandlerFunc
	poll    http.HandlerFunc
	revealH http.HandlerFunc
}

func newPairReqEnv(t *testing.T) *pairReqEnv {
	db, _ := newTestDeps(t)
	e := &pairReqEnv{t: t, db: db, nonces: newNonceStore()}
	e.cfg = Config{Name: "test-pc", CertFingerprint: testFp, OnPairRequest: func(p PairPrompt) { e.prompt = &p }}
	e.create = createPairRequestHandler(e.cfg, db, e.nonces)
	e.poll = pollPairRequestHandler(e.cfg, db)
	e.revealH = revealPairRequestHandler(e.cfg, db)
	return e
}

// commitOf is the commitment a phone sends for clientNonce: hex SHA-256 of its bytes.
func commitOf(clientNonce string) string {
	raw, err := hex.DecodeString(clientNonce)
	if err != nil {
		return clientNonce // deliberately malformed, for the validation test
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// reveal sends the nonce for request id.
func (e *pairReqEnv) reveal(id, clientNonce string) *httptest.ResponseRecorder {
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/pair/request/"+id+"/reveal", strings.NewReader(`{"clientNonce":"`+clientNonce+`"}`))
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", id)
	req = req.WithContext(withChiCtx(req, rctx))
	e.revealH(rr, req)
	return rr
}

func (e *pairReqEnv) request(clientNonce string) *httptest.ResponseRecorder {
	pub, nonce, sig := signedDeviceProofFor(e.t, e.nonces, testFp)
	body := `{"deviceLabel":"Pixel","devicePublicKey":"` + pub + `","nonce":"` + nonce + `","signature":"` + sig + `","clientNonceCommit":"` + commitOf(clientNonce) + `"}`
	rr := httptest.NewRecorder()
	e.create(rr, httptest.NewRequest(http.MethodPost, "/v1/pair/request", strings.NewReader(body)))
	return rr
}

func (e *pairReqEnv) pollStatus(id, nonce string) (int, map[string]any) {
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/pair/request/"+id+"?nonce="+nonce, nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", id)
	req = req.WithContext(withChiCtx(req, rctx))
	e.poll(rr, req)
	var out map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &out)
	return rr.Code, out
}

func TestPairRequest_ApprovedFlowMintsTokenOnce(t *testing.T) {
	e := newPairReqEnv(t)
	rr := e.request(testCNonc)
	if rr.Code != http.StatusOK {
		t.Fatalf("create: %d %s", rr.Code, rr.Body.String())
	}
	var created struct{ RequestID string }
	_ = json.Unmarshal(rr.Body.Bytes(), &created)
	if created.RequestID == "" || e.prompt != nil {
		t.Fatalf("want an id and no prompt before the reveal: %+v %+v", created, e.prompt)
	}
	if code, _ := e.pollStatus(created.RequestID, testCNonc); code != http.StatusNotFound {
		t.Fatalf("an unrevealed request must not be pollable, got %d", code)
	}
	if rv := e.reveal(created.RequestID, strings.Repeat("ff", 16)); rv.Code != http.StatusNotFound || e.prompt != nil {
		t.Fatalf("a nonce that does not match the commitment must be refused: %d", rv.Code)
	}
	if rv := e.reveal(created.RequestID, testCNonc); rv.Code != http.StatusOK || e.prompt == nil {
		t.Fatalf("reveal: %d %s", rv.Code, rv.Body.String())
	}
	if rv := e.reveal(created.RequestID, testCNonc); rv.Code != http.StatusNotFound {
		t.Fatalf("reveal works once, second got %d", rv.Code)
	}
	want, _ := pairing.SAS(testFp, testCNonc, created.RequestID)
	if e.prompt.SAS != want || e.prompt.Label != "Pixel" {
		t.Fatalf("prompt %+v, want SAS %s", e.prompt, want)
	}
	if strings.Contains(rr.Body.String(), want) || strings.Contains(rr.Body.String(), testFp) {
		t.Fatal("create response must not carry the match code or the fingerprint")
	}

	if code, out := e.pollStatus(created.RequestID, testCNonc); code != 200 || out["status"] != "pending" {
		t.Fatalf("pending poll: %d %v", code, out)
	}
	if err := e.db.DecidePairRequest(created.RequestID, true); err != nil {
		t.Fatal(err)
	}
	code, out := e.pollStatus(created.RequestID, testCNonc)
	if code != 200 || out["status"] != "approved" || out["deviceToken"] == "" || out["deviceId"] == "" {
		t.Fatalf("approved poll: %d %v", code, out)
	}
	if code, _ := e.pollStatus(created.RequestID, testCNonc); code != http.StatusNotFound {
		t.Fatalf("token must be collectable once, second poll got %d", code)
	}
}

func TestPairRequest_RejectedAndWrongNonce(t *testing.T) {
	e := newPairReqEnv(t)
	var created struct{ RequestID string }
	_ = json.Unmarshal(e.request(testCNonc).Body.Bytes(), &created)
	if rv := e.reveal(created.RequestID, testCNonc); rv.Code != http.StatusOK {
		t.Fatalf("reveal: %d", rv.Code)
	}
	if code, _ := e.pollStatus(created.RequestID, strings.Repeat("ff", 16)); code != http.StatusNotFound {
		t.Fatalf("wrong nonce got %d", code)
	}
	if err := e.db.DecidePairRequest(created.RequestID, false); err != nil {
		t.Fatal(err)
	}
	if _, out := e.pollStatus(created.RequestID, testCNonc); out["status"] != "rejected" {
		t.Fatalf("got %v", out)
	}
	if e.db.DecidePairRequest(created.RequestID, true) == nil {
		t.Fatal("a decided request must not be re-decided")
	}
	if devs, _ := e.db.ListDevices(); len(devs) != 0 {
		t.Fatalf("rejection must not create a device: %v", devs)
	}
}

func TestPairRequest_ValidatesAndBoundsPending(t *testing.T) {
	e := newPairReqEnv(t)
	if rr := e.request("nothex"); rr.Code != http.StatusBadRequest {
		t.Fatalf("bad clientNonce: %d", rr.Code)
	}
	rr := httptest.NewRecorder()
	e.create(rr, httptest.NewRequest(http.MethodPost, "/v1/pair/request", strings.NewReader(`{"clientNonce":"`+testCNonc+`"}`)))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("missing proof: %d", rr.Code)
	}
	for i := 0; i < store.MaxPendingPairRequests; i++ {
		if rr := e.request(testCNonc); rr.Code != http.StatusOK {
			t.Fatalf("request %d: %d %s", i, rr.Code, rr.Body.String())
		}
	}
	if rr := e.request(testCNonc); rr.Code != http.StatusTooManyRequests {
		t.Fatalf("over the pending cap: %d", rr.Code)
	}
}

func TestSanitizeLabel(t *testing.T) {
	if got := sanitizeLabel("a\nb\x00c"); got != "abc" {
		t.Fatalf("got %q", got)
	}
	if got := sanitizeLabel(strings.Repeat("x", 100)); len(got) != 40 {
		t.Fatalf("len %d", len(got))
	}
	if sanitizeLabel("\n") != "unnamed-device" {
		t.Fatal("empty label fallback")
	}
}

func withChiCtx(r *http.Request, rctx *chi.Context) context.Context {
	return context.WithValue(r.Context(), chi.RouteCtxKey, rctx)
}

// requestWith sends a pair request for clientID signed with the given key.
func (e *pairReqEnv) requestWith(clientID string, pub ed25519.PublicKey, priv ed25519.PrivateKey) (id string) {
	e.t.Helper()
	nonce, err := e.nonces.Mint()
	if err != nil {
		e.t.Fatal(err)
	}
	sig := base64.StdEncoding.EncodeToString(ed25519.Sign(priv, []byte(security.DeviceProofMessageV2(testFp, nonce))))
	body := `{"deviceLabel":"Pixel","deviceId":"` + clientID + `","devicePublicKey":"` + base64.StdEncoding.EncodeToString(pub) +
		`","nonce":"` + nonce + `","signature":"` + sig + `","clientNonceCommit":"` + commitOf(testCNonc) + `"}`
	rr := httptest.NewRecorder()
	e.create(rr, httptest.NewRequest(http.MethodPost, "/v1/pair/request", strings.NewReader(body)))
	if rr.Code != http.StatusOK {
		e.t.Fatalf("create: %d %s", rr.Code, rr.Body.String())
	}
	var created struct{ RequestID string }
	_ = json.Unmarshal(rr.Body.Bytes(), &created)
	if rv := e.reveal(created.RequestID, testCNonc); rv.Code != http.StatusOK {
		e.t.Fatalf("reveal: %d %s", rv.Code, rv.Body.String())
	}
	return created.RequestID
}

// approveAndCollect answers the request at the "computer" and polls for the result.
func (e *pairReqEnv) approveAndCollect(id string) string {
	e.t.Helper()
	if err := e.db.DecidePairRequest(id, true); err != nil {
		e.t.Fatal(err)
	}
	code, out := e.pollStatus(id, testCNonc)
	if code != 200 || out["status"] != "approved" {
		e.t.Fatalf("poll: %d %v", code, out)
	}
	return out["deviceId"].(string)
}

// seedPairedDevice pairs "phone-1" with a full set of grants and returns its key.
func (e *pairReqEnv) seedPairedDevice() (ed25519.PublicKey, ed25519.PrivateKey, string) {
	e.t.Helper()
	pub, priv, _ := ed25519.GenerateKey(nil)
	id, err := e.db.UpsertDevice("phone-1", "Owner phone", "old-token", base64.StdEncoding.EncodeToString(pub), false)
	if err != nil {
		e.t.Fatal(err)
	}
	if err := e.db.SetDeviceFilePermissions(id, true, true, true, true, true, true); err != nil {
		e.t.Fatal(err)
	}
	if err := e.db.SetDeviceAppPermissions(id, true, true); err != nil {
		e.t.Fatal(err)
	}
	return pub, priv, id
}

func TestPairRequest_NewKeyForKnownClientReplacesDeviceAndResetsAccess(t *testing.T) {
	e := newPairReqEnv(t)
	_, _, id := e.seedPairedDevice()
	otherPub, otherPriv, _ := ed25519.GenerateKey(nil)

	reqID := e.requestWith("phone-1", otherPub, otherPriv)
	if e.prompt == nil || e.prompt.Replaces != "Owner phone" {
		t.Fatalf("prompt must say which device it replaces: %+v", e.prompt)
	}
	if got := e.approveAndCollect(reqID); got != id {
		t.Fatalf("row is reused: got %s want %s", got, id)
	}
	d, err := e.db.GetDeviceByID(id)
	if err != nil {
		t.Fatal(err)
	}
	if !d.CanBrowse || d.CanDownload || d.CanUpload || d.CanModify || d.CanDelete || d.CanShare || d.ViewApps || d.LaunchApps {
		t.Fatalf("takeover must not inherit grants: %+v", d)
	}
	if d.PublicKey != base64.StdEncoding.EncodeToString(otherPub) {
		t.Fatal("new key not pinned")
	}
}

func TestPairRequest_RevokedDeviceReEnabledOnlyWithResetAndWarning(t *testing.T) {
	e := newPairReqEnv(t)
	pub, priv, id := e.seedPairedDevice()
	if err := e.db.RevokeDevice(id); err != nil {
		t.Fatal(err)
	}
	reqID := e.requestWith("phone-1", pub, priv)
	if e.prompt == nil || e.prompt.Replaces != "Owner phone" {
		t.Fatalf("a revoked device coming back must be flagged: %+v", e.prompt)
	}
	e.approveAndCollect(reqID)
	d, _ := e.db.GetDeviceByID(id)
	if d.Revoked || d.CanDownload || d.CanDelete || d.ViewApps {
		t.Fatalf("re-enabled device must start browse-only: %+v", d)
	}
}

func TestPairRequest_SameKeyRepairKeepsGrantsAndIsNotFlagged(t *testing.T) {
	e := newPairReqEnv(t)
	pub, priv, id := e.seedPairedDevice()
	reqID := e.requestWith("phone-1", pub, priv)
	if e.prompt == nil || e.prompt.Replaces != "" {
		t.Fatalf("a phone re-pairing with its own key is not a replacement: %+v", e.prompt)
	}
	e.approveAndCollect(reqID)
	d, _ := e.db.GetDeviceByID(id)
	if !d.CanDownload || !d.CanDelete || !d.ViewApps {
		t.Fatalf("same-key re-pair keeps its grants: %+v", d)
	}
}
