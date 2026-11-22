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
)

// pairReplacesEnv drives the code-based POST /v1/pair handler and the approve-on-PC handlers over one database.
type pairReplacesEnv struct {
	*pairReqEnv
	pm   *pairing.Manager
	pair http.HandlerFunc
}

func newPairReplacesEnv(t *testing.T) *pairReplacesEnv {
	t.Helper()
	e := newPairReqEnv(t)
	pm := pairing.New(e.db, "127.0.0.1:8765", "", "fingerprint")
	return &pairReplacesEnv{pairReqEnv: e, pm: pm, pair: pairHandler(e.cfg, e.db, pm, e.nonces)}
}

// pairWithCode mints a fresh code and posts /v1/pair for deviceID presenting key pub (signed by priv).
func (e *pairReplacesEnv) pairWithCode(deviceID string, pub ed25519.PublicKey, priv ed25519.PrivateKey) (string, *httptest.ResponseRecorder) {
	e.t.Helper()
	code, _, err := e.pm.Mint(time.Minute)
	if err != nil {
		e.t.Fatalf("mint: %v", err)
	}
	nonce, err := e.nonces.Mint()
	if err != nil {
		e.t.Fatal(err)
	}
	sig := base64.StdEncoding.EncodeToString(ed25519.Sign(priv, []byte(deviceProofMessage(nonce))))
	body := `{"pairingCode":"` + code + `","deviceLabel":"New phone","deviceId":"` + deviceID +
		`","devicePublicKey":"` + base64.StdEncoding.EncodeToString(pub) + `","nonce":"` + nonce + `","signature":"` + sig + `"}`
	rr := httptest.NewRecorder()
	e.pair(rr, httptest.NewRequest(http.MethodPost, "/v1/pair", strings.NewReader(body)))
	return code, rr
}

// seedPairedByCode pairs "phone-1" through the code flow, then gives it full grants and one share link.
func (e *pairReplacesEnv) seedPairedByCode(pub ed25519.PublicKey, priv ed25519.PrivateKey) (id, token string) {
	e.t.Helper()
	_, rr := e.pairWithCode("phone-1", pub, priv)
	if rr.Code != http.StatusOK {
		e.t.Fatalf("first pairing: %d %s", rr.Code, rr.Body.String())
	}
	var resp pairResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		e.t.Fatal(err)
	}
	if err := e.db.SetDeviceFilePermissions(resp.DeviceID, true, true, true, true, true, true); err != nil {
		e.t.Fatal(err)
	}
	if err := e.db.SetDeviceAppPermissions(resp.DeviceID, true, true); err != nil {
		e.t.Fatal(err)
	}
	if err := e.db.CreateShareToken("share-hash", "/docs/a.txt", resp.DeviceID, time.Now().Add(time.Hour)); err != nil {
		e.t.Fatal(err)
	}
	return resp.DeviceID, resp.DeviceToken
}

// deviceProofMessage is what a phone signs for nonce under this test agent's certificate fingerprint.
func deviceProofMessage(nonce string) string {
	return security.DeviceProofMessageV2(testFp, nonce)
}

