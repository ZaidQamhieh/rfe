package store

import (
	"fmt"
	"testing"
)

func openTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestAppendAndListAudit(t *testing.T) {
	db := openTestDB(t)

	for _, a := range []struct{ action, actor, target string }{
		{AuditPair, "phone", "dev1"},
		{AuditShareCreated, "phone", "/srv/file.txt"},
		{AuditDeviceRevoked, "owner", "dev1"},
	} {
		if err := db.AppendAudit(a.action, a.actor, a.target, ""); err != nil {
			t.Fatalf("append %s: %v", a.action, err)
		}
	}

	entries, err := db.AuditEntries(0, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("want 3 entries, got %d", len(entries))
	}
	// Newest first.
	if entries[0].Action != AuditDeviceRevoked || entries[2].Action != AuditPair {
		t.Fatalf("wrong order: %s ... %s", entries[0].Action, entries[2].Action)
	}
	if entries[0].Actor != "owner" || entries[0].Target != "dev1" {
		t.Fatalf("actor/target not persisted: %+v", entries[0])
	}
	if entries[0].At.IsZero() {
		t.Fatal("timestamp not persisted")
	}
}

func TestAuditEntriesPaging(t *testing.T) {
	db := openTestDB(t)
	for i := 0; i < 5; i++ {
		if err := db.AppendAudit(AuditLogin, "owner", fmt.Sprintf("dev%d", i), ""); err != nil {
			t.Fatalf("append: %v", err)
		}
	}

	first, err := db.AuditEntries(2, 0)
	if err != nil {
		t.Fatalf("page 1: %v", err)
	}
	if len(first) != 2 || first[0].Target != "dev4" {
		t.Fatalf("page 1 wrong: %+v", first)
	}
	next, err := db.AuditEntries(2, first[len(first)-1].ID)
	if err != nil {
		t.Fatalf("page 2: %v", err)
	}
	if len(next) != 2 || next[0].Target != "dev2" {
		t.Fatalf("page 2 wrong: %+v", next)
	}
}

// The trim runs on every insert, so a log that outlives its retention window
// must stay bounded rather than growing forever.
func TestAppendAuditTrimsToRetention(t *testing.T) {
	db := openTestDB(t)
	for i := 0; i < auditRetention+10; i++ {
		if err := db.AppendAudit(AuditLogin, "owner", "", ""); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}
	var n int
	if err := db.db.QueryRow(`SELECT COUNT(*) FROM audit_log`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n > auditRetention {
		t.Fatalf("log grew past retention: %d rows", n)
	}
}
