package store

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// openTemp opens a fresh database in a temp dir.
func openTemp(t *testing.T) (*DB, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "agent-v3.db")
	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db, path
}

func TestOpenCreatesSchemaAtVersionOne(t *testing.T) {
	db, path := openTemp(t)
	v, err := db.UserVersion()
	if err != nil || v != 1 {
		t.Fatalf("user_version = %d, %v; want 1", v, err)
	}
	counts, err := db.Counts()
	if err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"users", "devices", "share_links", "share_events", "uploads", "upload_chunks", "audit_log", "settings"} {
		if n, ok := counts[table]; !ok || n != 0 {
			t.Errorf("table %s: count %d present=%v; want an empty table", table, n, ok)
		}
	}
	// Reopening an existing v1 file must succeed and keep the version.
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	again, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer again.Close()
	if v, _ := again.UserVersion(); v != 1 {
		t.Fatalf("user_version after reopen = %d", v)
	}
}

func TestOpenRefusesNewerSchema(t *testing.T) {
	db, path := openTemp(t)
	if _, err := db.db.Exec("PRAGMA user_version = 2"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if _, err := Open(path); !errors.Is(err, ErrNewerSchema) {
		t.Fatalf("Open of a version 2 file: err = %v, want ErrNewerSchema", err)
	}
}

func TestOpenKeepsPragmasForOddPath(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "a?b#c%d")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	db, err := Open(filepath.Join(dir, "agent-v3.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()
	var fk int
	if err := db.db.QueryRow("PRAGMA foreign_keys").Scan(&fk); err != nil || fk != 1 {
		t.Fatalf("foreign_keys = %d, %v; the DSN pragmas were lost", fk, err)
	}
}

// sample is a small data set covering every table.
func sample() ImportData {
	served := int64(50)
	ip := "192.0.2.1"
	return ImportData{
		Users: []User{{Username: "alice", PasswordHash: "h-alice", Created: 1}},
		Devices: []Device{
			{ID: "d-code", Label: "Phone", TokenHash: HashToken("t-code"), Origin: OriginCode, ClientID: "c1", CanBrowse: true},
			{ID: "d-admin", Label: "Laptop", TokenHash: HashToken("t-admin"), Origin: OriginLogin, Username: "alice", CanBrowse: true, CanShare: true},
			{ID: "d-orphan", Label: "Old", TokenHash: HashToken("t-orphan"), Origin: OriginLogin, CanBrowse: true},
			{ID: "d-revoked", Label: "Lost", TokenHash: HashToken("t-revoked"), Origin: OriginCode, Revoked: true},
		},
		ShareLinks:  []ShareLink{{TokenHash: HashToken("s1"), Path: "/a", DeviceID: "d-code", Created: 2, Expires: 3}},
		ShareEvents: []ShareEvent{{ID: 7, TokenHash: HashToken("s1"), Path: "/a", MintedAt: 2, ExpiresAt: 3, ServedAt: &served, RequesterIP: &ip}},
		Uploads: []Upload{{ID: "u1", DeviceID: "d-code", TargetPath: "/a", TotalSize: 16, ChunkSize: 8, TotalChunks: 2, SHA256: "x",
			Status: UploadOpen, TempName: "u1.tmp", UpdatedAt: 5, Chunks: []int{0, 1}}},
		Audit:         []AuditEntry{{ID: 40, At: 9, Action: "pair", Actor: "d-code"}},
		Settings:      map[string]string{"agentName": "pc"},
		Meta:          map[string]string{"schema_version": "1"},
		AuditSeq:      44,
		ShareEventSeq: 7,
	}
}

func TestImportAllRoundTripAndLookups(t *testing.T) {
	db, _ := openTemp(t)
	want := sample()
	if err := db.ImportAll(want); err != nil {
		t.Fatalf("ImportAll: %v", err)
	}
	got, err := db.Devices()
	if err != nil {
		t.Fatal(err)
	}
	// Devices come back ordered by id, which is not the order of sample().
	byID := map[string]Device{}
	for _, d := range got {
		byID[d.ID] = d
	}
	for _, d := range want.Devices {
		if !reflect.DeepEqual(byID[d.ID], d) {
			t.Errorf("device %s: got %+v, want %+v", d.ID, byID[d.ID], d)
		}
	}
	ups, err := db.Uploads()
	if err != nil || !reflect.DeepEqual(ups, want.Uploads) {
		t.Errorf("uploads: got %+v (%v), want %+v", ups, err, want.Uploads)
	}
	ev, err := db.ShareEvents()
	if err != nil || !reflect.DeepEqual(ev, want.ShareEvents) {
		t.Errorf("share events: got %+v (%v)", ev, err)
	}

	cases := []struct {
		token string
		found bool
	}{
		{"t-code", true}, {"t-admin", true},
		{"t-orphan", false}, // admin origin without an account does not authenticate
		{"t-revoked", true}, // revoked devices are found; the caller refuses them
		{"nope", false},
	}
	for _, c := range cases {
		d, err := db.DeviceByToken(c.token)
		if err != nil || (d != nil) != c.found {
			t.Errorf("DeviceByToken(%q) = %v, %v; found want %v", c.token, d, err, c.found)
		}
	}
	if d, _ := db.DeviceByToken("t-revoked"); d == nil || !d.Revoked {
		t.Error("revoked device must come back with Revoked set")
	}
}

func TestImportAllRaisesSequencesAndRollsBack(t *testing.T) {
	db, _ := openTemp(t)
	if err := db.ImportAll(sample()); err != nil {
		t.Fatal(err)
	}
	var seq int64
	if err := db.db.QueryRow(`SELECT seq FROM sqlite_sequence WHERE name='audit_log'`).Scan(&seq); err != nil || seq != 44 {
		t.Fatalf("audit_log seq = %d, %v; want 44 (the old counter, above the highest id 40)", seq, err)
	}

	// A bad row anywhere must leave the database as it was.
	empty, _ := openTemp(t)
	bad := sample()
	bad.Uploads[0].Status = "paused" // violates the status CHECK
	if err := empty.ImportAll(bad); err == nil {
		t.Fatal("ImportAll accepted an upload with an unknown status")
	}
	counts, err := empty.Counts()
	if err != nil {
		t.Fatal(err)
	}
	for table, n := range counts {
		if n != 0 {
			t.Errorf("after a failed ImportAll %s holds %d rows; want 0", table, n)
		}
	}
}

func TestSchemaRejectsDeviceWithMissingAccount(t *testing.T) {
	db, _ := openTemp(t)
	d := sample()
	d.Devices[2].Username = "ghost" // no such account
	if err := db.ImportAll(d); err == nil {
		t.Fatal("expected the foreign key on devices.username to reject an unknown account")
	}
}

func TestIntegrityProblemsEmptyForSoundFile(t *testing.T) {
	db, _ := openTemp(t)
	if err := db.ImportAll(sample()); err != nil {
		t.Fatal(err)
	}
	if p, err := db.IntegrityProblems(); err != nil || len(p) != 0 {
		t.Fatalf("IntegrityProblems = %v, %v", p, err)
	}
}
