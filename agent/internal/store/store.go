// Package store manages the agent's SQLite database.
// Tables: devices, config, transfers.
// Tokens are only stored as SHA-256 hashes.
package store

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite" // pure-Go SQLite driver
)

// DB wraps the SQLite connection and exposes a typed API.
type DB struct {
	db *sql.DB

	// PR-41: debounce last_seen writes. TouchDevice fires on every authenticated
	// request/chunk; without this, high-frequency traffic serializes behind a
	// nonessential timestamp on the single write connection.
	touchMu   sync.Mutex
	lastTouch map[string]time.Time
}

// touchInterval is the minimum gap between persisted last_seen updates per
// device. Address/version changes within the window are coalesced.
const touchInterval = time.Minute

// Open opens (or creates) agent.db under dir.
func Open(dir string) (*DB, error) {
	path := filepath.Join(dir, "agent.db")
	// busy_timeout lets the daemon and the `rfe` admin CLI write to the
	// same DB across processes without immediately erroring on a brief lock.
	// modernc.org/sqlite only recognizes `_pragma` (plus `_time_format` and
	// `_txlock`) DSN params, not the mattn-style `_journal_mode`/`_busy_timeout`
	// keys — those would be silently ignored.
	// A '?', '#' or '%' in the data directory would cut the DSN short and drop the pragmas below
	// (audit F-96), so such a path goes through an escaped file: URI; other paths keep the plain form.
	dsnPath := path
	if strings.ContainsAny(path, "?#%") {
		dsnPath = "file:" + strings.NewReplacer("%", "%25", "?", "%3f", "#", "%23").Replace(path)
	}
	dsn := dsnPath + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(on)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	db.SetMaxOpenConns(1) // SQLite WAL still prefers a single writer
	s := &DB{db: db, lastTouch: make(map[string]time.Time)}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// Close closes the database.
func (s *DB) Close() error { return s.db.Close() }
