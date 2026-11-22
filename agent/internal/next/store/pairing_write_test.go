package store

import (
	"errors"
	"testing"
)

// Fixed instants (Unix seconds) for the pairing store tests; nothing reads the real clock.
const (
	pwT0     int64 = 2_000_000
	pwCodeTT int64 = 600
)

// pwSpec is a code-device spec written at pwT0.
func pwSpec(id, client, token string) DeviceSpec {
	return DeviceSpec{ID: id, ClientID: client, Label: "phone", TokenHash: HashToken(token), PublicKey: "key-" + client, Origin: OriginCode, Now: pwT0}
}

// pwAddCode stores a code valid for pwCodeTT seconds.
func pwAddCode(t *testing.T, db *DB, hash string, c PairingCode) {
	t.Helper()
	c.Hash, c.Expires = hash, pwT0+pwCodeTT
	if err := db.AddPairingCode(c, pwT0); err != nil {
		t.Fatalf("AddPairingCode: %v", err)
	}
}

func TestPairWithCodeSpendsCodeOnceAndStartsBrowseOnly(t *testing.T) {
	db, _ := openTemp(t)
	pwAddCode(t, db, "h1", PairingCode{})
	c, saved, err := db.PairWithCode("h1", pwSpec("d1", "c1", "tok1"))
	if err != nil || saved.ID != "d1" || saved.Replaced || c.CanRegister {
		t.Fatalf("PairWithCode = %+v %+v %v", c, saved, err)
	}
	d, err := db.DeviceByToken("tok1")
	if err != nil || d == nil {
		t.Fatalf("DeviceByToken: %v %v", d, err)
	}
	if !d.CanBrowse || d.CanDownload || d.CanUpload || d.CanModify || d.CanDelete || d.CanShare || d.ViewApps || d.LaunchApps || d.IsAdmin() {
		t.Fatalf("a code device must be browse-only: %+v", d)
	}
	if _, _, err := db.PairWithCode("h1", pwSpec("d2", "c2", "tok2")); !errors.Is(err, ErrCodeInvalid) {
		t.Fatalf("second use of the code = %v, want ErrCodeInvalid", err)
	}
}

func TestPairWithCodeFailureLeavesCodeUsable(t *testing.T) {
	db, _ := openTemp(t)
	pwAddCode(t, db, "h1", PairingCode{})
	bad := pwSpec("d1", "c1", "tok1")
	bad.TokenHash = "" // the device write fails after the code was read
	if _, _, err := db.PairWithCode("h1", bad); err == nil {
		t.Fatal("expected the device write to fail")
	}
	if _, err := db.PeekPairingCode("h1", pwT0); err != nil {
		t.Fatalf("code was spent by a failed pairing: %v", err)
	}
}

func TestPairingCodeExpiresAndDuplicateIsRefused(t *testing.T) {
	db, _ := openTemp(t)
	pwAddCode(t, db, "h1", PairingCode{})
	if err := db.AddPairingCode(PairingCode{Hash: "h1", Expires: pwT0 + 1}, pwT0); !errors.Is(err, ErrCodeExists) {
		t.Fatalf("duplicate hash = %v, want ErrCodeExists", err)
	}
	if _, err := db.PeekPairingCode("h1", pwT0+pwCodeTT); err != nil {
		t.Fatalf("code must be valid at its expiry second: %v", err)
	}
	if _, err := db.PeekPairingCode("h1", pwT0+pwCodeTT+1); !errors.Is(err, ErrCodeInvalid) {
		t.Fatalf("expired code = %v, want ErrCodeInvalid", err)
	}
	if _, _, err := db.PairWithCode("h1", func() DeviceSpec { s := pwSpec("d1", "c1", "t"); s.Now = pwT0 + pwCodeTT + 1; return s }()); !errors.Is(err, ErrCodeInvalid) {
		t.Fatalf("pairing with an expired code = %v", err)
	}
}

func TestGuestCodeLimitsAreAppliedAtCreation(t *testing.T) {
	db, _ := openTemp(t)
	pwAddCode(t, db, "g", PairingCode{JailRoot: "/srv/guest", ReadOnly: true})
	if _, _, err := db.PairWithCode("g", pwSpec("d1", "c1", "tok")); err != nil {
		t.Fatal(err)
	}
	d, _ := db.DeviceByID("d1")
	if d == nil || d.JailRoot != "/srv/guest" || !d.ReadOnly {
		t.Fatalf("guest limits missing: %+v", d)
	}
}

