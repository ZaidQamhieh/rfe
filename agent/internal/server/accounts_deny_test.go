package server

import (
	"net/http"
	"strings"
	"testing"
)

// A federated device the owner revoked is not revived by a fresh assertion for the same device id.
func TestFederatedLogin_OwnerRevokedFederatedDeviceStaysRevoked(t *testing.T) {
	f := newFederation(t)
	p := f.memberProof(t)
	if rr := f.login(f.sign(t, f.claims(p)), p); rr.Code != http.StatusOK {
		t.Fatalf("first login = %d %s", rr.Code, rr.Body.String())
	}
	devs, _ := f.memberDB.ListDevices()
	if len(devs) != 1 {
		t.Fatalf("want one device, got %d", len(devs))
	}
	if err := f.memberDB.RevokeDevice(devs[0].ID); err != nil {
		t.Fatal(err)
	}
	p2 := f.memberProof(t)
	rr := f.login(f.sign(t, f.claims(p2)), p2)
	if rr.Code != http.StatusConflict || accCode(t, rr) != "PAIR_REPLACES" {
		t.Fatalf("login = %d %s, want 409 PAIR_REPLACES", rr.Code, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), "deviceToken") {
		t.Fatal("a token came back with the refusal")
	}
}

// A member on the deny list is refused at login with 403 FORBIDDEN and gets no token or device row; allowing
// the member again lets the same login through.
func TestFederatedLogin_DeniedMemberIsForbidden(t *testing.T) {
	f := newFederation(t)
	if err := f.memberDB.DenyAccount(f.keyFp, "alice"); err != nil {
		t.Fatal(err)
	}
	p := f.memberProof(t)
	rr := f.login(f.sign(t, f.claims(p)), p)
	if rr.Code != http.StatusForbidden || accCode(t, rr) != "FORBIDDEN" {
		t.Fatalf("login = %d %s, want 403 FORBIDDEN", rr.Code, rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), "deviceToken") {
		t.Fatal("a token came back with the refusal")
	}
	if devs, _ := f.memberDB.ListDevices(); len(devs) != 0 {
		t.Fatalf("denied login left %d device rows", len(devs))
	}
	if err := f.memberDB.AllowAccount(f.keyFp, "alice"); err != nil {
		t.Fatal(err)
	}
	p2 := f.memberProof(t)
	if rr := f.login(f.sign(t, f.claims(p2)), p2); rr.Code != http.StatusOK {
		t.Fatalf("login after allow = %d %s", rr.Code, rr.Body.String())
	}
}
