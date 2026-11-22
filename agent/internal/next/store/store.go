// Package store is the v3 agent database (agent-v3.db). It holds the same
// facts as the old agent.db (accounts, paired devices, share links, upload
// sessions, audit log, settings) in a stricter schema. Tokens are only ever
// stored as SHA-256 hashes. The package is SQL only: no HTTP, no filesystem
// access beyond the database file itself.
package store

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	_ "modernc.org/sqlite" // pure-Go SQLite driver, the same one the old store uses
)

// SchemaVersion is the PRAGMA user_version this code writes and understands.
const SchemaVersion = 1

// busyTimeoutMs is how long a write waits on a lock held by another process
// (the daemon and the admin CLI share one file).
const busyTimeoutMs = 5000

// Device origins: how a device came to be paired. The login and register
// origins are the admin ones (they also need an existing account).
const (
	OriginCode     = "code"
	OriginApprove  = "approve"
	OriginLogin    = "login"
	OriginRegister = "register"
)

// Upload session statuses (the same set the schema's CHECK allows).
const (
	UploadOpen      = "open"
	UploadCompleted = "completed"
	UploadFailed    = "failed"
)

// ErrNewerSchema is returned by Open for a file written by a newer agent.
var ErrNewerSchema = errors.New("agent-v3.db was written by a newer agent")

// DB wraps the SQLite connection.
type DB struct {
	db *sql.DB
}

// schemaSQL creates every table. Device grant columns have no default on
// purpose: every writer must state them, so no row silently gains access.
const schemaSQL = `
CREATE TABLE IF NOT EXISTS meta (key TEXT PRIMARY KEY, value TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS users (
    username      TEXT PRIMARY KEY,
    password_hash TEXT NOT NULL,
    created       INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS devices (
    id           TEXT PRIMARY KEY,
    label        TEXT NOT NULL,
    token_hash   TEXT NOT NULL UNIQUE,
    origin       TEXT NOT NULL CHECK (origin IN ('code','approve','login','register')),
    username     TEXT REFERENCES users(username),
    client_id    TEXT NOT NULL,
    public_key   TEXT NOT NULL,
    created      INTEGER NOT NULL,
    last_seen    INTEGER NOT NULL,
    last_address TEXT NOT NULL,
    last_version TEXT NOT NULL,
    revoked      INTEGER NOT NULL,
    jail_root    TEXT NOT NULL,
    read_only    INTEGER NOT NULL,
    can_browse   INTEGER NOT NULL,
    can_download INTEGER NOT NULL,
    can_upload   INTEGER NOT NULL,
    can_modify   INTEGER NOT NULL,
    can_delete   INTEGER NOT NULL,
    can_share    INTEGER NOT NULL,
    view_apps    INTEGER NOT NULL,
    launch_apps  INTEGER NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_devices_client_id ON devices(client_id) WHERE client_id <> '';
CREATE TABLE IF NOT EXISTS share_links (
    token_hash  TEXT PRIMARY KEY,
    path        TEXT NOT NULL,
    device_id   TEXT,
    created     INTEGER NOT NULL,
    expires     INTEGER NOT NULL,
    consumed_at INTEGER
);
CREATE TABLE IF NOT EXISTS share_events (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    token_hash   TEXT NOT NULL,
    path         TEXT NOT NULL,
    minted_at    INTEGER NOT NULL,
    expires_at   INTEGER NOT NULL,
    served_at    INTEGER,
    requester_ip TEXT
);
CREATE TABLE IF NOT EXISTS uploads (
    id           TEXT PRIMARY KEY,
    device_id    TEXT NOT NULL,
    target_path  TEXT NOT NULL,
    total_size   INTEGER NOT NULL,
    chunk_size   INTEGER NOT NULL,
    total_chunks INTEGER NOT NULL,
    sha256       TEXT NOT NULL,
    overwrite    INTEGER NOT NULL,
    status       TEXT NOT NULL CHECK (status IN ('open','completed','failed')),
    temp_name    TEXT NOT NULL,
    updated_at   INTEGER NOT NULL,
    terminal_at  INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS upload_chunks (
    upload_id TEXT NOT NULL REFERENCES uploads(id) ON DELETE CASCADE,
    chunk_no  INTEGER NOT NULL,
    PRIMARY KEY (upload_id, chunk_no)
) WITHOUT ROWID;
CREATE TABLE IF NOT EXISTS audit_log (
    id     INTEGER PRIMARY KEY AUTOINCREMENT,
    at     INTEGER NOT NULL,
    action TEXT NOT NULL,
    actor  TEXT NOT NULL,
    target TEXT NOT NULL,
    detail TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS settings (key TEXT PRIMARY KEY, value TEXT NOT NULL);
`

