// Package store manages the agent's SQLite database.
// Tokens are only stored as SHA-256 hashes.
package store

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	_ "modernc.org/sqlite" // pure-Go SQLite driver
)

// DB wraps the SQLite connection and exposes a typed API.
type DB struct {
	db *sql.DB
	// dir is the data directory agent.db lives in; the account-server signing key file sits beside it.
	dir string

	// debounce last_seen writes. TouchDevice fires on every authenticated
	// request/chunk; without this, high-frequency traffic serializes behind a
	// nonessential timestamp on the single write connection.
	touchMu   sync.Mutex
	lastTouch map[string]time.Time

	// auditHook, when set, runs after each audit entry is stored.
	auditHook atomic.Pointer[func()]
	// auditActionHook, when set, runs after each audit entry is stored and is told its action name.
	auditActionHook atomic.Pointer[func(action string)]

	// federatedTTL is the federated-session lifetime in nanoseconds; zero means FederatedSessionTTL.
	federatedTTL atomic.Int64
}

// touchInterval is the minimum gap between persisted last_seen updates per
// device. Address/version changes within the window are coalesced.
const touchInterval = time.Minute

// sqliteBusyTimeoutMs is how long a write waits on a lock held by the other process (the daemon or the admin CLI).
const sqliteBusyTimeoutMs = 5000

// DBFile is the database's file name in the data directory.
const DBFile = "agent.db"

// Open opens (or creates) agent.db under dir and migrates it to this build's schema. It runs with
// synchronous=FULL so every migration commit is on disk before the next step begins. A database whose
// min_compatible is newer than this build supports fails with ErrSchemaTooNew and is left unmodified.
func Open(dir string) (*DB, error) {
	path := filepath.Join(dir, DBFile)
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
	dsn := dsnPath + fmt.Sprintf("?_pragma=journal_mode(WAL)&_pragma=busy_timeout(%d)&_pragma=foreign_keys(on)&_pragma=synchronous(FULL)", sqliteBusyTimeoutMs)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	db.SetMaxOpenConns(1) // SQLite WAL still prefers a single writer
	s := &DB{db: db, dir: dir, lastTouch: make(map[string]time.Time)}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *DB) Close() error { return s.db.Close() }
