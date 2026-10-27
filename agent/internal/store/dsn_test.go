package store

import (
	"os"
	"path/filepath"
	"testing"
)

// F-96: a data directory holding '?', '#' or '%' must still open with its pragmas applied.
func TestOpen_DataDirWithQueryCharacters(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "a?b#c%d")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	db, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := os.Stat(filepath.Join(dir, "agent.db")); err != nil {
		t.Fatalf("database not created inside the data dir: %v", err)
	}
	var fk, bt int
	if err := db.db.QueryRow("PRAGMA foreign_keys").Scan(&fk); err != nil || fk != 1 {
		t.Fatalf("foreign_keys = %d (%v): pragmas were dropped", fk, err)
	}
	if err := db.db.QueryRow("PRAGMA busy_timeout").Scan(&bt); err != nil || bt != 5000 {
		t.Fatalf("busy_timeout = %d (%v)", bt, err)
	}
}
