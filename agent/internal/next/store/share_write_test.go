package store

import (
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
)

// Fixed instants (Unix seconds) the share tests run against; nothing reads the
// real clock.
const (
	shareT0 int64 = 1_000_000
	shareHr int64 = 3600
)

// shareLinkFor returns an unused link minted by dev at shareT0 that expires after ttl seconds.
func shareLinkFor(hash, dev string, ttl int64) ShareLink {
	return ShareLink{TokenHash: hash, Path: "/data/" + hash, DeviceID: dev, Created: shareT0, Expires: shareT0 + ttl}
}

// shareEventFor is the mint history row that matches l.
func shareEventFor(l ShareLink) ShareEvent {
	return ShareEvent{TokenHash: l.TokenHash, Path: l.Path, MintedAt: l.Created, ExpiresAt: l.Expires}
}

// shareInsert inserts l with its mint row and fails the test on error.
func shareInsert(t *testing.T, db *DB, l ShareLink) {
	t.Helper()
	if err := db.InsertShareLink(l, shareEventFor(l)); err != nil {
		t.Fatalf("InsertShareLink %s: %v", l.TokenHash, err)
	}
}

func TestInsertShareLinkRoundTripWritesHistoryAndRejectsDuplicate(t *testing.T) {
	db, _ := openTemp(t)
	l := shareLinkFor("h1", "dev-a", shareHr)
	shareInsert(t, db, l)
	got, err := db.ShareLinkByHash("h1")
	if err != nil || got == nil || !reflect.DeepEqual(*got, l) {
		t.Fatalf("ShareLinkByHash = %+v, %v; want %+v", got, err, l)
	}
	evs, err := db.ShareEvents()
	if err != nil || len(evs) != 1 || evs[0].TokenHash != "h1" || evs[0].Path != l.Path ||
		evs[0].MintedAt != shareT0 || evs[0].ExpiresAt != l.Expires || evs[0].ServedAt != nil || evs[0].RequesterIP != nil {
		t.Fatalf("ShareEvents = %+v, %v; want one unserved row for h1", evs, err)
	}
	// A duplicate hash fails and leaves no second history row (the transaction rolled back).
	if err := db.InsertShareLink(l, shareEventFor(l)); err == nil {
		t.Fatal("duplicate InsertShareLink: want an error")
	}
	if evs, _ := db.ShareEvents(); len(evs) != 1 {
		t.Fatalf("after a failed duplicate insert there are %d history rows; want 1", len(evs))
	}
	if got, err := db.ShareLinkByHash("missing"); got != nil || err != nil {
		t.Fatalf("ShareLinkByHash(missing) = %+v, %v; want nil, nil", got, err)
	}
}

func TestInsertShareLinkWithoutOwnerReadsBackEmptyDeviceID(t *testing.T) {
	db, _ := openTemp(t)
	shareInsert(t, db, shareLinkFor("legacy", "", shareHr))
	got, err := db.ShareLinkByHash("legacy")
	if err != nil || got == nil || got.DeviceID != "" {
		t.Fatalf("ShareLinkByHash = %+v, %v; want an owner-less link", got, err)
	}
}

func TestConsumeShareLinkIsSingleUseAndHonoursExpiry(t *testing.T) {
	db, _ := openTemp(t)
	shareInsert(t, db, shareLinkFor("ok", "d", shareHr))
	shareInsert(t, db, shareLinkFor("late", "d", shareHr))

	// The expiry second itself still counts as valid; the second after does not.
	if ok, err := db.ConsumeShareLink("late", shareT0+shareHr+1); ok || err != nil {
		t.Fatalf("consume after expiry = %v, %v; want false, nil", ok, err)
	}
	if ok, err := db.ConsumeShareLink("ok", shareT0+shareHr); !ok || err != nil {
		t.Fatalf("consume at the expiry second = %v, %v; want true, nil", ok, err)
	}
	if ok, err := db.ConsumeShareLink("ok", shareT0+shareHr); ok || err != nil {
		t.Fatalf("second consume = %v, %v; want false, nil", ok, err)
	}
	if ok, err := db.ConsumeShareLink("missing", shareT0); ok || err != nil {
		t.Fatalf("consume of an unknown hash = %v, %v; want false, nil", ok, err)
	}
	got, _ := db.ShareLinkByHash("ok")
	if got == nil || got.ConsumedAt == nil || *got.ConsumedAt != shareT0+shareHr {
		t.Fatalf("consumed link = %+v; want ConsumedAt %d", got, shareT0+shareHr)
	}
}

func TestConsumeShareLinkHasExactlyOneWinnerUnderRace(t *testing.T) {
	db, _ := openTemp(t)
	shareInsert(t, db, shareLinkFor("race", "d", shareHr))
	var wins atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if ok, err := db.ConsumeShareLink("race", shareT0); err != nil {
				t.Errorf("ConsumeShareLink: %v", err)
			} else if ok {
				wins.Add(1)
			}
		}()
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatalf("%d goroutines consumed the link; want exactly 1", wins.Load())
	}
}

