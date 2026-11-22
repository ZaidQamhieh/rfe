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
)

// registerReplacesEnv drives POST /v1/register over one database that already holds a paired device.
type registerReplacesEnv struct {
	t        *testing.T
	db       *store.DB
	pm       *pairing.Manager
	nonces   *nonceStore
	register http.HandlerFunc
}

func newRegisterReplacesEnv(t *testing.T) *registerReplacesEnv {
	t.Helper()
	db, _ := newTestDeps(t)
	pm := pairing.New(db, "127.0.0.1:8765", "", "fingerprint")
	cfg := Config{Name: "test-pc", CertFingerprint: testProofFp}
	nonces := newNonceStore()
	return &registerReplacesEnv{t: t, db: db, pm: pm, nonces: nonces, register: registerHandler(cfg, db, pm, nonces)}
}

// seedDevice stores "phone-1" pinned to pub with full grants and one share link, and returns its id.
func (e *registerReplacesEnv) seedDevice(pub ed25519.PublicKey) string {
	e.t.Helper()
	id, err := e.db.UpsertDevice("phone-1", "Old phone", "old-token", base64.StdEncoding.EncodeToString(pub), false)
	if err != nil {
		e.t.Fatal(err)
	}
	if err := e.db.SetDeviceFilePermissions(id, true, true, true, true, true, true); err != nil {
		e.t.Fatal(err)
	}
	if err := e.db.SetDeviceAppPermissions(id, true, true); err != nil {
		e.t.Fatal(err)
	}
	if err := e.db.CreateShareToken("share-hash", "/docs/a.txt", id, time.Now().Add(time.Hour)); err != nil {
		e.t.Fatal(err)
	}
	return id
}

// registerWithOwnerCode mints an owner code and posts /v1/register for deviceID presenting pub (signed by priv).
func (e *registerReplacesEnv) registerWithOwnerCode(deviceID string, pub ed25519.PublicKey, priv ed25519.PrivateKey) (string, *httptest.ResponseRecorder) {
	e.t.Helper()
	code, _, err := e.pm.MintOwner(time.Minute)
	if err != nil {
		e.t.Fatalf("mint: %v", err)
	}
	nonce, err := e.nonces.Mint()
	if err != nil {
		e.t.Fatal(err)
	}
	sig := base64.StdEncoding.EncodeToString(ed25519.Sign(priv, []byte(security.DeviceProofMessageV2(testProofFp, nonce))))
	body := `{"pairingCode":"` + code + `","username":"owner","password":"correct-horse-battery","deviceLabel":"New phone","deviceId":"` + deviceID +
		`","devicePublicKey":"` + base64.StdEncoding.EncodeToString(pub) + `","nonce":"` + nonce + `","signature":"` + sig + `"}`
	rr := httptest.NewRecorder()
	e.register(rr, httptest.NewRequest(http.MethodPost, "/v1/register", strings.NewReader(body)))
	return code, rr
}

// requireRefusedAndUntouched checks a 409 PAIR_REPLACES that created no account, left the device row, token,
// grants and share link as they were, and did not spend the owner code.
func (e *registerReplacesEnv) requireRefusedAndUntouched(id, code string, rr *httptest.ResponseRecorder, before *store.Device, wantKey string) {
	e.t.Helper()
	if rr.Code != http.StatusConflict {
		e.t.Fatalf("want 409, got %d: %s", rr.Code, rr.Body.String())
	}
	var body struct{ Code, Message string }
	_ = json.Unmarshal(rr.Body.Bytes(), &body)
	if body.Code != "PAIR_REPLACES" {
		e.t.Fatalf("want code PAIR_REPLACES, got %q (%s)", body.Code, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), "New phone") || strings.Contains(rr.Body.String(), before.Label) || strings.Contains(rr.Body.String(), "old-token") {
		e.t.Fatalf("the refusal must not echo device labels or tokens: %s", rr.Body.String())
	}
	if has, err := e.db.HasAnyUser(); err != nil || has {
		e.t.Fatalf("a refused register must create no account: has=%v err=%v", has, err)
	}
	after, err := e.db.GetDeviceByID(id)
	if err != nil {
		e.t.Fatal(err)
	}
	if *after != *before {
		e.t.Fatalf("device row changed:\n before %+v\n after  %+v", before, after)
	}
	if after.PublicKey != wantKey {
		e.t.Fatalf("pinned key changed: got %q want %q", after.PublicKey, wantKey)
	}
	if shares, err := e.db.ListShareTokens(id); err != nil || len(shares) != 1 {
		e.t.Fatalf("share link must survive: %v, %v", shares, err)
	}
	if !e.pm.Peek(code).Valid {
		e.t.Fatal("a refused replacement must not burn the owner code")
	}
}

