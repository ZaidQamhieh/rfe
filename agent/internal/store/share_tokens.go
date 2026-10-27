package store

import (
	"database/sql"
	"fmt"
	"time"
)

// --------- share tokens (R1) ---------

// ShareToken is an active (unconsumed, unexpired) one-time share link.
type ShareToken struct {
	TokenHash string
	Path      string
	Expires   time.Time
	DeviceID  string // device that minted it; "" for tokens predating the column
}

// CreateShareToken stores a new one-time share token. tokenHash is the
// SHA-256 hash of the raw token — only the hash is ever persisted. deviceID
// is the minting device, used to scope list/revoke to the owner (PR-03).
func (s *DB) CreateShareToken(tokenHash, path, deviceID string, expiresAt time.Time) error {
	_, err := s.db.Exec(
		`INSERT INTO share_tokens (token_hash, path, created, expires, device_id) VALUES (?,?,?,?,?)`,
		tokenHash, path, time.Now().Unix(), expiresAt.Unix(), deviceID,
	)
	return err
}

// ConsumeShareToken atomically looks up tokenHash, deletes it (single-use),
// and returns the path it was bound to. ok is false if the token doesn't
// exist or has expired — callers must not distinguish the two in their
// response (don't leak which).
func (s *DB) ConsumeShareToken(tokenHash string) (path string, ok bool, err error) {
	path, _, ok, err = s.ConsumeShareTokenFor(tokenHash)
	return path, ok, err
}

// ConsumeShareTokenFor is ConsumeShareToken that also returns the minting
// device id ("" for tokens that predate the column), so the caller can
// re-check that device's current rights before serving.
func (s *DB) ConsumeShareTokenFor(tokenHash string) (path, deviceID string, ok bool, err error) {
	tx, err := s.db.Begin()
	if err != nil {
		return "", "", false, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op once committed

	var expires int64
	err = tx.QueryRow(
		`SELECT path, expires, device_id FROM share_tokens WHERE token_hash=?`, tokenHash,
	).Scan(&path, &expires, &deviceID)
	if err == sql.ErrNoRows {
		return "", "", false, nil
	}
	if err != nil {
		return "", "", false, err
	}

	if _, err := tx.Exec(`DELETE FROM share_tokens WHERE token_hash=?`, tokenHash); err != nil {
		return "", "", false, err
	}
	if err := tx.Commit(); err != nil {
		return "", "", false, err
	}

	if time.Now().Unix() > expires {
		return "", "", false, nil
	}
	return path, deviceID, true, nil
}

// GetShareToken returns the token with this hash, or (nil, nil) if there is
// none. Used to check ownership before revoking (PR-03).
func (s *DB) GetShareToken(tokenHash string) (*ShareToken, error) {
	var t ShareToken
	var expires int64
	err := s.db.QueryRow(
		`SELECT token_hash, path, expires, device_id FROM share_tokens WHERE token_hash=?`,
		tokenHash,
	).Scan(&t.TokenHash, &t.Path, &expires, &t.DeviceID)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	t.Expires = time.Unix(expires, 0)
	return &t, nil
}

// DeleteShareToken removes a share token (explicit revoke), regardless of
// whether it has expired.
func (s *DB) DeleteShareToken(tokenHash string) error {
	_, err := s.db.Exec(`DELETE FROM share_tokens WHERE token_hash=?`, tokenHash)
	return err
}

// ListShareTokens returns active (unexpired) share tokens. deviceID scopes
// the result to one minting device; "" returns every token (admin view).
func (s *DB) ListShareTokens(deviceID string) ([]ShareToken, error) {
	query := `SELECT token_hash, path, expires, device_id FROM share_tokens WHERE expires >= ?`
	args := []any{time.Now().Unix()}
	if deviceID != "" {
		query += ` AND device_id = ?`
		args = append(args, deviceID)
	}
	query += ` ORDER BY created`
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ShareToken
	for rows.Next() {
		var t ShareToken
		var expires int64
		if err := rows.Scan(&t.TokenHash, &t.Path, &expires, &t.DeviceID); err != nil {
			return nil, err
		}
		t.Expires = time.Unix(expires, 0)
		out = append(out, t)
	}
	return out, rows.Err()
}

// LogShareMint records a share token's minting in the audit log.
func (s *DB) LogShareMint(tokenHash, path string, expiresAt time.Time) error {
	_, err := s.db.Exec(
		`INSERT INTO share_log (token_hash, path, minted_at, expires_at) VALUES (?,?,?,?)`,
		tokenHash, path, time.Now().Unix(), expiresAt.Unix(),
	)
	return err
}

// LogShareServed records that a share token was successfully served, by
// stamping served_at + requesterIP on its most recent (served_at IS NULL)
// mint row.
func (s *DB) LogShareServed(tokenHash, requesterIP string) error {
	_, err := s.db.Exec(
		`UPDATE share_log SET served_at=?, requester_ip=?
         WHERE id = (SELECT id FROM share_log WHERE token_hash=? AND served_at IS NULL ORDER BY id DESC LIMIT 1)`,
		time.Now().Unix(), requesterIP, tokenHash,
	)
	return err
}

// SweepExpiredShareTokens deletes every share token whose expiry has passed
// and returns the number of rows removed. Called periodically by a
// background goroutine (see cmd/agent/main.go).
func (s *DB) SweepExpiredShareTokens() (int, error) {
	res, err := s.db.Exec(`DELETE FROM share_tokens WHERE expires < ?`, time.Now().Unix())
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	return int(n), err
}
