package store

import (
	"testing"
	"time"
)

func approvedRequest(t *testing.T, db *DB, id string) {
	t.Helper()
	if err := db.CreatePairRequest(testPairRequest(id, "10.0.0.5", time.Now().Add(time.Minute))); err != nil {
		t.Fatal(err)
	}
	if _, err := db.RevealPairRequest(id, "nonce"); err != nil {
		t.Fatal(err)
	}
	if err := db.DecidePairRequest(id, true); err != nil {
		t.Fatal(err)
	}
}

// Completing an approval is one unit: the request is spent only together with the device row.
func TestCompletePairRequestCreatesDeviceAndSpendsRequestOnce(t *testing.T) {
	db := openTestDB(t)
	approvedRequest(t, db, "ok")
	r, deviceID, ok, err := db.CompletePairRequest("ok", "tok-1", false)
	if err != nil || !ok || deviceID == "" || r.ClientID != "client-ok" {
		t.Fatalf("complete = (%+v, %q, %v, %v)", r, deviceID, ok, err)
	}
	if _, _, ok, err := db.CompletePairRequest("ok", "tok-2", false); err != nil || ok {
		t.Fatalf("second completion = ok:%v err:%v, want not claimable", ok, err)
	}
}

// A failure while writing the device must leave the approval unspent so the phone can poll again.
func TestCompletePairRequestFailureLeavesApprovalRetryable(t *testing.T) {
	db := openTestDB(t)
	approvedRequest(t, db, "fail")
	if _, err := db.db.Exec(`ALTER TABLE devices RENAME TO devices_gone`); err != nil {
		t.Fatal(err)
	}
	if _, _, ok, err := db.CompletePairRequest("fail", "tok", false); err == nil || ok {
		t.Fatalf("complete with a broken devices table = ok:%v err:%v, want an error", ok, err)
	}
	pr, found, err := db.GetPairRequest("fail")
	if err != nil || !found || pr.Status != PairApproved {
		t.Fatalf("request after failed completion = (%+v, %v, %v), want still approved", pr, found, err)
	}
	if _, err := db.db.Exec(`ALTER TABLE devices_gone RENAME TO devices`); err != nil {
		t.Fatal(err)
	}
	if _, _, ok, err := db.CompletePairRequest("fail", "tok", false); err != nil || !ok {
		t.Fatalf("retry = ok:%v err:%v, want success", ok, err)
	}
}
