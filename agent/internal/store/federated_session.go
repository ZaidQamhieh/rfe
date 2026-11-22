package store

import (
	"database/sql"
	"log"
	"time"
)

// FederatedSessionTTL is how long a session an account server vouched for stays valid after the assertion
// that created or last renewed it; a fresh login renews it. It is deliberately short (like an OAuth access
// token): the member never hears from the issuer between logins, so this bounds how long a revoked
// account keeps working here, and the account epoch (see account_epoch.go) cuts it off sooner once any
// higher-epoch login arrives.
const FederatedSessionTTL = 10 * time.Minute

// SetFederatedSessionTTL changes the federated-session lifetime for this open store. Zero restores
// FederatedSessionTTL; a negative value makes every federated session count as already expired.
func (s *DB) SetFederatedSessionTTL(d time.Duration) { s.federatedTTL.Store(int64(d)) }

// federatedSessionCutoff returns the Unix time a federated login must be newer than to still be valid.
func (s *DB) federatedSessionCutoff() int64 {
	ttl := time.Duration(s.federatedTTL.Load())
	if ttl == 0 {
		ttl = FederatedSessionTTL
	}
	return time.Now().Add(-ttl).Unix()
}

// expireFederatedSession ends the session holding this token hash when it is federated and past the cutoff
// (or has no login time): the device is revoked and the share links it minted deleted in one transaction,
// and one audit row is written. The conditional UPDATE makes that happen once however many requests race.
func (s *DB) expireFederatedSession(tokenHash string, cutoff int64) error {
	const expired = `token_hash=? AND login_issuer<>'' AND revoked=0 AND (federated_login_at IS NULL OR federated_login_at<=?)`
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var id, username, issuer string
	err = tx.QueryRow(`SELECT id,username,login_issuer FROM devices WHERE `+expired, tokenHash, cutoff).Scan(&id, &username, &issuer)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM share_tokens WHERE device_id=?`, id); err != nil {
		return err
	}
	res, err := tx.Exec(`UPDATE devices SET revoked=1 WHERE id=? AND `+expired, id, tokenHash, cutoff)
	if err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 1 {
		if len(issuer) > AuditFingerprintLen {
			issuer = issuer[:AuditFingerprintLen]
		}
		if err := s.AppendAudit(AuditFederatedSessionExpired, username+"@"+issuer, id, "federated session expired; log in again"); err != nil {
			log.Printf("audit federated session expiry: %v", err)
		}
	}
	return nil
}
