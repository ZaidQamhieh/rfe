package store

import (
	"database/sql"
	"errors"
	"time"
)

// --------- users ---------
//
// One account per agent (per PC) — logging in with it grants access to
// everything this agent's device tokens already grant (same authorization
// model as a paired device; login is just an additional way to obtain a
// device token, alongside the existing one-time pairing code). password_hash
// is produced by internal/security's bcrypt wrapper, never stored raw.

// User is an account that can log in from any client (phone or browser) to
// obtain a device token, instead of a one-time pairing code.
type User struct {
	Username     string
	PasswordHash string
	Created      time.Time
}

// CreateUser inserts a new account. Returns an error if the username already
// exists (PRIMARY KEY conflict) — callers should surface that as "already
// set up", not overwrite silently.
func (s *DB) CreateUser(username, passwordHash string) error {
	_, err := s.db.Exec(
		`INSERT INTO users (username,password_hash,created) VALUES (?,?,?)`,
		username, passwordHash, time.Now().Unix(),
	)
	return err
}

// HasAnyUser reports whether at least one account exists on this agent —
// used to enforce the documented one-account-per-agent model at the
// /v1/register gate (CLI `adduser` intentionally bypasses this, for
// headless/scripted setups).
func (s *DB) HasAnyUser() (bool, error) {
	var exists int
	err := s.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM users)`).Scan(&exists)
	return exists != 0, err
}

// GetUserByUsername returns the user, or (nil,nil) if none exists.
func (s *DB) GetUserByUsername(username string) (*User, error) {
	var u User
	var created int64
	err := s.db.QueryRow(
		`SELECT username,password_hash,created FROM users WHERE username=?`, username,
	).Scan(&u.Username, &u.PasswordHash, &created)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	u.Created = time.Unix(created, 0)
	return &u, nil
}

// ListUsers returns all login accounts (never the password hash), newest
// first. RFE has no per-user roles or session tracking, so only the real
// fields — username and created — are returned.
func (s *DB) ListUsers() ([]User, error) {
	rows, err := s.db.Query(`SELECT username,created FROM users ORDER BY created DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []User
	for rows.Next() {
		var u User
		var created int64
		if err := rows.Scan(&u.Username, &created); err != nil {
			return nil, err
		}
		u.Created = time.Unix(created, 0)
		out = append(out, u)
	}
	return out, rows.Err()
}

// ErrLastUser is returned by DeleteUser when username is the only remaining
// login account — deleting it would brick password login entirely (there'd
// be no way to obtain a device token except an existing pairing code).
var ErrLastUser = errors.New("cannot delete the last remaining user")

// ErrLoginAccountChanged is returned when an account changes after the login
// handler verifies its password but before the device token is committed.
var ErrLoginAccountChanged = errors.New("login account changed")

// DeleteUser permanently removes a login account. Returns ErrLastUser if
// username is the only account, or sql.ErrNoRows if no such account exists.
func (s *DB) DeleteUser(username string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	// Existence first: asking to delete an account that is not there is a 404
	// whatever the number of accounts, not a "last user" refusal.
	var exists int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM users WHERE username=?`, username).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		return sql.ErrNoRows
	}
	var count int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&count); err != nil {
		return err
	}
	if count <= 1 {
		return ErrLastUser
	}
	if _, err := tx.Exec(`DELETE FROM users WHERE username=?`, username); err != nil {
		return err
	}
	// End every device session that belongs to the account, in the same
	// transaction, with the share links those devices minted. A device that
	// was later re-paired by code keeps its username, so match on the
	// username alone rather than on via_login.
	if _, err := tx.Exec(`DELETE FROM share_tokens WHERE device_id IN (SELECT id FROM devices WHERE username=?)`, username); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE devices SET revoked=1 WHERE username=?`, username); err != nil {
		return err
	}
	return tx.Commit()
}
