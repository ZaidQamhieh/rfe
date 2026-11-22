package store

import (
	"errors"
	"testing"
)

// twoAccounts seeds accounts "alice" (with a login device that minted a share
// link) and "bob" (with a register device).
func twoAccounts(t *testing.T) *DB {
	t.Helper()
	db, _ := openTemp(t)
	err := db.ImportAll(ImportData{
		Users: []User{{Username: "alice", PasswordHash: "h1", Created: 1}, {Username: "bob", PasswordHash: "h2", Created: 2}},
		Devices: []Device{
			{ID: "d-alice", Label: "A", TokenHash: HashToken("ta"), Origin: OriginLogin, Username: "alice", CanBrowse: true},
			{ID: "d-bob", Label: "B", TokenHash: HashToken("tb"), Origin: OriginRegister, Username: "bob", CanBrowse: true},
			{ID: "d-code", Label: "C", TokenHash: HashToken("tc"), Origin: OriginCode, CanBrowse: true},
		},
		ShareLinks: []ShareLink{
			{TokenHash: "s-alice", Path: "/a", DeviceID: "d-alice", Created: 1, Expires: 2},
			{TokenHash: "s-code", Path: "/c", DeviceID: "d-code", Created: 1, Expires: 2},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return db
}

func TestDeleteUserRemovesAccountEndsItsSessionsAndKeepsOthers(t *testing.T) {
	db := twoAccounts(t)
	if err := db.DeleteUser("alice"); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	if u, err := db.UserByUsername("alice"); u != nil || err != nil {
		t.Fatalf("alice after delete = %+v, %v", u, err)
	}
	if u, _ := db.UserByUsername("bob"); u == nil {
		t.Fatal("bob was removed too")
	}
	if d, err := db.DeviceByToken("ta"); d != nil || err != nil {
		t.Fatalf("alice's device still authenticates: %+v, %v", d, err)
	}
	dev, err := db.DeviceByID("d-alice")
	if err != nil || dev == nil || !dev.Revoked || dev.Username != "" {
		t.Fatalf("d-alice = %+v, %v; want a revoked device with no account", dev, err)
	}
	if l, _ := db.ShareLinkByHash("s-alice"); l != nil {
		t.Fatal("alice's share link survived")
	}
	if l, _ := db.ShareLinkByHash("s-code"); l == nil {
		t.Fatal("another device's share link was removed")
	}
	if d, _ := db.DeviceByToken("tb"); d == nil || d.Revoked {
		t.Fatalf("bob's device was touched: %+v", d)
	}
}

func TestDeleteUserUnknownIsNotFoundEvenWithOneAccount(t *testing.T) {
	db := twoAccounts(t)
	if err := db.DeleteUser("alice"); err != nil {
		t.Fatal(err)
	}
	// bob is now the last account: a missing name is still "not found", the last one is refused.
	if err := db.DeleteUser("nobody"); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("DeleteUser(nobody) = %v, want ErrUserNotFound", err)
	}
	if err := db.DeleteUser("bob"); !errors.Is(err, ErrLastUser) {
		t.Fatalf("DeleteUser(last) = %v, want ErrLastUser", err)
	}
	if u, _ := db.UserByUsername("bob"); u == nil {
		t.Fatal("the last account was deleted")
	}
}
