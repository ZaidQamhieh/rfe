package audit

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/next/store"
)

// fakeClock hands out a time that tests advance by hand.
type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time { return c.t }

// newLog opens a database in a temp dir and a Log over it with a fake clock.
func newLog(t *testing.T, retention int) (*Log, *fakeClock, *store.DB, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "agent-v3.db")
	db, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	clk := &fakeClock{t: time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)}
	return New(db, Options{Now: clk.now, Retention: retention}), clk, db, path
}

func TestRecordAndListNewestFirstWithClockTime(t *testing.T) {
	l, clk, _, _ := newLog(t, 0)
	first, err := l.Record(Event{Action: ActionPair, Actor: "Pixel", Target: "dev-1", Detail: "from 10.0.0.2"})
	if err != nil {
		t.Fatal(err)
	}
	clk.t = clk.t.Add(time.Minute)
	if _, err := l.Record(Event{Action: ActionDeviceRevoked, Actor: "admin", Target: "dev-1"}); err != nil {
		t.Fatal(err)
	}
	if !first.At.Equal(time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)) {
		t.Fatalf("first.At = %v, want the fake clock's time", first.At)
	}
	p, err := l.List(0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Entries) != 2 || p.Entries[0].Action != ActionDeviceRevoked || p.Entries[1].Action != ActionPair {
		t.Fatalf("entries = %+v, want revoke then pair", p.Entries)
	}
	if !p.Entries[0].At.Equal(clk.t) || p.Entries[1].Actor != "Pixel" || p.Entries[1].Detail != "from 10.0.0.2" || p.Next != 0 {
		t.Fatalf("entry fields or cursor wrong: %+v next=%d", p.Entries, p.Next)
	}
}

func TestEveryRecordedActionIsAccepted(t *testing.T) {
	l, _, _, _ := newLog(t, 0)
	for _, a := range []string{ActionPair, ActionRegister, ActionLogin, ActionLoginFailed, ActionDeviceRevoked,
		ActionDeviceRemoved, ActionDeviceUpdated, ActionShareCreated, ActionShareRevoked, ActionAgentRestart,
		ActionAppLaunch, ActionWake} {
		if _, err := l.Record(Event{Action: a}); err != nil {
			t.Errorf("action %q: %v", a, err)
		}
	}
}

func TestFailureEventsAreRecorded(t *testing.T) {
	l, _, _, _ := newLog(t, 0)
	if _, err := l.Record(Event{Action: ActionLoginFailed, Actor: "mallory", Detail: "from 192.0.2.9"}); err != nil {
		t.Fatal(err)
	}
	p, _ := l.List(10, 0)
	if len(p.Entries) != 1 || p.Entries[0].Action != ActionLoginFailed || p.Entries[0].Actor != "mallory" {
		t.Fatalf("entries = %+v, want the failed login", p.Entries)
	}
}

func TestFileOperationsAreRefused(t *testing.T) {
	l, _, db, _ := newLog(t, 0)
	for _, a := range []string{"file_delete", "upload", "download", "rename", "", "PAIR"} {
		if _, err := l.Record(Event{Action: a, Target: "/etc/passwd"}); !errors.Is(err, ErrUnknownAction) {
			t.Errorf("action %q: err = %v, want ErrUnknownAction", a, err)
		}
	}
	if rows, _ := db.AuditEntries(); len(rows) != 0 {
		t.Fatalf("%d rows written for refused actions", len(rows))
	}
}

func TestPagingWalksBackwardsWithoutGapsOrRepeats(t *testing.T) {
	l, clk, _, _ := newLog(t, 0)
	const total = 7
	for i := 0; i < total; i++ {
		clk.t = clk.t.Add(time.Second)
		if _, err := l.Record(Event{Action: ActionLogin, Target: string(rune('a' + i))}); err != nil {
			t.Fatal(err)
		}
	}
	var got []string
	var before int64
	pages := 0
	for {
		p, err := l.List(3, before)
		if err != nil {
			t.Fatal(err)
		}
		pages++
		for _, e := range p.Entries {
			got = append(got, e.Target)
		}
		if p.Next == 0 {
			break
		}
		before = p.Next
	}
	if want := "gfedcba"; strings.Join(got, "") != want || pages != 3 {
		t.Fatalf("walked %v in %d pages, want %q in 3", got, pages, want)
	}
}

func TestLimitOutsideRangeFallsBackToDefault(t *testing.T) {
	l, _, _, _ := newLog(t, 0)
	for i := 0; i < DefaultLimit+5; i++ {
		l.Try(Event{Action: ActionLogin})
	}
	for _, lim := range []int{0, -3, MaxLimit + 1} {
		p, err := l.List(lim, 0)
		if err != nil || len(p.Entries) != DefaultLimit || p.Next == 0 {
			t.Errorf("limit %d: %d entries next=%d err=%v, want %d and a next page", lim, len(p.Entries), p.Next, err, DefaultLimit)
		}
	}
	if p, _ := l.List(MaxLimit, 0); len(p.Entries) != DefaultLimit+5 {
		t.Errorf("limit MaxLimit returned %d entries", len(p.Entries))
	}
}

