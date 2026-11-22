package store

import (
	"reflect"
	"testing"
)

func openIntentDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// An intent is written before a publish copy and deleted once the final name exists; listing returns only
// the rows older than the cutoff, so a publish that is still young is never offered to the sweeper.
func TestPublishIntentPutListDelete(t *testing.T) {
	db := openIntentDB(t)
	if err := db.PutPublishIntent("s1", "/d/one", ".rfe-upload-s1", 100); err != nil {
		t.Fatalf("put s1: %v", err)
	}
	if err := db.PutPublishIntent("s2", "/d/two", ".rfe-upload-s2", 5000); err != nil {
		t.Fatalf("put s2: %v", err)
	}
	got, err := db.ListPublishIntentsBefore(1000)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	want := []PublishIntent{{SessionID: "s1", DestDir: "/d/one", TempName: ".rfe-upload-s1", CreatedAt: 100}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("intents older than 1000 = %+v, want %+v", got, want)
	}
	if err := db.DeletePublishIntent("s1"); err != nil {
		t.Fatalf("delete s1: %v", err)
	}
	if err := db.DeletePublishIntent("never-existed"); err != nil {
		t.Fatalf("deleting a missing intent must not fail: %v", err)
	}
	got, err = db.ListPublishIntentsBefore(10000)
	if err != nil || len(got) != 1 || got[0].SessionID != "s2" {
		t.Fatalf("after delete: %+v, %v; want only s2", got, err)
	}
}

// A retried complete records its intent again under the same session id: one row, with the new time and place.
func TestPublishIntentRetryReplacesTheRow(t *testing.T) {
	db := openIntentDB(t)
	if err := db.PutPublishIntent("s1", "/d/one", ".rfe-upload-s1", 100); err != nil {
		t.Fatal(err)
	}
	if err := db.PutPublishIntent("s1", "/d/other", ".rfe-upload-s1", 9000); err != nil {
		t.Fatalf("second put for the same session: %v", err)
	}
	if got, _ := db.ListPublishIntentsBefore(1000); len(got) != 0 {
		t.Fatalf("the retried row kept its old time: %+v", got)
	}
	got, err := db.ListPublishIntentsBefore(10000)
	if err != nil || len(got) != 1 || got[0].DestDir != "/d/other" || got[0].CreatedAt != 9000 {
		t.Fatalf("rows = %+v, %v; want one row for /d/other at 9000", got, err)
	}
}

// publish_intents arrives as a new, additive migration: an older agent still opens the file.
func TestMigratePublishIntentsIsAnAdditiveStep(t *testing.T) {
	db := openIntentDB(t)
	var n int
	if err := db.db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='publish_intents'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("publish_intents table present = %d, err %v", n, err)
	}
	var minCompat int
	if err := db.db.QueryRow(`SELECT value FROM schema_meta WHERE key='min_compatible'`).Scan(&minCompat); err != nil {
		t.Fatalf("read min_compatible: %v", err)
	}
	if minCompat >= SchemaVersion() {
		t.Fatalf("min_compatible %d reaches the newest step %d: publish_intents must not be a breaking migration", minCompat, SchemaVersion())
	}
	if migrations[len(migrations)-1].breaking {
		t.Fatal("the newest migration is marked breaking")
	}
}
