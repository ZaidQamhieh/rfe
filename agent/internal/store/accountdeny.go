package store

import (
	"database/sql"
	"errors"
	"time"
)

// ErrAccountDenied is returned by LoginDeviceFederatedAtEpoch when the owner of this computer has put the
// account (issuer fingerprint, account name) on the deny list: the login is refused and nothing is stored.
var ErrAccountDenied = errors.New("this account is denied on this computer")

// DeniedAccount is one deny-list entry: an account name as vouched for by the account server whose key has
// this fingerprint.
type DeniedAccount struct {
	Issuer  string
	Sub     string
	Created time.Time
}

// DenyAccount adds (issuer, sub) to the deny list and, in the same transaction, revokes every device that
// account already holds from that issuer and drops those devices' share links, so the denial takes effect
// at once. Denying an already denied account changes nothing.
func (s *DB) DenyAccount(issuer, sub string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`INSERT OR IGNORE INTO account_denied (issuer, sub, created) VALUES (?,?,?)`, issuer, sub, time.Now().Unix()); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM share_tokens WHERE device_id IN (SELECT id FROM devices WHERE login_issuer=? AND username=?)`, issuer, sub); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE devices SET revoked=1 WHERE login_issuer=? AND username=?`, issuer, sub); err != nil {
		return err
	}
	return tx.Commit()
}

// AllowAccount removes (issuer, sub) from the deny list; an account that is not listed is left as it is.
// Devices revoked by the denial stay revoked: the member logs in again to get a new session.
func (s *DB) AllowAccount(issuer, sub string) error {
	_, err := s.db.Exec(`DELETE FROM account_denied WHERE issuer=? AND sub=?`, issuer, sub)
	return err
}

// AccountDenied reports whether (issuer, sub) is on the deny list.
func (s *DB) AccountDenied(issuer, sub string) (bool, error) {
	return accountDeniedTx(s.db, issuer, sub)
}

// accountDeniedTx is AccountDenied on either a transaction or the database handle.
func accountDeniedTx(q interface {
	QueryRow(query string, args ...any) *sql.Row
}, issuer, sub string) (bool, error) {
	var n int
	if err := q.QueryRow(`SELECT COUNT(*) FROM account_denied WHERE issuer=? AND sub=?`, issuer, sub).Scan(&n); err != nil {
		return false, err
	}
	return n > 0, nil
}

// DeniedAccounts lists the deny list, oldest entry first.
func (s *DB) DeniedAccounts() ([]DeniedAccount, error) {
	rows, err := s.db.Query(`SELECT issuer, sub, created FROM account_denied ORDER BY created, issuer, sub`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DeniedAccount
	for rows.Next() {
		var d DeniedAccount
		var created int64
		if err := rows.Scan(&d.Issuer, &d.Sub, &created); err != nil {
			return nil, err
		}
		d.Created = time.Unix(created, 0)
		out = append(out, d)
	}
	return out, rows.Err()
}

// migrateOwnerRevokeAndAccountDeny adds devices.revoked_by_owner (set when the owner revokes a device, so an
// account-server assertion can never revive it, unlike an expired federated session which is also stored
// revoked) and the account_denied table (per-member deny list keyed by issuer fingerprint and account name).
// Rows revoked before this step stay 0: their cause was not recorded.
func migrateOwnerRevokeAndAccountDeny(tx *sql.Tx) error {
	if _, err := tx.Exec(`
CREATE TABLE IF NOT EXISTS account_denied (
    issuer  TEXT NOT NULL,
    sub     TEXT NOT NULL,
    created INTEGER NOT NULL,
    PRIMARY KEY (issuer, sub)
);
`); err != nil {
		return err
	}
	return addColumn(tx, "devices", "revoked_by_owner", "INTEGER NOT NULL DEFAULT 0")
}
