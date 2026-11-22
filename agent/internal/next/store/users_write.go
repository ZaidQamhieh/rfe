package store

import "errors"

// Errors of DeleteUser.
var (
	// ErrUserNotFound means no account has the given user name.
	ErrUserNotFound = errors.New("user not found")
	// ErrLastUser means the account is the only one left; deleting it would
	// leave no way to sign in with a password.
	ErrLastUser = errors.New("cannot delete the only remaining login account")
)

// DeleteUser removes the login account and ends every session that belongs to
// it, in one transaction: its devices are revoked and detached from the
// account (the schema ties a device to an existing account), and the share
// links those devices minted are deleted. A missing account is ErrUserNotFound
// whatever the number of accounts; the last remaining account is ErrLastUser.
// Any failure rolls everything back.
func (s *DB) DeleteUser(username string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var exists int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM users WHERE username=?`, username).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		return ErrUserNotFound
	}
	var count int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&count); err != nil {
		return err
	}
	if count <= 1 {
		return ErrLastUser
	}
	if _, err := tx.Exec(`DELETE FROM share_links WHERE device_id IN (SELECT id FROM devices WHERE username=?)`, username); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE devices SET revoked=1, username=NULL WHERE username=?`, username); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM users WHERE username=?`, username); err != nil {
		return err
	}
	return tx.Commit()
}