func TestRepairingATakenOverRowResetsGrantsAndShareLinks(t *testing.T) {
	db, _ := openTemp(t)
	pwAddCode(t, db, "h1", PairingCode{})
	pwAddCode(t, db, "h2", PairingCode{})
	if _, _, err := db.PairWithCode("h1", pwSpec("d1", "c1", "tok1")); err != nil {
		t.Fatal(err)
	}
	if err := db.SetDeviceFileGrants("d1", [6]bool{true, true, true, true, true, true}); err != nil {
		t.Fatal(err)
	}
	if err := db.SetDeviceAppGrants("d1", true, true); err != nil {
		t.Fatal(err)
	}
	shareInsert(t, db, shareLinkFor("s1", "d1", shareHr))
	other := pwSpec("dX", "c1", "tok2")
	other.PublicKey = "another-key" // same client id, new key: a takeover
	_, saved, err := db.PairWithCode("h2", other)
	if err != nil || saved.ID != "d1" || !saved.Replaced {
		t.Fatalf("takeover = %+v %v", saved, err)
	}
	d, _ := db.DeviceByID("d1")
	if d.CanDownload || d.CanDelete || d.CanShare || d.ViewApps || d.LaunchApps || !d.CanBrowse {
		t.Fatalf("takeover kept grants: %+v", d)
	}
	if old, _ := db.DeviceByToken("tok1"); old != nil {
		t.Fatal("old token still works after takeover")
	}
	if l, _ := db.ShareLinkByHash("s1"); l != nil {
		t.Fatal("share link survived the takeover")
	}
}

func TestRegisterWithOrdinaryCodeLeavesCodeUnspentAndSecondAccountRefused(t *testing.T) {
	db, _ := openTemp(t)
	pwAddCode(t, db, "plain", PairingCode{})
	pwAddCode(t, db, "owner", PairingCode{CanRegister: true})
	reg := pwSpec("d1", "c1", "tok1")
	reg.Origin = OriginRegister
	if _, err := db.RegisterWithCode("plain", reg, User{Username: "me", PasswordHash: "x"}); !errors.Is(err, ErrCodeCannotRegister) {
		t.Fatalf("ordinary code register = %v", err)
	}
	if _, err := db.PeekPairingCode("plain", pwT0); err != nil {
		t.Fatalf("ordinary code was spent: %v", err)
	}
	if _, err := db.RegisterWithCode("owner", reg, User{Username: "me", PasswordHash: "x"}); err != nil {
		t.Fatal(err)
	}
	pwAddCode(t, db, "owner2", PairingCode{CanRegister: true})
	reg2 := pwSpec("d2", "c2", "tok2")
	reg2.Origin = OriginRegister
	if _, err := db.RegisterWithCode("owner2", reg2, User{Username: "you", PasswordHash: "y"}); !errors.Is(err, ErrAccountExists) {
		t.Fatalf("second account = %v", err)
	}
	if _, err := db.PeekPairingCode("owner2", pwT0); err != nil {
		t.Fatalf("code spent by a refused registration: %v", err)
	}
	d, _ := db.DeviceByToken("tok1")
	if d == nil || !d.IsAdmin() || !d.CanDownload || !d.CanDelete {
		t.Fatalf("registered device = %+v", d)
	}
}

func TestLoginDeviceRefusesChangedAccountAndWrongOrigin(t *testing.T) {
	db, _ := openTemp(t)
	pwAddCode(t, db, "owner", PairingCode{CanRegister: true})
	reg := pwSpec("d1", "c1", "tok1")
	reg.Origin = OriginRegister
	if _, err := db.RegisterWithCode("owner", reg, User{Username: "me", PasswordHash: "hash"}); err != nil {
		t.Fatal(err)
	}
	login := pwSpec("d9", "c9", "tok9")
	login.Origin, login.Username = OriginLogin, "me"
	if _, err := db.LoginDevice(login, "other-hash"); !errors.Is(err, ErrLoginAccountChanged) {
		t.Fatalf("changed hash = %v", err)
	}
	if _, err := db.LoginDevice(pwSpec("d9", "c9", "tok9"), "hash"); err == nil {
		t.Fatal("LoginDevice accepted a code origin")
	}
	if _, err := db.LoginDevice(login, "hash"); err != nil {
		t.Fatal(err)
	}
}

