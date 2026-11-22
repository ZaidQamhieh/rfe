package server

import (
	"net/http"
	"strings"
	"testing"
)

// A device the owner revoked on this computer must stay revoked when a federated assertion names its id:
// the login is refused with 409 PAIR_REPLACES and no token is issued.
func TestFederatedLogin_RevokedDeviceStaysRevoked(t *testing.T) {
	f := newFederation(t)
	p := f.memberProof(t)
	id, err := f.memberDB.UpsertDevice("fed-client", "old phone", "tok-old", p.pub, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.memberDB.RevokeDevice(id); err != nil {
		t.Fatal(err)
	}
	rr := f.login(f.sign(t, f.claims(p)), p)
	if rr.Code != http.StatusConflict || accCode(t, rr) != "PAIR_REPLACES" {
		t.Fatalf("login = %d %s, want 409 PAIR_REPLACES", rr.Code, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), "deviceToken") {
		t.Fatal("a token came back with the refusal")
	}
	d, err := f.memberDB.GetDeviceByID(id)
	if err != nil || d == nil || !d.Revoked {
		t.Fatalf("device after refused login = %+v, %v; want still revoked", d, err)
	}
	if dev, _ := f.memberDB.DeviceByToken("tok-old"); dev == nil || !dev.Revoked {
		t.Fatalf("old token = %+v, want still revoked", dev)
	}
}

// A device that was code- or owner-paired with app and wake rights loses them when a federated login
// takes over its row: a federated device never holds app flags or the wake grant.
func TestFederatedLogin_ResetsAppAndWakeRightsOfEarlierPairing(t *testing.T) {
	f := newFederation(t)
	p := f.memberProof(t)
	id, err := f.memberDB.UpsertDevice("fed-client", "old phone", "tok-old", p.pub, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.memberDB.SetDeviceAppPermissions(id, true, true); err != nil {
		t.Fatal(err)
	}
	if err := f.memberDB.SetDeviceGrants(id, true, true, true, true, true, true, true); err != nil {
		t.Fatal(err)
	}
	rr := f.login(f.sign(t, f.claims(p)), p)
	if rr.Code != http.StatusOK {
		t.Fatalf("login = %d: %s", rr.Code, rr.Body.String())
	}
	d, err := f.memberDB.GetDeviceByID(id)
	if err != nil || d == nil {
		t.Fatalf("device = %+v, %v", d, err)
	}
	if d.ViewApps || d.LaunchApps || d.CanWake || d.ViaLogin || d.Revoked {
		t.Fatalf("device kept rights after federated login: %+v", d)
	}
}
