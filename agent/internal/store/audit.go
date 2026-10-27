package store

import (
	"database/sql"
	"time"
)

// --------- audit log ---------

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
)

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
		limit = 100
	}
	if limit > 500 {
		limit = 500
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