func TestRevokeAndDeleteDropShareLinksAndReportMissingDevice(t *testing.T) {
	db, _ := openTemp(t)
	pwAddCode(t, db, "h1", PairingCode{})
	if _, _, err := db.PairWithCode("h1", pwSpec("d1", "c1", "tok1")); err != nil {
		t.Fatal(err)
	}
	shareInsert(t, db, shareLinkFor("s1", "d1", shareHr))
	if err := db.RevokeDevice("d1"); err != nil {
		t.Fatal(err)
	}
	if l, _ := db.ShareLinkByHash("s1"); l != nil {
		t.Fatal("revoke left a share link")
	}
	if d, _ := db.DeviceByToken("tok1"); d == nil || !d.Revoked {
		t.Fatalf("revoked device must be returned flagged: %+v", d)
	}
	if err := db.DeleteDevice("d1"); err != nil {
		t.Fatal(err)
	}
	if err := db.RevokeDevice("d1"); !errors.Is(err, ErrDeviceNotFound) {
		t.Fatalf("revoke of a missing device = %v", err)
	}
}

func TestPairRequestLimitsAndSingleCollection(t *testing.T) {
	db, _ := openTemp(t)
	lim := PairRequestLimits{MaxPending: 1, MaxCommittedPerIP: 2, MaxCommitted: 3, RetentionSecs: 600}
	mk := func(id, ip string) PairRequest {
		return PairRequest{ID: id, Label: "p", ClientID: "c-" + id, PublicKey: "k", NonceCommit: "aa", RemoteIP: ip, Created: pwT0, Expires: pwT0 + 120}
	}
	for _, id := range []string{"r1", "r2"} {
		if err := db.CreatePairRequest(mk(id, "1.1.1.1"), pwT0, lim); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.CreatePairRequest(mk("r3", "1.1.1.1"), pwT0, lim); !errors.Is(err, ErrTooManyPairRequests) {
		t.Fatalf("per-source cap = %v", err)
	}
	if _, err := db.RevealPairRequest("r1", "nn", pwT0, lim.MaxPending); err != nil {
		t.Fatal(err)
	}
	if _, err := db.RevealPairRequest("r2", "mm", pwT0, lim.MaxPending); !errors.Is(err, ErrTooManyPairRequests) {
		t.Fatalf("pending cap at reveal = %v", err)
	}
	if err := db.DecidePairRequest("r1", true, pwT0); err != nil {
		t.Fatal(err)
	}
	spec := DeviceSpec{ID: "dev", TokenHash: HashToken("t"), Now: pwT0 + 5}
	pr, saved, ok, err := db.CompleteApprovedPairRequest("r1", spec)
	if err != nil || !ok || pr.ClientID != "c-r1" || saved.ID != "dev" {
		t.Fatalf("collect = %+v %+v %v %v", pr, saved, ok, err)
	}
	d, _ := db.DeviceByID("dev")
	if d == nil || d.Origin != OriginApprove || d.ClientID != "c-r1" || d.CanDownload {
		t.Fatalf("approved device = %+v", d)
	}
	if _, _, ok, _ := db.CompleteApprovedPairRequest("r1", DeviceSpec{ID: "dev2", TokenHash: HashToken("t2"), Now: pwT0 + 6}); ok {
		t.Fatal("an approved request was collected twice")
	}
	if err := db.DecidePairRequest("r1", false, pwT0); !errors.Is(err, ErrPairRequestGone) {
		t.Fatalf("decide twice = %v", err)
	}
}

func TestUpdateDeviceAppliesAllChangesTogetherAndDropsShareLinks(t *testing.T) {
	db, _ := openTemp(t)
	pwAddCode(t, db, "h1", PairingCode{})
	if _, _, err := db.PairWithCode("h1", pwSpec("d1", "c1", "tok1")); err != nil {
		t.Fatal(err)
	}
	shareInsert(t, db, shareLinkFor("s1", "d1", shareHr))
	jail, ro := "/srv/jail", true
	u := DeviceUpdate{Jail: &jail, ReadOnly: &ro, File: &[6]bool{true, true, false, false, false, false}, App: &[2]bool{true, false}}
	if err := db.UpdateDevice("d1", u); err != nil {
		t.Fatal(err)
	}
	d, _ := db.DeviceByToken("tok1")
	if d == nil || d.JailRoot != jail || !d.ReadOnly || !d.CanBrowse || !d.CanDownload || d.CanUpload || d.CanShare || !d.ViewApps || d.LaunchApps {
		t.Fatalf("update not applied together: %+v", d)
	}
	if l, _ := db.ShareLinkByHash("s1"); l != nil {
		t.Fatal("turning share off kept the share link")
	}
	if err := db.UpdateDevice("nope", u); !errors.Is(err, ErrDeviceNotFound) {
		t.Fatalf("missing device = %v", err)
	}
}