// dsnFor builds the connection string for a database file: WAL, a busy
// timeout and enforced foreign keys. modernc.org/sqlite only honours the
// `_pragma` DSN key. A '?', '#' or '%' in the path would cut the DSN short and
// drop the pragmas, so such a path goes through an escaped file: URI.
func dsnFor(path string) string {
	dsnPath := path
	if strings.ContainsAny(path, "?#%") {
		dsnPath = "file:" + strings.NewReplacer("%", "%25", "?", "%3f", "#", "%23").Replace(path)
	}
	return dsnPath + fmt.Sprintf("?_pragma=journal_mode(WAL)&_pragma=busy_timeout(%d)&_pragma=foreign_keys(on)", busyTimeoutMs)
}

// Open opens (or creates) the database file at path and makes sure the schema
// is there. A fresh file gets the schema and PRAGMA user_version=1 in one
// transaction; a file from a newer agent is refused.
func Open(path string) (*DB, error) {
	db, err := sql.Open("sqlite", dsnFor(path))
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	db.SetMaxOpenConns(1) // SQLite WAL prefers a single writer
	s := &DB{db: db}
	if err := s.ensureSchema(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// ensureSchema creates the tables on a file whose user_version is 0.
func (s *DB) ensureSchema() error {
	v, err := s.UserVersion()
	if err != nil {
		return fmt.Errorf("read user_version: %w", err)
	}
	switch {
	case v > SchemaVersion:
		return fmt.Errorf("%w (schema %d, this build knows %d)", ErrNewerSchema, v, SchemaVersion)
	case v == SchemaVersion:
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(schemaSQL); err != nil {
		return fmt.Errorf("create schema: %w", err)
	}
	if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", SchemaVersion)); err != nil {
		return fmt.Errorf("set user_version: %w", err)
	}
	return tx.Commit()
}

// Close closes the database.
func (s *DB) Close() error { return s.db.Close() }

// UserVersion returns the file's PRAGMA user_version.
func (s *DB) UserVersion() (int, error) {
	var v int
	err := s.db.QueryRow("PRAGMA user_version").Scan(&v)
	return v, err
}

// PrepareForCopy folds the write-ahead log into the main file and switches
// the file to the rollback journal, so that after Close the database is one
// self-contained file that can be fsynced and renamed.
func (s *DB) PrepareForCopy() error {
	if _, err := s.db.Exec("PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		return fmt.Errorf("checkpoint: %w", err)
	}
	var mode string
	if err := s.db.QueryRow("PRAGMA journal_mode=DELETE").Scan(&mode); err != nil {
		return fmt.Errorf("journal_mode: %w", err)
	}
	return nil
}

// IntegrityProblems runs SQLite's integrity and foreign-key checks and
// returns one line per problem found (empty when the file is sound).
func (s *DB) IntegrityProblems() ([]string, error) {
	var out []string
	rows, err := s.db.Query("PRAGMA integrity_check")
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			rows.Close()
			return nil, err
		}
		if line != "ok" {
			out = append(out, "integrity_check: "+line)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	fk, err := s.db.Query("PRAGMA foreign_key_check")
	if err != nil {
		return nil, err
	}
	defer fk.Close()
	for fk.Next() {
		var table, parent string
		var rowid sql.NullInt64
		var idx int
		if err := fk.Scan(&table, &rowid, &parent, &idx); err != nil {
			return nil, err
		}
		out = append(out, fmt.Sprintf("foreign_key_check: %s row %d has no matching %s", table, rowid.Int64, parent))
	}
	return out, fk.Err()
}

// HashToken is the stored form of a bearer or share token: lowercase hex
// SHA-256, the same format the old store used.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// b2i is the 0/1 SQLite keeps for a bool.
func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// nullStr maps "" to SQL NULL.
func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}
