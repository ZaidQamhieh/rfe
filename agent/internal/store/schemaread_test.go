package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadSchemaStateReportsVersionsWithoutWriting(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, DBFile)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	uv, minCompat, err := ReadSchemaState(dir)
	if err != nil {
		t.Fatal(err)
	}
	if uv != SchemaVersion() || minCompat < 0 || minCompat > SchemaVersion() {
		t.Fatalf("user_version %d min_compatible %d, this build is level %d", uv, minCompat, SchemaVersion())
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("ReadSchemaState changed the database file")
	}
	if _, _, err := ReadSchemaState(t.TempDir()); err == nil {
		t.Fatal("a missing database must be an error")
	}
}
