package store

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"hash"
	"net/url"
	"path/filepath"
	"strings"
)

// SchemaState returns the database's PRAGMA user_version and its min_compatible (0 when none is recorded),
// for reports such as `rfe selfcheck`.
func (s *DB) SchemaState() (userVersion, minCompatible int, err error) {
	if err = s.db.QueryRow(`PRAGMA user_version`).Scan(&userVersion); err != nil {
		return 0, 0, fmt.Errorf("read schema version: %w", err)
	}
	minCompatible, err = readMinCompatible(s.db)
	return userVersion, minCompatible, err
}

// IntegrityCheck runs PRAGMA integrity_check on the open database. It returns nil only when SQLite reports
// the single row "ok"; anything else is returned as an error listing what SQLite found.
func (s *DB) IntegrityCheck() error {
	rows, err := s.db.Query(`PRAGMA integrity_check`)
	if err != nil {
		return fmt.Errorf("integrity_check: %w", err)
	}
	defer rows.Close()
	var results []string
	for rows.Next() {
		var r string
		if err := rows.Scan(&r); err != nil {
			return fmt.Errorf("integrity_check: %w", err)
		}
		results = append(results, r)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("integrity_check: %w", err)
	}
	if len(results) != 1 || results[0] != "ok" {
		return fmt.Errorf("integrity_check reported: %s", strings.Join(results, "; "))
	}
	return nil
}

// TokenHashDigest returns a SHA-256 over every stored token hash in dir's agent.db: each device's id and
// token_hash, and each share link's token_hash. It opens the file read-only and never migrates it, so it can
// be taken before and after a migration to prove the migration left the hashes alone. Share links of
// federated sessions (devices with a login_issuer) are left out because migration 12 deliberately deletes
// them. A table or column an old schema does not have yet counts as empty.
func TokenHashDigest(dir string) (string, error) {
	p := filepath.ToSlash(filepath.Join(dir, DBFile))
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	u := url.URL{Scheme: "file", Path: p, RawQuery: "mode=ro"}
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return "", fmt.Errorf("open database read-only: %w", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	h := sha256.New()
	hasLoginIssuer, err := dbHasColumn(db, "devices", "login_issuer")
	if err != nil {
		return "", err
	}
	devices := `SELECT id, token_hash FROM devices ORDER BY id`
	if err := digestRows(db, h, "devices", devices, 2); err != nil {
		return "", err
	}
	shares := `SELECT token_hash FROM share_tokens ORDER BY token_hash`
	if hasDev, err := dbHasColumn(db, "share_tokens", "device_id"); err != nil {
		return "", err
	} else if hasDev && hasLoginIssuer {
		shares = `SELECT token_hash FROM share_tokens
WHERE COALESCE(device_id,'') NOT IN (SELECT id FROM devices WHERE login_issuer<>'') ORDER BY token_hash`
	}
	if err := digestRows(db, h, "share_tokens", shares, 1); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// dbHasColumn reports whether table has column in db; a missing table has no columns.
func dbHasColumn(db *sql.DB, table, column string) (bool, error) {
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info(?) WHERE name=?`, table, column).Scan(&n); err != nil {
		return false, fmt.Errorf("inspect %s: %w", table, err)
	}
	return n > 0, nil
}

// digestRows feeds the label and every row of query (cols text columns each) into h, length-prefixed so
// different rows cannot produce the same byte stream. A table that does not exist contributes nothing.
func digestRows(db *sql.DB, h hash.Hash, label, query string, cols int) error {
	var exists int
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, label).Scan(&exists); err != nil {
		return fmt.Errorf("inspect %s: %w", label, err)
	}
	fmt.Fprintf(h, "%d:%s|", len(label), label)
	if exists == 0 {
		return nil
	}
	rows, err := db.Query(query)
	if err != nil {
		return fmt.Errorf("read %s: %w", label, err)
	}
	defer rows.Close()
	vals := make([]string, cols)
	ptrs := make([]any, cols)
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	for rows.Next() {
		if err := rows.Scan(ptrs...); err != nil {
			return fmt.Errorf("read %s: %w", label, err)
		}
		for _, v := range vals {
			fmt.Fprintf(h, "%d:%s|", len(v), v)
		}
	}
	return rows.Err()
}
