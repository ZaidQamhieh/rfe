package store

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// auditDetails returns the detail text of every audit row with the given action.
func auditDetails(t *testing.T, db *DB, action string) []string {
	t.Helper()
	entries, err := db.AuditEntries(200, 0)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		if e.Action == action {
			out = append(out, e.Detail)
		}
	}
	return out
}

func TestFederatedGrantProfile_BrowseDownloadUploadOnly(t *testing.T) {
	browse, download, upload, modify, del, share := FederatedGrantProfile()
	if !browse || !download || !upload || modify || del || share {
		t.Fatalf("profile = %v %v %v %v %v %v, want browse/download/upload only", browse, download, upload, modify, del, share)
	}
}

func TestFederatedSessionTTL_IsTenMinutes(t *testing.T) {
	if FederatedSessionTTL != 10*time.Minute {
		t.Fatalf("FederatedSessionTTL = %v, want 10m", FederatedSessionTTL)
	}
}

func TestLoginDeviceFederated_GetsProfileNotOwner(t *testing.T) {
	db, fp := federatedFixture(t)
	if _, err := db.LoginDeviceFederated("c1", "phone", "tok", "pk", "alice", fp); err != nil {
		t.Fatal(err)
	}
	dev, err := db.DeviceByToken("tok")
	if err != nil || dev == nil {
		t.Fatalf("DeviceByToken = %+v, %v", dev, err)
	}
	if dev.ViaLogin {
		t.Fatal("a federated device must not be a via_login (owner) device")
	}
	if !dev.CanBrowse || !dev.CanDownload || !dev.CanUpload || dev.CanModify || dev.CanDelete || dev.CanShare {
		t.Fatalf("grants = %+v", dev)
	}
	// Logging in again after the owner widened the grants puts the profile back.
	if err := db.SetDeviceFilePermissions(dev.ID, true, true, true, true, true, true); err != nil {
		t.Fatal(err)
	}
	if _, err := db.LoginDeviceFederated("c1", "phone", "tok2", "pk", "alice", fp); err != nil {
		t.Fatal(err)
	}
	dev, _ = db.DeviceByToken("tok2")
	if dev == nil || dev.CanModify || dev.CanDelete || dev.CanShare || dev.ViaLogin {
		t.Fatalf("grants after re-login = %+v", dev)
	}
}

func TestFederatedSession_ExpiresAfterTenMinutes(t *testing.T) {
	db, fp := federatedFixture(t)
	id, err := db.LoginDeviceFederated("c1", "phone", "tok", "pk", "alice", fp)
	if err != nil {
		t.Fatal(err)
	}
	setFederatedLoginAge(t, db, id, 9*time.Minute)
	if dev, _ := db.DeviceByToken("tok"); dev == nil {
		t.Fatal("session refused after 9 minutes")
	}
	setFederatedLoginAge(t, db, id, 11*time.Minute)
	if dev, _ := db.DeviceByToken("tok"); dev != nil {
		t.Fatal("session still valid after 11 minutes")
	}
}

func TestAccountEpoch_StartsAtZeroAndBumpsWithAudit(t *testing.T) {
	db, _ := openTestStore(t)
	if n, err := db.AccountEpoch(); err != nil || n != 0 {
		t.Fatalf("initial epoch = %d, %v", n, err)
	}
	n, err := db.BumpAccountEpoch("test reason")
	if err != nil || n != 1 {
		t.Fatalf("bump = %d, %v", n, err)
	}
	if n, _ = db.BumpAccountEpoch("again"); n != 2 {
		t.Fatalf("second bump = %d", n)
	}
	details := auditDetails(t, db, AuditAccountServer)
	if len(details) != 2 || !strings.Contains(details[1], "account epoch raised to 1: test reason") {
		t.Fatalf("audit = %q", details)
	}
}

