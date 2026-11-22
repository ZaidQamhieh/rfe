package store

import (
	"database/sql"
	"time"
)

// --------- audit log ---------

// AuditEntries page sizes: the default when limit is not positive, and the cap.
const (
	auditDefaultLimit = 100
	auditMaxLimit     = 500
)

// Audit action names. Kept as constants so the handler, the CLI, and the
// call sites can't drift apart on spelling.
const (
	AuditPair          = "pair"
	AuditRegister      = "register"
	AuditLogin         = "login"
	AuditLoginFailed   = "login_failed"
	AuditDeviceRevoked = "device_revoked"
	AuditDeviceRemoved = "device_removed"
	AuditDeviceUpdated = "device_updated"
	AuditShareCreated  = "share_created"
	AuditShareRevoked  = "share_revoked"
	AuditAgentRestart  = "agent_restart"
	AuditAppLaunch     = "app_launch"
	AuditWake          = "wake_on_lan"
	// AuditAccountServer covers the account-server role: enabled/disabled, a server pinned or forgotten,
	// and each assertion issued.
	AuditAccountServer = "account_server"
	// AuditAccountServerPinChanged is written when the trusted account server's key is set or replaced
	// over the API; its detail carries the old and the new key fingerprint.
	AuditAccountServerPinChanged = "account_server.pin_changed"
	// AuditFederatedSessionExpired is written once when a session an account server vouched for outlives
	// FederatedSessionTTL and is refused.
	AuditFederatedSessionExpired = "federated_session_expired"
)

// AuditFingerprintLen is how many hex characters of a key fingerprint an audit actor carries.
const AuditFingerprintLen = 12

// auditRetention is the number of most recent entries kept. The log records
// only account/device/share events (not per-file operations — those would
// swamp it at transfer volume), so a few thousand rows covers a long history
// while keeping the table small enough to never need its own maintenance.
const auditRetention = 5000

// AuditEntry is one recorded security-relevant event.
type AuditEntry struct {
	ID     int64     `json:"id"`
	At     time.Time `json:"at"`
	Action string    `json:"action"`
	// Actor is the device label or username that caused the event, copied in
	// at write time rather than joined on read — a revoked-then-purged device
	// must still be identifiable in the entry that recorded its removal.
	Actor  string `json:"actor"`
	Target string `json:"target,omitempty"`
	Detail string `json:"detail,omitempty"`
}

// AppendAudit records one event and trims the log back to auditRetention.
// Callers treat failures as non-fatal: losing an audit row must never fail
// the operation being audited.
func (s *DB) AppendAudit(action, actor, target, detail string) error {
	if err := s.appendAudit(action, actor, target, detail); err != nil {
		return err
	}
	if fn := s.auditHook.Load(); fn != nil {
		(*fn)()
	}
	if fn := s.auditActionHook.Load(); fn != nil {
		(*fn)(action)
	}
	return nil
}

// SetAuditActionHook registers fn to run after each audit entry is stored, with that entry's action (the backup
// scheduler uses it to snapshot after a pairing or a revocation). fn must not block.
func (s *DB) SetAuditActionHook(fn func(action string)) { s.auditActionHook.Store(&fn) }

// SetAuditHook registers fn to run after each audit entry is stored (the event
// stream uses it to tell clients the log changed). fn must not block.
func (s *DB) SetAuditHook(fn func()) { s.auditHook.Store(&fn) }

// appendAudit inserts the entry and trims the log back to auditRetention.
func (s *DB) appendAudit(action, actor, target, detail string) error {
	if _, err := s.db.Exec(
		`INSERT INTO audit_log (at,action,actor,target,detail) VALUES (?,?,?,?,?)`,
		time.Now().Unix(), action, actor, target, detail,
	); err != nil {
		return err
	}
	_, err := s.db.Exec(
		`DELETE FROM audit_log WHERE id <= (SELECT MAX(id) FROM audit_log) - ?`,
		auditRetention,
	)
	return err
}

// AuditEntries returns up to limit entries, newest first. A non-zero beforeID
// pages backwards from an earlier response's last id.
func (s *DB) AuditEntries(limit int, beforeID int64) ([]AuditEntry, error) {
	if limit <= 0 {
		limit = auditDefaultLimit
	}
	if limit > auditMaxLimit {
		limit = auditMaxLimit
	}
	var (
		rows *sql.Rows
		err  error
	)
	if beforeID > 0 {
		rows, err = s.db.Query(
			`SELECT id,at,action,actor,target,detail FROM audit_log WHERE id < ? ORDER BY id DESC LIMIT ?`,
			beforeID, limit,
		)
	} else {
		rows, err = s.db.Query(
			`SELECT id,at,action,actor,target,detail FROM audit_log ORDER BY id DESC LIMIT ?`,
			limit,
		)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	entries := []AuditEntry{}
	for rows.Next() {
		var (
			e  AuditEntry
			at int64
		)
		if err := rows.Scan(&e.ID, &at, &e.Action, &e.Actor, &e.Target, &e.Detail); err != nil {
			return nil, err
		}
		e.At = time.Unix(at, 0)
		entries = append(entries, e)
	}
	return entries, rows.Err()
}
