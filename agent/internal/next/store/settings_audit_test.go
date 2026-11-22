package store

import (
	"reflect"
	"testing"
)

func TestSeedSettingsKeepsExistingRows(t *testing.T) {
	db, _ := openTemp(t)
	if err := db.SetSettings(map[string]string{"roots": "", "agentName": "mine"}); err != nil {
		t.Fatal(err)
	}
	if err := db.SeedSettings(map[string]string{"roots": "/x", "agentName": "other", "readOnly": "false"}); err != nil {
		t.Fatal(err)
	}
	got, err := db.Settings()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"roots": "", "agentName": "mine", "readOnly": "false"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("settings = %v, want %v", got, want)
	}
}

func TestSetSettingsReplacesAndSurvivesReopen(t *testing.T) {
	db, path := openTemp(t)
	if err := db.SetSettings(map[string]string{"a": "1", "b": "2"}); err != nil {
		t.Fatal(err)
	}
	if err := db.SetSettings(map[string]string{"a": "3"}); err != nil {
		t.Fatal(err)
	}
	if err := db.SetSettings(nil); err != nil {
		t.Fatalf("empty write: %v", err)
	}
	db.Close()
	again, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	got, _ := again.Settings()
	if want := map[string]string{"a": "3", "b": "2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("settings = %v, want %v", got, want)
	}
}

func TestAppendAuditTrimsToRetainAndPages(t *testing.T) {
	db, _ := openTemp(t)
	const retain = 3
	var last int64
	for i := 1; i <= 5; i++ {
		id, err := db.AppendAudit(AuditEntry{At: int64(i), Action: "pair", Actor: "a", Target: "t", Detail: "d"}, retain)
		if err != nil {
			t.Fatal(err)
		}
		if id <= last {
			t.Fatalf("id %d not above previous %d", id, last)
		}
		last = id
	}
	all, err := db.ListAudit(10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != retain || all[0].ID != last || all[0].At != 5 || all[retain-1].At != 3 {
		t.Fatalf("after trim got %+v, want the newest %d, newest first", all, retain)
	}
	page, _ := db.ListAudit(1, all[0].ID)
	if len(page) != 1 || page[0].ID != all[1].ID {
		t.Fatalf("page before newest = %+v, want id %d", page, all[1].ID)
	}
	if none, _ := db.ListAudit(0, 0); len(none) != 0 {
		t.Fatalf("limit 0 returned %d entries", len(none))
	}
	// retain <= 0 keeps everything.
	if _, err := db.AppendAudit(AuditEntry{At: 6, Action: "pair"}, 0); err != nil {
		t.Fatal(err)
	}
	if rows, _ := db.ListAudit(10, 0); len(rows) != retain+1 {
		t.Fatalf("retain 0 trimmed: %d rows", len(rows))
	}
}

func TestAppendAuditContinuesImportedSequence(t *testing.T) {
	db, _ := openTemp(t)
	if err := db.ImportAll(ImportData{Audit: []AuditEntry{{ID: 40, At: 1, Action: "pair"}}, AuditSeq: 40}); err != nil {
		t.Fatal(err)
	}
	id, err := db.AppendAudit(AuditEntry{At: 2, Action: "login"}, 100)
	if err != nil || id != 41 {
		t.Fatalf("id = %d, %v; want 41", id, err)
	}
}
