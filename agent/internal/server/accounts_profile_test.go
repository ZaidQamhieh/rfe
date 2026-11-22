package server

import (
	"crypto/ed25519"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ZaidQamhieh/rfe/agent/internal/security"
	"github.com/ZaidQamhieh/rfe/agent/internal/store"
)

// fedSession logs the federation's phone into the member agent with a fresh assertion at the given epoch and
// returns the member-side device token.
func (f *federation) fedSession(t *testing.T, epoch int64) string {
	t.Helper()
	p := f.memberProof(t)
	c := f.claims(p)
	c.Epoch = epoch
	rr := f.login(f.sign(t, c), p)
	if rr.Code != http.StatusOK {
		t.Fatalf("login = %d: %s", rr.Code, rr.Body.String())
	}
	tok, _ := accJSON(t, rr)["deviceToken"].(string)
	if tok == "" {
		t.Fatalf("no deviceToken: %s", rr.Body.String())
	}
	return tok
}

func TestFederatedSession_GrantProfileIsEnforcedOnFileRoutes(t *testing.T) {
	f := newFederation(t)
	tok := f.fedSession(t, 0)
	allowed := []struct{ method, path string }{
		{http.MethodGet, "/v1/fs?path=/"},
		{http.MethodGet, "/v1/content?path=/nonexistent"},
	}
	for _, c := range allowed {
		rr := accCall(f.member, c.method, c.path, tok, "")
		if rr.Code == http.StatusForbidden {
			t.Errorf("%s %s = 403 %s, want it allowed by the profile", c.method, c.path, rr.Body.String())
		}
	}
	denied := []struct{ method, path string }{
		{http.MethodDelete, "/v1/fs?path=/x"},
		{http.MethodPost, "/v1/fs/folder"},
		{http.MethodPatch, "/v1/fs/rename"},
		{http.MethodPut, "/v1/content?path=/x"},
		{http.MethodPost, "/v1/share/mint"},
	}
	for _, c := range denied {
		rr := accCall(f.member, c.method, c.path, tok, "{}")
		if rr.Code != http.StatusForbidden || accCode(t, rr) != "CAPABILITY_DENIED" {
			t.Errorf("%s %s = %d %s, want 403 CAPABILITY_DENIED", c.method, c.path, rr.Code, rr.Body.String())
		}
	}
	// Upload is part of the profile: the capability check passes for the stored device.
	dev, err := f.memberDB.DeviceByToken(tok)
	if err != nil || dev == nil {
		t.Fatalf("device: %+v %v", dev, err)
	}
	r := httptest.NewRequest(http.MethodPost, "/v1/uploads", nil)
	r = r.WithContext(withDevice(r.Context(), dev))
	if !requireDeviceFileCapabilities(httptest.NewRecorder(), r, capBrowse, capDownload, capUpload) {
		t.Error("federated session lacks browse/download/upload")
	}
}

func TestFederatedSession_RefusedAdminRoutes(t *testing.T) {
	f := newFederation(t)
	tok := f.fedSession(t, 0)
	if isAdminDevice(mustDevice(t, f.memberDB, tok)) {
		t.Fatal("a federated device must not be an admin device")
	}
	for _, c := range []struct{ method, path string }{
		{http.MethodGet, "/v1/audit"},
		{http.MethodGet, "/v1/users"},
		{http.MethodDelete, "/v1/users/alice"},
		{http.MethodPatch, "/v1/settings"},
		{http.MethodPost, "/v1/pairing/generate"},
		{http.MethodGet, "/v1/settings/account-server"},
		{http.MethodPut, "/v1/settings/account-server"},
	} {
		rr := accCall(f.member, c.method, c.path, tok, "{}")
		if rr.Code != http.StatusForbidden {
			t.Errorf("%s %s = %d %s, want 403", c.method, c.path, rr.Code, rr.Body.String())
		}
	}
}

func mustDevice(t *testing.T, db *store.DB, tok string) *store.Device {
	t.Helper()
	d, err := db.DeviceByToken(tok)
	if err != nil || d == nil {
		t.Fatalf("DeviceByToken = %+v, %v", d, err)
	}
	return d
}

func TestPasswordOwner_UnchangedByGrantProfile(t *testing.T) {
	f := newFederation(t)
	if rr := accCall(f.srv, http.MethodGet, "/v1/audit", f.adminToken, ""); rr.Code != http.StatusOK {
		t.Fatalf("owner audit = %d", rr.Code)
	}
	dev := mustDevice(t, f.srvDB, f.adminToken)
	if !dev.ViaLogin || !isAdminDevice(dev) {
		t.Fatalf("owner device = %+v", dev)
	}
	rr := accCall(f.srv, http.MethodDelete, "/v1/fs?path=/x", f.adminToken, "")
	if rr.Code == http.StatusForbidden && accCode(t, rr) == "CAPABILITY_DENIED" {
		t.Fatal("the password owner lost the file-grant bypass")
	}
}

func TestAssertionIssue_CarriesAccountEpoch(t *testing.T) {
	f := newFederation(t)
	if _, err := f.srvDB.BumpAccountEpoch("test"); err != nil {
		t.Fatal(err)
	}
	p := f.memberProof(t)
	rr := accCall(f.srv, http.MethodPost, "/v1/accounts/assertion", f.adminToken, issueBody(acctMemberCertFp, p.pub, p.nonce))
	tok, _ := accJSON(t, rr)["assertion"].(string)
	if tok == "" {
		t.Fatalf("no assertion: %s", rr.Body.String())
	}
	priv, _ := f.srvDB.AccountSigningKey()
	got, err := security.VerifyAssertionSignature(priv.Public().(ed25519.PublicKey), tok)
	if err != nil || got.Epoch != 1 {
		t.Fatalf("claims = %+v, %v; want Epoch 1", got, err)
	}
}

func TestFederatedLogin_StaleEpochRejectedHigherAccepted(t *testing.T) {
	f := newFederation(t)
	f.fedSession(t, 2)
	p := f.memberProof(t)
	c := f.claims(p)
	c.Epoch = 1
	f.expectRejected(t, f.login(f.sign(t, c), p))
	trusted, _ := f.memberDB.TrustedAccountServer()
	if trusted == nil || trusted.HighestEpoch != 2 {
		t.Fatalf("trusted = %+v", trusted)
	}
	// Equal and higher epochs still log in.
	f.fedSession(t, 2)
	f.fedSession(t, 3)
}

func TestFederatedLogin_HigherEpochEndsOlderSession(t *testing.T) {
	f := newFederation(t)
	old := f.fedSession(t, 0)
	if d, _ := f.memberDB.DeviceByToken(old); d == nil {
		t.Fatal("first session not usable")
	}
	// A second phone logs in after the issuer raised its epoch: the first phone's session is gone.
	if _, err := f.memberDB.LoginDeviceFederatedAtEpoch("other", "tablet", "tok-other", "pk-other", "alice", f.keyFp, 1); err != nil {
		t.Fatal(err)
	}
	if d, _ := f.memberDB.DeviceByToken(old); d != nil {
		t.Fatal("older-epoch session still valid after a higher-epoch login")
	}
	if rr := accCall(f.member, http.MethodGet, "/v1/fs?path=/", old, ""); rr.Code != http.StatusUnauthorized {
		t.Fatalf("old token = %d, want 401", rr.Code)
	}
}