func TestAccountEpoch_RaisedWhenAccessIsRevoked(t *testing.T) {
	db, _ := openTestStore(t)
	for _, u := range []string{"alice", "bob"} {
		if err := db.CreateUser(u, "hash"); err != nil {
			t.Fatal(err)
		}
	}
	epoch := func() int64 { n, _ := db.AccountEpoch(); return n }
	if err := db.DeleteUser("bob"); err != nil || epoch() != 1 {
		t.Fatalf("DeleteUser: err %v epoch %d, want 1", err, epoch())
	}
	// A failed delete (no such user) must not raise it.
	if err := db.DeleteUser("ghost"); err == nil || epoch() != 1 {
		t.Fatalf("DeleteUser(ghost): err %v epoch %d", err, epoch())
	}
	// A code-paired device has no account: revoking it leaves the epoch alone.
	plain, err := db.UpsertDevice("p", "plain", "tok-p", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.RevokeDevice(plain); err != nil || epoch() != 1 {
		t.Fatalf("revoke plain: err %v epoch %d", err, epoch())
	}
	// A device logged in with an account raises it on revoke and on delete.
	acct, err := db.LoginDevice("c-a", "laptop", "tok-a", "pk", "alice", "hash")
	if err != nil {
		t.Fatalf("LoginDevice: %v", err)
	}
	if err := db.RevokeDevice(acct); err != nil || epoch() != 2 {
		t.Fatalf("revoke account device: err %v epoch %d, want 2", err, epoch())
	}
	// Removing a device that is already revoked adds nothing; removing a live one does.
	if err := db.DeleteDevice(acct); err != nil || epoch() != 2 {
		t.Fatalf("delete revoked account device: err %v epoch %d, want 2", err, epoch())
	}
	live, err := db.LoginDevice("c-b", "tablet", "tok-b", "pk", "alice", "hash")
	if err != nil {
		t.Fatalf("LoginDevice: %v", err)
	}
	if err := db.DeleteDevice(live); err != nil || epoch() != 3 {
		t.Fatalf("delete live account device: err %v epoch %d, want 3", err, epoch())
	}
}

func TestAccountEpochFor_RefusesMissingAccount(t *testing.T) {
	db, _ := openTestStore(t)
	if err := db.CreateUser("alice", "hash"); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := db.AccountEpochFor("ghost"); err != nil || ok {
		t.Fatalf("ghost: ok=%v err=%v", ok, err)
	}
	if n, ok, err := db.AccountEpochFor("alice"); err != nil || !ok || n != 0 {
		t.Fatalf("alice: %d %v %v", n, ok, err)
	}
}

func TestLoginDeviceFederatedAtEpoch_HigherEpochRevokesOlderSessions(t *testing.T) {
	db, fp := federatedFixture(t)
	old, err := db.LoginDeviceFederatedAtEpoch("c-old", "old", "tok-old", "pk1", "alice", fp, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.CreateShareToken("share-hash", "/x", old, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.LoginDeviceFederatedAtEpoch("c-same", "same", "tok-same", "pk2", "bob", fp, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := db.LoginDeviceFederatedAtEpoch("c-new", "new", "tok-new", "pk3", "alice", fp, 3); err != nil {
		t.Fatalf("higher epoch login: %v", err)
	}
	if d, _ := db.GetDeviceByID(old); d != nil {
		t.Fatalf("older session survived a higher epoch: %+v", d)
	}
	if d, _ := db.DeviceByToken("tok-same"); d != nil {
		t.Fatal("older session of another user survived a higher epoch")
	}
	var n int
	if err := db.db.QueryRow(`SELECT COUNT(*) FROM share_tokens WHERE device_id=?`, old).Scan(&n); err != nil || n != 0 {
		t.Fatalf("share links of the revoked session: %d, %v", n, err)
	}
	if d, _ := db.DeviceByToken("tok-new"); d == nil {
		t.Fatal("the new session was not created")
	}
	trusted, _ := db.TrustedAccountServer()
	if trusted == nil || trusted.HighestEpoch != 3 {
		t.Fatalf("highest epoch = %+v", trusted)
	}
}

func TestLoginDeviceFederatedAtEpoch_LowerEpochRejectedEqualKeepsSessions(t *testing.T) {
	db, fp := federatedFixture(t)
	if _, err := db.LoginDeviceFederatedAtEpoch("c1", "a", "tok-a", "pk1", "alice", fp, 5); err != nil {
		t.Fatal(err)
	}
	if _, err := db.LoginDeviceFederatedAtEpoch("c2", "b", "tok-b", "pk2", "alice", fp, 4); !errors.Is(err, ErrStaleAccountEpoch) {
		t.Fatalf("lower epoch err = %v, want ErrStaleAccountEpoch", err)
	}
	if d, _ := db.DeviceByToken("tok-b"); d != nil {
		t.Fatal("a stale assertion minted a device")
	}
	if _, err := db.LoginDeviceFederatedAtEpoch("c3", "c", "tok-c", "pk3", "alice", fp, 5); err != nil {
		t.Fatalf("equal epoch: %v", err)
	}
	if d, _ := db.DeviceByToken("tok-a"); d == nil {
		t.Fatal("an equal-epoch login revoked a session of the same epoch")
	}
}

func TestRepinningAnotherKeyResetsHighestEpoch(t *testing.T) {
	db, fp := federatedFixture(t)
	if _, err := db.LoginDeviceFederatedAtEpoch("c1", "a", "tok-a", "pk1", "alice", fp, 7); err != nil {
		t.Fatal(err)
	}
	key2, _ := newTrustKey(t)
	set, err := db.SetTrustedAccountServer("other", key2)
	if err != nil || set.HighestEpoch != 0 {
		t.Fatalf("after re-pin: %+v, %v", set, err)
	}
}
