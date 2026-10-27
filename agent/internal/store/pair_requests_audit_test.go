package store

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

func testPairRequest(id, ip string, expires time.Time) PairRequest {
	return PairRequest{ID: id, Label: "phone", ClientID: "client-" + id, PublicKey: "pk", NonceCommit: "c", RemoteIP: ip, Created: time.Now().Unix(), Expires: expires.Unix()}
}

// Audit F-42: unrevealed (committed) requests are invisible to the owner, so they must not use up the
// owner-prompt cap that real phones need.
func TestCommittedRequestsDoNotFillOwnerPromptCap(t *testing.T) {
	db := openTestDB(t)
	exp := time.Now().Add(time.Minute)
	for _, id := range []string{"a", "b", "c"} {
		if err := db.CreatePairRequest(testPairRequest(id, "10.0.0.9", exp)); err != nil {
			t.Fatalf("committed request %s: %v", id, err)
		}
	}
	// The same source cannot hold more than its share of committed requests...
	if err := db.CreatePairRequest(testPairRequest("d", "10.0.0.9", exp)); !errors.Is(err, ErrTooManyPairRequests) {
		t.Fatalf("fourth committed request from one address = %v, want ErrTooManyPairRequests", err)
	}
	// ...but another phone can still ask.
	if err := db.CreatePairRequest(testPairRequest("e", "10.0.0.20", exp)); err != nil {
		t.Fatalf("a different address was locked out: %v", err)
	}
}

// Audit F-134: an approved request that has expired can no longer be claimed.
func TestExpiredApprovedRequestIsNotClaimable(t *testing.T) {
	db := openTestDB(t)
	exp := time.Now().Add(time.Minute)
	if err := db.CreatePairRequest(testPairRequest("x", "10.0.0.5", exp)); err != nil {
		t.Fatal(err)
	}
	if _, err := db.RevealPairRequest("x", "nonce"); err != nil {
		t.Fatal(err)
	}
	if err := db.DecidePairRequest("x", true); err != nil {
		t.Fatal(err)
	}
	if _, err := db.db.Exec(`UPDATE pair_requests SET expires=? WHERE id='x'`, time.Now().Add(-time.Second).Unix()); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := db.ClaimApprovedPairRequest("x"); err != nil || ok {
		t.Fatalf("claim of an expired approval = ok:%v err:%v, want not claimable", ok, err)
	}
}

// Audit F-169: a login device later re-paired by code keeps its username; deleting the account must
// still end that device's session and its share links.
func TestDeleteUserRevokesRePairedLoginDevice(t *testing.T) {
	db := openTestDB(t)
	if err := db.CreateUser("owner", "h1"); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateUser("guest", "h2"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.LoginDevice("client-1", "browser", "login-token", "pk", "guest", "h2"); err != nil {
		t.Fatalf("LoginDevice: %v", err)
	}
	if _, err := db.UpsertDevice("client-1", "browser", "code-token", "pk", false); err != nil {
		t.Fatalf("re-pair by code: %v", err)
	}
	if err := db.DeleteUser("guest"); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	d, err := db.DeviceByToken("code-token")
	if err != nil {
		t.Fatal(err)
	}
	if d != nil && !d.Revoked {
		t.Fatal("the device re-paired by code is still valid after its account was deleted")
	}
}

// Audit F-43: deleting an account that does not exist is "not found" even when only one account exists.
func TestDeleteUnknownUserWithOneAccountIsNotLastUser(t *testing.T) {
	db := openTestDB(t)
	if err := db.CreateUser("only", "h"); err != nil {
		t.Fatal(err)
	}
	if err := db.DeleteUser("ghost"); err == nil || errors.Is(err, ErrLastUser) {
		t.Fatalf("DeleteUser(ghost) = %v, want a not-found error", err)
	}
}

// The owner prompt cap also holds at reveal: many committed requests revealing together must not put more than
// MaxPendingPairRequests prompts in front of the owner.
func TestRevealRespectsTheOwnerPromptCap(t *testing.T) {
	db := openTestDB(t)
	exp := time.Now().Add(time.Minute)
	ids := []string{"r1", "r2", "r3", "r4"}
	for i, id := range ids {
		if err := db.CreatePairRequest(testPairRequest(id, fmt.Sprintf("10.0.1.%d", i+1), exp)); err != nil {
			t.Fatalf("commit %s: %v", id, err)
		}
	}
	for _, id := range ids[:MaxPendingPairRequests] {
		if _, err := db.RevealPairRequest(id, "nonce"); err != nil {
			t.Fatalf("reveal %s: %v", id, err)
		}
	}
	if _, err := db.RevealPairRequest(ids[MaxPendingPairRequests], "nonce"); !errors.Is(err, ErrTooManyPairRequests) {
		t.Fatalf("reveal past the cap = %v, want ErrTooManyPairRequests", err)
	}
}