func TestPairCode_DifferentKeyForKnownDeviceIsRefusedAndChangesNothing(t *testing.T) {
	e := newPairReplacesEnv(t)
	pubA, privA, _ := ed25519.GenerateKey(nil)
	id, token := e.seedPairedByCode(pubA, privA)
	before, err := e.db.GetDeviceByID(id)
	if err != nil {
		t.Fatal(err)
	}

	pubB, privB, _ := ed25519.GenerateKey(nil)
	code, rr := e.pairWithCode("phone-1", pubB, privB)
	if rr.Code != http.StatusConflict {
		t.Fatalf("want 409, got %d: %s", rr.Code, rr.Body.String())
	}
	var body struct{ Code, Message string }
	_ = json.Unmarshal(rr.Body.Bytes(), &body)
	if body.Code != "PAIR_REPLACES" {
		t.Fatalf("want code PAIR_REPLACES, got %q (%s)", body.Code, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), "New phone") || strings.Contains(rr.Body.String(), before.Label) || strings.Contains(rr.Body.String(), token) {
		t.Fatalf("the refusal must not echo device labels or tokens: %s", rr.Body.String())
	}

	// Victim row untouched: same token, key, label, grants and share link.
	d, err := e.db.DeviceByToken(token)
	if err != nil || d == nil || d.ID != id {
		t.Fatalf("A's token must still authenticate: %+v, %v", d, err)
	}
	after, err := e.db.GetDeviceByID(id)
	if err != nil {
		t.Fatal(err)
	}
	after.LastSeen, before.LastSeen = time.Time{}, time.Time{} // a token lookup refreshes last_seen only
	if *after != *before {
		t.Fatalf("device row changed:\n before %+v\n after  %+v", before, after)
	}
	if after.PublicKey != base64.StdEncoding.EncodeToString(pubA) {
		t.Fatal("A's pinned key must not change")
	}
	if !after.CanDownload || !after.CanUpload || !after.CanModify || !after.CanDelete || !after.CanShare || !after.ViewApps || !after.LaunchApps {
		t.Fatalf("A's grants must be unchanged: %+v", after)
	}
	shares, err := e.db.ListShareTokens(id)
	if err != nil || len(shares) != 1 {
		t.Fatalf("A's share link must survive: %v, %v", shares, err)
	}
	// The refusal leaves the one-time code usable (for another, new device).
	if !e.pm.Peek(code).Valid {
		t.Fatal("a refused replacement must not burn the pairing code")
	}
}

func TestPairCode_SameKeyRepairAndNewDeviceStillWork(t *testing.T) {
	e := newPairReplacesEnv(t)
	pubA, privA, _ := ed25519.GenerateKey(nil)
	id, oldToken := e.seedPairedByCode(pubA, privA)

	_, rr := e.pairWithCode("phone-1", pubA, privA)
	if rr.Code != http.StatusOK {
		t.Fatalf("same-key re-pair: want 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var resp pairResponse
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp.DeviceID != id || resp.DeviceToken == oldToken {
		t.Fatalf("same-key re-pair reuses the row with a new token: %+v", resp)
	}
	d, _ := e.db.GetDeviceByID(id)
	if !d.CanDownload || !d.CanDelete || !d.ViewApps {
		t.Fatalf("same-key re-pair keeps its grants: %+v", d)
	}

	pubC, privC, _ := ed25519.GenerateKey(nil)
	if _, rr := e.pairWithCode("phone-2", pubC, privC); rr.Code != http.StatusOK {
		t.Fatalf("a new device id pairs normally: %d %s", rr.Code, rr.Body.String())
	}
}

func TestPairCode_RefusedReplacementStillGoesThroughApproveOnPC(t *testing.T) {
	e := newPairReplacesEnv(t)
	pubA, privA, _ := ed25519.GenerateKey(nil)
	id, token := e.seedPairedByCode(pubA, privA)
	pubB, privB, _ := ed25519.GenerateKey(nil)

	if _, rr := e.pairWithCode("phone-1", pubB, privB); rr.Code != http.StatusConflict {
		t.Fatalf("code flow: want 409, got %d", rr.Code)
	}
	reqID := e.requestWith("phone-1", pubB, privB)
	if e.prompt == nil || e.prompt.Replaces != "New phone" {
		t.Fatalf("the owner must be told which device the request replaces: %+v", e.prompt)
	}
	// Until the owner answers, A is untouched.
	if d, err := e.db.DeviceByToken(token); err != nil || d == nil {
		t.Fatalf("A's token must work until the owner approves: %v", err)
	}
	if got := e.approveAndCollect(reqID); got != id {
		t.Fatalf("approval reuses the row: got %s want %s", got, id)
	}
	d, _ := e.db.GetDeviceByID(id)
	if d.PublicKey != base64.StdEncoding.EncodeToString(pubB) {
		t.Fatal("approved key not pinned")
	}
	if d.CanDownload || d.CanDelete || d.ViewApps {
		t.Fatalf("approved replacement starts browse-only: %+v", d)
	}
	if shares, _ := e.db.ListShareTokens(id); len(shares) != 0 {
		t.Fatalf("approved replacement drops the old device's shares: %v", shares)
	}
}
