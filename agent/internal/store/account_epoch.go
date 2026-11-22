package store

import (
	"database/sql"
	"errors"
	"fmt"
)

// ErrStaleAccountEpoch is returned by a federated login whose assertion carries a lower account epoch than
// this member has already seen from the issuer: it was signed before a revocation the member knows about.
var ErrStaleAccountEpoch = errors.New("assertion epoch is older than the issuer's latest seen epoch")

// auditActorSystem is the audit actor of an event the agent raises by itself rather than for one caller.
const auditActorSystem = "system"

// FederatedGrantProfile is the one definition of what a session vouched for by an account server may do
// with files: browse, download and upload, but not modify, delete or share. The values come back in the
// argument order of SetDeviceFilePermissions (browse, download, upload, modify, delete, share), so a caller
// can pass them straight on. A federated session is never an administrator.
func FederatedGrantProfile() (browse, download, upload, modify, del, share bool) {
	return true, true, true, false, false, false
}

// AccountEpoch returns this agent's account epoch as an issuer: a counter that only ever rises, each time
// access that earlier assertions could still carry is revoked (account deleted, account device revoked or
// removed, or "rfe accounts revoke-all"). It is signed into every assertion this agent issues.
func (s *DB) AccountEpoch() (int64, error) {
	var n int64
	err := s.db.QueryRow(`SELECT COALESCE((SELECT epoch FROM account_epoch WHERE id=1), 0)`).Scan(&n)
	return n, err
}

// AccountEpochFor returns the current account epoch together with whether login account username still
// exists, read in one statement so the pair is consistent. ok is false when the account is gone, and the
// caller must then refuse to sign an assertion for it.
func (s *DB) AccountEpochFor(username string) (epoch int64, ok bool, err error) {
	err = s.db.QueryRow(`SELECT COALESCE((SELECT epoch FROM account_epoch WHERE id=1), 0)
WHERE EXISTS(SELECT 1 FROM users WHERE username=?)`, username).Scan(&epoch)
	if err == sql.ErrNoRows {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return epoch, true, nil
}

// BumpAccountEpoch raises the account epoch by one for the stated reason, writes the audit row
// "account epoch raised to N: reason" and returns the new value. It is what "rfe accounts revoke-all" runs.
func (s *DB) BumpAccountEpoch(reason string) (int64, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	n, err := bumpAccountEpochTx(tx)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	s.auditEpochBump(n, reason)
	return n, nil
}

// bumpAccountEpochTx raises the epoch inside the caller's transaction, so a revocation and its bump commit
// or roll back together. The caller audits the returned value with auditEpochBump after committing.
func bumpAccountEpochTx(tx *sql.Tx) (int64, error) {
	if _, err := tx.Exec(`INSERT INTO account_epoch (id,epoch) VALUES (1,1) ON CONFLICT(id) DO UPDATE SET epoch=epoch+1`); err != nil {
		return 0, err
	}
	var n int64
	if err := tx.QueryRow(`SELECT epoch FROM account_epoch WHERE id=1`).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

// auditEpochBump records an epoch raise. A lost audit row never fails the revocation it describes.
func (s *DB) auditEpochBump(epoch int64, reason string) {
	_ = s.AppendAudit(AuditAccountServer, auditActorSystem, "", fmt.Sprintf("account epoch raised to %d: %s", epoch, reason))
}
