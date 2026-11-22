package store

import (
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// ReadSchemaState reports the user_version and min_compatible of dir's agent.db without migrating or writing it:
// the file is opened read-only. The launcher's rollback and "rfe downgrade" use it to learn what the database now
// needs from an older agent. A missing database is an error.
func ReadSchemaState(dir string) (userVersion, minCompatible int, err error) {
	path := filepath.Join(dir, DBFile)
	if _, err := os.Stat(path); err != nil {
		return 0, 0, fmt.Errorf("agent database: %w", err)
	}
	p := filepath.ToSlash(path)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	u := url.URL{Scheme: "file", Path: p, RawQuery: "mode=ro"}
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return 0, 0, fmt.Errorf("open db read-only: %w", err)
	}
	defer db.Close()
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&userVersion); err != nil {
		return 0, 0, fmt.Errorf("read schema version: %w", err)
	}
	minCompatible, err = readMinCompatible(db)
	return userVersion, minCompatible, err
}
