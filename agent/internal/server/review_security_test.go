package server

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/pairing"
)

func TestRegisterHandler_OrdinaryCodeCannotCreateOwner(t *testing.T) {
	db, _ := newTestDeps(t)
	pm := pairing.New(db, "127.0.0.1:8765", "", "sha256-fp")
	cfg := Config{Name: "test-pc", CertFingerprint: "sha256-fp"}
	nonces := newNonceStore()
	code, _, err := pm.Mint(time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	pubKey, nonce, sig := signedDeviceProof(t, nonces)
	body := `{"pairingCode":"` + code + `","username":"owner","password":"correct-horse-battery",` +
		`"deviceLabel":"p","deviceId":"android-1",` +
		`"devicePublicKey":"` + pubKey + `","nonce":"` + nonce + `","signature":"` + sig + `"}`
	rr := httptest.NewRecorder()
	registerHandler(cfg, db, pm, nonces)(rr, httptest.NewRequest(http.MethodPost, "/v1/register", strings.NewReader(body)))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rr.Code, rr.Body.String())
	}
	if has, _ := db.HasAnyUser(); has {
		t.Fatal("no account may be created from an ordinary code")
	}
}

func serveToken(t *testing.T, h http.HandlerFunc, token string) int {
	t.Helper()
	rr := httptest.NewRecorder()
	req := withURLParam(httptest.NewRequest(http.MethodGet, "/v1/share/"+token, nil), map[string]string{"token": token})
	h(rr, req)
	return rr.Code
}

func TestServeShare_RechecksMintingDevice(t *testing.T) {
	ops, root := newFsFixture(t)
	db, _ := newTestDeps(t)
	file := filepath.Join(root, "a.txt")

	ids := map[string]string{}
	mkDevice := func(name string) {
		id, err := db.UpsertDevice(name, name, "tok-"+name, "", false)
		if err != nil {
			t.Fatal(err)
		}
		ids[name] = id
		if err := db.SetDeviceFilePermissions(id, true, true, false, false, false, true); err != nil {
			t.Fatal(err)
		}
	}
	mint := func(token, dev string) {
		if err := db.CreateShareToken(hashShareToken(token), file, ids[dev], time.Now().Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	enabled := true
	h := serveShareHandler(db, ops, func() bool { return enabled })

	mkDevice("dev-ok")
	mint("t-ok", "dev-ok")
	if got := serveToken(t, h, "t-ok"); got != http.StatusOK {
		t.Fatalf("link from a healthy device must serve, got %d", got)
	}

	mkDevice("dev-share-off")
	mint("t-off", "dev-share-off")
	if err := db.SetDeviceFilePermissions(ids["dev-share-off"], true, true, false, false, false, false); err != nil {
		t.Fatal(err)
	}
	// Turning share off deletes tokens at the store; re-insert to prove the
	// serve path checks the grant itself.
	mint("t-off", "dev-share-off")
	if got := serveToken(t, h, "t-off"); got != http.StatusNotFound {
		t.Fatalf("link from a device without share must 404, got %d", got)
	}

	mkDevice("dev-gone")
	mint("t-gone", "dev-gone")
	if err := db.RevokeDevice(ids["dev-gone"]); err != nil {
		t.Fatal(err)
	}
	if got := serveToken(t, h, "t-gone"); got != http.StatusNotFound {
		t.Fatalf("link from a revoked device must 404, got %d", got)
	}
	mint("t-gone2", "dev-gone") // survives only if revoke did not purge; serve still refuses
	if got := serveToken(t, h, "t-gone2"); got != http.StatusNotFound {
		t.Fatalf("revoked device's link must 404, got %d", got)
	}

	mkDevice("dev-global")
	mint("t-global", "dev-global")
	enabled = false
	if got := serveToken(t, h, "t-global"); got != http.StatusNotFound {
		t.Fatalf("link must 404 while sharing is disabled, got %d", got)
	}
}

func TestRevokeAndDeleteDevice_PurgeShareTokens(t *testing.T) {
	db, _ := newTestDeps(t)
	ids := map[string]string{}
	for _, n := range []string{"a", "b"} {
		id, err := db.UpsertDevice(n, n, "tok-"+n, "", false)
		if err != nil {
			t.Fatal(err)
		}
		ids[n] = id
		if err := db.CreateShareToken("h-"+n, "/x", id, time.Now().Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.RevokeDevice(ids["a"]); err != nil {
		t.Fatal(err)
	}
	if err := db.DeleteDevice(ids["b"]); err != nil {
		t.Fatal(err)
	}
	for _, h := range []string{"h-a", "h-b"} {
		if tk, _ := db.GetShareToken(h); tk != nil {
			t.Fatalf("token %s must be purged", h)
		}
	}
}

func TestCodePair_TakeoverDoesNotInheritGrants(t *testing.T) {
	db, _ := newTestDeps(t)
	id, err := db.UpsertDevice("android-9", "victim", "tok", "KEY-A", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetDeviceFilePermissions(id, true, true, true, true, true, true); err != nil {
		t.Fatal(err)
	}
	replaces, err := pairReplaces(db, "android-9", "KEY-B")
	if err != nil || replaces == "" {
		t.Fatalf("a different key on an existing id must count as a takeover: %q %v", replaces, err)
	}
}
