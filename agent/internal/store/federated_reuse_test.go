package store

import (
	"errors"
	"testing"
)

func TestLoginDeviceFederated_RefusesRevokedDevice(t *testing.T) {
	db, fp := federatedFixture(t)
	id, err := db.UpsertDevice("client-1", "phone", "tok-old", "pk", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.RevokeDevice(id); err != nil {
		t.Fatal(err)
	}
	if _, err := db.LoginDeviceFederated("client-1", "phone", "tok-new", "pk", "alice", fp); !errors.Is(err, ErrDeviceRevoked) {
		t.Fatalf("err = %v, want ErrDeviceRevoked", err)
	}
	if d, _ := db.GetDeviceByID(id); d == nil || !d.Revoked {
		t.Fatalf("device = %+v, want still revoked", d)
	}
	if dev, _ := db.DeviceByToken("tok-new"); dev != nil {
		t.Fatal("a token was minted for a revoked device")
	}
}

func TestLoginDeviceFederated_ResetsAppAndWakeRightsOfEarlierPairing(t *testing.T) {
	db, fp := federatedFixture(t)
	id, err := db.UpsertDevice("client-1", "phone", "tok-old", "pk", true)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetDeviceAppPermissions(id, true, true); err != nil {
		t.Fatal(err)
	}
	if err := db.SetDeviceGrants(id, true, true, true, true, true, true, true); err != nil {
		t.Fatal(err)
	}
	if _, err := db.LoginDeviceFederated("client-1", "phone", "tok-new", "pk", "alice", fp); err != nil {
		t.Fatal(err)
	}
	d, err := db.GetDeviceByID(id)
	if err != nil || d == nil {
		t.Fatalf("device = %+v, %v", d, err)
	}
	if d.ViewApps || d.LaunchApps || d.CanWake || d.ViaLogin {
		t.Fatalf("device kept rights: %+v", d)
	}
}