func TestRegister_DifferentKeyForKnownDeviceIsRefusedAndChangesNothing(t *testing.T) {
	e := newRegisterReplacesEnv(t)
	pubA, _, _ := ed25519.GenerateKey(nil)
	id := e.seedDevice(pubA)
	before, err := e.db.GetDeviceByID(id)
	if err != nil {
		t.Fatal(err)
	}

	pubB, privB, _ := ed25519.GenerateKey(nil)
	code, rr := e.registerWithOwnerCode("phone-1", pubB, privB)
	e.requireRefusedAndUntouched(id, code, rr, before, base64.StdEncoding.EncodeToString(pubA))

	// The old token still authenticates: nothing was re-issued.
	if d, err := e.db.DeviceByToken("old-token"); err != nil || d == nil || d.ID != id {
		t.Fatalf("old token must still authenticate: %+v, %v", d, err)
	}
}

func TestRegister_RevokedDeviceWithDifferentKeyIsRefused(t *testing.T) {
	e := newRegisterReplacesEnv(t)
	pubA, _, _ := ed25519.GenerateKey(nil)
	id := e.seedDevice(pubA)
	if err := e.db.RevokeDevice(id); err != nil {
		t.Fatal(err)
	}
	// Revoking drops the device's share links; add one back so the check below can see it stay.
	if err := e.db.CreateShareToken("share-hash-2", "/docs/b.txt", id, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	before, err := e.db.GetDeviceByID(id)
	if err != nil {
		t.Fatal(err)
	}
	if !before.Revoked {
		t.Fatal("test setup: the device should be revoked")
	}

	pubB, privB, _ := ed25519.GenerateKey(nil)
	code, rr := e.registerWithOwnerCode("phone-1", pubB, privB)
	e.requireRefusedAndUntouched(id, code, rr, before, base64.StdEncoding.EncodeToString(pubA))
}

func TestRegister_SameKeyAndNewDeviceStillRegister(t *testing.T) {
	e := newRegisterReplacesEnv(t)
	pubA, privA, _ := ed25519.GenerateKey(nil)
	id := e.seedDevice(pubA)

	_, rr := e.registerWithOwnerCode("phone-1", pubA, privA)
	if rr.Code != http.StatusOK {
		t.Fatalf("same-key register: want 200, got %d: %s", rr.Code, rr.Body.String())
	}
	var resp pairResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.DeviceID != id || resp.DeviceToken == "old-token" {
		t.Fatalf("same-key register reuses the row with a new token: %+v", resp)
	}
	if has, err := e.db.HasAnyUser(); err != nil || !has {
		t.Fatalf("the account must exist: has=%v err=%v", has, err)
	}
	if d, _ := e.db.GetDeviceByID(id); !d.CanDownload || !d.CanDelete || !d.ViewApps {
		t.Fatalf("same-key register keeps the device's grants: %+v", d)
	}
}

func TestRegister_FreshDeviceIDRegistersNormally(t *testing.T) {
	e := newRegisterReplacesEnv(t)
	e.seedDevice(ed25519.PublicKey(make([]byte, ed25519.PublicKeySize)))

	pubC, privC, _ := ed25519.GenerateKey(nil)
	_, rr := e.registerWithOwnerCode("phone-2", pubC, privC)
	if rr.Code != http.StatusOK {
		t.Fatalf("a new device id registers normally: %d %s", rr.Code, rr.Body.String())
	}
	if has, err := e.db.HasAnyUser(); err != nil || !has {
		t.Fatalf("the account must exist: has=%v err=%v", has, err)
	}
}