func TestActiveShareLinksFiltersByOwnerExpiryAndUse(t *testing.T) {
	db, _ := openTemp(t)
	a := shareLinkFor("a", "dev-a", shareHr)
	b := shareLinkFor("b", "dev-b", shareHr)
	old := shareLinkFor("old", "dev-a", shareHr)
	old.Expires = shareT0 - 1 // already expired
	used := shareLinkFor("used", "dev-a", shareHr)
	for _, l := range []ShareLink{a, b, old, used} {
		shareInsert(t, db, l)
	}
	if ok, err := db.ConsumeShareLink("used", shareT0); !ok || err != nil {
		t.Fatalf("consume used: %v, %v", ok, err)
	}
	hashes := func(ls []ShareLink) []string {
		var out []string
		for _, l := range ls {
			out = append(out, l.TokenHash)
		}
		return out
	}
	all, err := db.ActiveShareLinks("", shareT0)
	if err != nil || !reflect.DeepEqual(hashes(all), []string{"a", "b"}) {
		t.Fatalf("all active = %v, %v; want [a b]", hashes(all), err)
	}
	mine, err := db.ActiveShareLinks("dev-a", shareT0)
	if err != nil || !reflect.DeepEqual(hashes(mine), []string{"a"}) {
		t.Fatalf("dev-a active = %v, %v; want [a]", hashes(mine), err)
	}
	if none, err := db.ActiveShareLinks("nobody", shareT0); err != nil || len(none) != 0 {
		t.Fatalf("nobody active = %v, %v; want none", hashes(none), err)
	}
}

func TestDeleteShareLinkReportsWhetherOneExisted(t *testing.T) {
	db, _ := openTemp(t)
	shareInsert(t, db, shareLinkFor("gone", "d", shareHr))
	if ok, err := db.DeleteShareLink("gone"); !ok || err != nil {
		t.Fatalf("delete existing = %v, %v; want true, nil", ok, err)
	}
	if ok, err := db.DeleteShareLink("gone"); ok || err != nil {
		t.Fatalf("delete again = %v, %v; want false, nil", ok, err)
	}
	if evs, _ := db.ShareEvents(); len(evs) != 1 {
		t.Fatalf("history rows after a revoke = %d; want the mint row kept", len(evs))
	}
}

func TestRecordShareServedStampsNewestUnservedRowOnly(t *testing.T) {
	db, _ := openTemp(t)
	l := shareLinkFor("h", "d", shareHr)
	shareInsert(t, db, l)
	if err := db.RecordShareServed("h", "192.0.2.7", shareT0+5); err != nil {
		t.Fatal(err)
	}
	// A second serve with no unserved row left changes nothing.
	if err := db.RecordShareServed("h", "192.0.2.8", shareT0+9); err != nil {
		t.Fatal(err)
	}
	evs, _ := db.ShareEvents()
	if len(evs) != 1 || evs[0].ServedAt == nil || *evs[0].ServedAt != shareT0+5 ||
		evs[0].RequesterIP == nil || *evs[0].RequesterIP != "192.0.2.7" {
		t.Fatalf("history = %+v; want one row served at %d from 192.0.2.7", evs, shareT0+5)
	}
}

func TestSweepShareLinksDeletesOnlyExpiredOnes(t *testing.T) {
	db, _ := openTemp(t)
	short := shareLinkFor("short", "d", 10)
	long := shareLinkFor("long", "d", shareHr)
	shareInsert(t, db, short)
	shareInsert(t, db, long)
	if n, err := db.SweepShareLinks(shareT0 + 10); n != 0 || err != nil {
		t.Fatalf("sweep at the expiry second = %d, %v; want 0 (not yet expired)", n, err)
	}
	if n, err := db.SweepShareLinks(shareT0 + 11); n != 1 || err != nil {
		t.Fatalf("sweep after expiry = %d, %v; want 1", n, err)
	}
	if got, _ := db.ShareLinkByHash("short"); got != nil {
		t.Fatal("expired link survived the sweep")
	}
	if got, _ := db.ShareLinkByHash("long"); got == nil {
		t.Fatal("unexpired link was swept")
	}
}

func TestShareOwnerDeviceFindsRevokedAndHidesAdminWithoutAccount(t *testing.T) {
	db, _ := openTemp(t)
	plain := Device{ID: "plain", Label: "p", TokenHash: "t1", Origin: OriginCode, Revoked: true, CanShare: true}
	admin := Device{ID: "admin", Label: "a", TokenHash: "t2", Origin: OriginLogin, Username: "alice"}
	orphan := Device{ID: "orphan", Label: "o", TokenHash: "t3", Origin: OriginLogin, Username: ""}
	err := db.ImportAll(ImportData{
		Users:   []User{{Username: "alice", PasswordHash: "x", Created: 1}},
		Devices: []Device{plain, admin, orphan},
	})
	if err != nil {
		t.Fatal(err)
	}
	if d, err := db.ShareOwnerDevice("plain"); err != nil || d == nil || !d.Revoked || !d.CanShare {
		t.Fatalf("plain = %+v, %v; want the revoked device returned with its grants", d, err)
	}
	if d, err := db.ShareOwnerDevice("admin"); err != nil || d == nil || !d.IsAdmin() {
		t.Fatalf("admin = %+v, %v; want the admin device", d, err)
	}
	if d, err := db.ShareOwnerDevice("orphan"); err != nil || d != nil {
		t.Fatalf("orphan admin = %+v, %v; want nil, nil (no account)", d, err)
	}
	if d, err := db.ShareOwnerDevice("missing"); err != nil || d != nil {
		t.Fatalf("missing = %+v, %v; want nil, nil", d, err)
	}
}