func TestEmptyListIsNotNil(t *testing.T) {
	l, _, _, _ := newLog(t, 0)
	p, err := l.List(10, 0)
	if err != nil || p.Entries == nil || len(p.Entries) != 0 || p.Next != 0 {
		t.Fatalf("page = %+v, %v; want an empty non-nil slice", p, err)
	}
}

func TestRetentionKeepsOnlyTheNewest(t *testing.T) {
	const keep = 4
	l, _, db, _ := newLog(t, keep)
	for i := 0; i < keep+6; i++ {
		if _, err := l.Record(Event{Action: ActionPair, Target: string(rune('a' + i))}); err != nil {
			t.Fatal(err)
		}
	}
	rows, _ := db.AuditEntries()
	if len(rows) != keep || rows[0].Target != "g" || rows[keep-1].Target != "j" {
		t.Fatalf("rows = %+v, want the newest %d (g..j)", rows, keep)
	}
}

func TestDefaultsApplied(t *testing.T) {
	l := New(nil, Options{})
	if l.opts.Retention != DefaultRetention || l.opts.Now == nil {
		t.Fatalf("defaults not applied: %+v", l.opts)
	}
}

func TestEntriesSurviveReopen(t *testing.T) {
	l, _, db, path := newLog(t, 0)
	l.Try(Event{Action: ActionShareCreated, Actor: "admin", Target: "/docs/a.txt"})
	db.Close()
	again, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	p, _ := New(again, Options{}).List(10, 0)
	if len(p.Entries) != 1 || p.Entries[0].Target != "/docs/a.txt" {
		t.Fatalf("after reopen: %+v", p.Entries)
	}
}

func TestImportedEntriesAreListedAndNewOnesContinueTheIds(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agent-v3.db")
	db, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	err = db.ImportAll(store.ImportData{AuditSeq: 9, Audit: []store.AuditEntry{
		{ID: 9, At: 100, Action: "login_failed", Actor: "x", Target: "", Detail: "from 1.2.3.4"}}})
	if err != nil {
		t.Fatal(err)
	}
	l := New(db, Options{Now: func() time.Time { return time.Unix(200, 0) }})
	e, err := l.Record(Event{Action: ActionLogin})
	if err != nil || e.ID != 10 {
		t.Fatalf("new id = %d, %v; want 10", e.ID, err)
	}
	p, _ := l.List(10, 0)
	if len(p.Entries) != 2 || p.Entries[1].Action != ActionLoginFailed || p.Entries[1].At.Unix() != 100 {
		t.Fatalf("entries = %+v", p.Entries)
	}
}

func TestFieldsAreCleaned(t *testing.T) {
	l, _, _, _ := newLog(t, 0)
	long := strings.Repeat("é", MaxFieldBytes) // 2 bytes each, so the cut must land on a boundary
	e, err := l.Record(Event{Action: ActionLoginFailed, Actor: "bob\nadmin\x1b[31m logged in", Target: "bad\xffbyte", Detail: long})
	if err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(e.Actor, "\n\x1b") || e.Actor != "bob admin [31m logged in" {
		t.Errorf("actor = %q, control characters not replaced", e.Actor)
	}
	if e.Target != "bad�byte" {
		t.Errorf("target = %q, invalid UTF-8 not replaced", e.Target)
	}
	if len(e.Detail) > MaxFieldBytes || len(e.Detail)%2 != 0 || e.Detail == "" {
		t.Errorf("detail is %d bytes, want <= %d and cut between characters", len(e.Detail), MaxFieldBytes)
	}
}

// brokenStore fails every call.
type brokenStore struct{}

func (brokenStore) AppendAudit(store.AuditEntry, int) (int64, error) {
	return 0, errors.New("disk full")
}
func (brokenStore) ListAudit(int, int64) ([]store.AuditEntry, error) {
	return nil, errors.New("disk gone")
}

func TestStoreFailuresAreReportedNotSwallowed(t *testing.T) {
	var seen string
	l := New(brokenStore{}, Options{OnError: func(action string, err error) { seen = action + ": " + err.Error() }})
	if _, err := l.Record(Event{Action: ActionPair}); err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("Record err = %v, want the store's error", err)
	}
	l.Try(Event{Action: ActionLogin})
	if !strings.HasPrefix(seen, ActionLogin+": ") || !strings.Contains(seen, "disk full") {
		t.Fatalf("OnError saw %q", seen)
	}
	if _, err := l.List(10, 0); err == nil || !strings.Contains(err.Error(), "disk gone") {
		t.Fatalf("List err = %v", err)
	}
	New(brokenStore{}, Options{}).Try(Event{Action: ActionLogin}) // no OnError: must not panic
}
