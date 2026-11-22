package store

import "fmt"

// AppendAudit adds one entry to the audit log and trims the log to the newest
// retain entries, in a single transaction, and returns the new entry's id. A
// retain of 0 or less keeps everything. The entry's ID field is ignored: the
// database assigns it.
func (s *DB) AppendAudit(e AuditEntry, retain int) (int64, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`INSERT INTO audit_log (at,action,actor,target,detail) VALUES (?,?,?,?,?)`,
		e.At, e.Action, e.Actor, e.Target, e.Detail)
	if err != nil {
		return 0, fmt.Errorf("insert audit entry: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	if retain > 0 {
		// Ids only grow, so everything at or below (newest - retain) is older
		// than the newest retain entries.
		if _, err := tx.Exec(`DELETE FROM audit_log WHERE id <= ?`, id-int64(retain)); err != nil {
			return 0, fmt.Errorf("trim audit log: %w", err)
		}
	}
	return id, tx.Commit()
}

// ListAudit returns up to limit entries, newest first. A beforeID above zero
// returns only entries with a smaller id, which is how a caller pages
// backwards from the last id of the previous page. A limit of 0 or less
// returns nothing.
func (s *DB) ListAudit(limit int, beforeID int64) ([]AuditEntry, error) {
	if limit <= 0 {
		return nil, nil
	}
	query := `SELECT id,at,action,actor,target,detail FROM audit_log `
	args := []any{}
	if beforeID > 0 {
		query += `WHERE id < ? `
		args = append(args, beforeID)
	}
	query += `ORDER BY id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuditEntry
	for rows.Next() {
		var a AuditEntry
		if err := rows.Scan(&a.ID, &a.At, &a.Action, &a.Actor, &a.Target, &a.Detail); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
