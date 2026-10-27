package store

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// --------- transfers ---------

// Transfer is an in-progress or completed upload session.
type Transfer struct {
	ID             string
	TargetPath     string
	TotalSize      int64
	ChunkSize      int
	SHA256         string
	ReceivedChunks []int  // populated by ChunkNumbers on the resume path only
	ReceivedCount  int    // how many chunks landed; cheap aggregate, always set
	Status         string // open | completed | failed
	TempPath       string
	TotalChunks    int
	DeviceID       string
	UpdatedAt      int64 // unix seconds, stamped on each received chunk; 0 = never
	Overwrite      bool  // may Complete replace an existing target? (PR-50)
}

// TransferHistoryRetention is how long the host keeps completed/failed upload
// session metadata for the web companion's recent activity view. Uploaded
// destination files are never removed by this policy.
const TransferHistoryRetention = 90 * 24 * time.Hour

// TransferLimits caps open sessions and their declared-byte reservations.
// Limits are enforced in the same SQLite INSERT statement that creates the
// reservation row, so concurrent callers cannot both pass a stale count.
type TransferLimits struct {
	MaxHostSessions   int
	MaxHostBytes      int64
	MaxDeviceSessions int
	MaxDeviceBytes    int64
}

// ExpectedChunkLen returns exactly how many bytes chunk n must carry: a full
// ChunkSize for every chunk but the last, and the remainder for the last one.
// A zero-length transfer has one empty chunk. Out-of-range n returns -1, which
// no chunk length can equal.
func (t *Transfer) ExpectedChunkLen(n int) int {
	if n < 0 || n >= t.TotalChunks {
		return -1
	}
	if t.ChunkSize <= 0 {
		return -1
	}
	remaining := t.TotalSize - int64(n)*int64(t.ChunkSize)
	if remaining < 0 {
		return -1
	}
	if remaining > int64(t.ChunkSize) {
		return t.ChunkSize
	}
	return int(remaining)
}

// CreateTransfer inserts a new transfer row.
func (s *DB) CreateTransfer(t *Transfer) error {
	_, err := s.db.Exec(
		`INSERT INTO transfers (id,target_path,total_size,chunk_size,sha256,status,temp_path,total_chunks,device_id,overwrite)
         VALUES (?,?,?,?,?,?,?,?,?,?)`,
		t.ID, t.TargetPath, t.TotalSize, t.ChunkSize, t.SHA256,
		"open", t.TempPath, t.TotalChunks, t.DeviceID, t.Overwrite,
	)
	return err
}

// CreateTransferWithinLimits atomically reserves one open session if both the
// host-wide and per-device session/byte quotas permit it. The returned bool
// is false when a quota is full; a database error is returned separately.
func (s *DB) CreateTransferWithinLimits(t *Transfer, limits TransferLimits) (bool, error) {
	res, err := s.db.Exec(`
INSERT INTO transfers (id,target_path,total_size,chunk_size,sha256,status,temp_path,total_chunks,device_id,overwrite)
SELECT ?,?,?,?,?, 'open',?,?,?,?
WHERE (SELECT COUNT(*) FROM transfers WHERE status='open') < ?
  AND COALESCE((SELECT SUM(total_size) FROM transfers WHERE status='open'),0) <= ? - ?
  AND (SELECT COUNT(*) FROM transfers WHERE status='open' AND device_id=?) < ?
  AND COALESCE((SELECT SUM(total_size) FROM transfers WHERE status='open' AND device_id=?),0) <= ? - ?`,
		t.ID, t.TargetPath, t.TotalSize, t.ChunkSize, t.SHA256,
		t.TempPath, t.TotalChunks, t.DeviceID, t.Overwrite,
		limits.MaxHostSessions, limits.MaxHostBytes, t.TotalSize,
		t.DeviceID, limits.MaxDeviceSessions, t.DeviceID, limits.MaxDeviceBytes, t.TotalSize,
	)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// GetTransfer retrieves a transfer by ID.
func (s *DB) GetTransfer(id string) (*Transfer, error) {
	row := s.db.QueryRow(
		`SELECT id,target_path,total_size,chunk_size,sha256,status,temp_path,total_chunks,device_id,updated_at,overwrite,
                (SELECT COUNT(*) FROM transfer_chunks c WHERE c.transfer_id=transfers.id)
         FROM transfers WHERE id=?`, id,
	)
	return scanTransfer(row)
}

// TouchOpenTransfer marks a status/resume request as activity so the stale
// transfer sweeper does not expire a session while its owner is reconnecting.
func (s *DB) TouchOpenTransfer(id string, at int64) error {
	_, err := s.db.Exec(
		`UPDATE transfers SET updated_at=? WHERE id=? AND status='open'`, at, id,
	)
	return err
}

// ListOpenTransfers returns open sessions for the manager's stale-session
// sweep. It intentionally loads only persisted session metadata, not the
// potentially large received-chunk set.
func (s *DB) ListOpenTransfers() ([]Transfer, error) {
	rows, err := s.db.Query(`
SELECT id,target_path,total_size,chunk_size,sha256,status,temp_path,total_chunks,device_id,updated_at,overwrite,
       (SELECT COUNT(*) FROM transfer_chunks c WHERE c.transfer_id=transfers.id)
FROM transfers WHERE status='open' ORDER BY rowid ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Transfer
	for rows.Next() {
		var t Transfer
		if err := rows.Scan(
			&t.ID, &t.TargetPath, &t.TotalSize, &t.ChunkSize, &t.SHA256,
			&t.Status, &t.TempPath, &t.TotalChunks, &t.DeviceID, &t.UpdatedAt,
			&t.Overwrite, &t.ReceivedCount,
		); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// ListTransferTempPaths returns temp paths still owned by open sessions.
// Terminal rows cannot resume and their old temp files may be swept after the
// retention period if an earlier remove failed.
func (s *DB) ListTransferTempPaths() ([]string, error) {
	rows, err := s.db.Query(`SELECT temp_path FROM transfers WHERE status='open' AND temp_path<>''`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var paths []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		paths = append(paths, p)
	}
	return paths, rows.Err()
}

// DeleteStaleOpenTransfer deletes an open row only if its activity timestamp
// and received-chunk count still match the sweep snapshot. Chunk deletion and
// row deletion are one transaction so no orphan chunk rows remain.
func (s *DB) DeleteStaleOpenTransfer(t *Transfer) (bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return false, fmt.Errorf("begin stale transfer delete: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op after commit

	res, err := tx.Exec(`
DELETE FROM transfers
WHERE id=? AND status='open' AND updated_at=?
  AND (SELECT COUNT(*) FROM transfer_chunks c WHERE c.transfer_id=transfers.id)=?`,
		t.ID, t.UpdatedAt, t.ReceivedCount,
	)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil || n == 0 {
		return false, err
	}
	if _, err := tx.Exec(`DELETE FROM transfer_chunks WHERE transfer_id=?`, t.ID); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

// MarkChunkReceived atomically records chunk n as received.
//
// The read-modify-write of received_chunks must happen inside a single
// transaction: without one, two concurrent chunk uploads can both read the
// same JSON array, add their own chunk number, and write back — and one
// update silently clobbers the other (lost update).
func (s *DB) MarkChunkReceived(id string, n int) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op once committed

	// The transfer must exist: an INSERT against a deleted session would
	// otherwise silently resurrect chunk rows nothing ever cleans up.
	var exists int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM transfers WHERE id=?`, id).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		return sql.ErrNoRows
	}
	// OR IGNORE makes a re-sent chunk a no-op rather than an error: the
	// primary key is the idempotency, so there is no read-modify-write left to
	// lose an update (PR-42).
	if _, err := tx.Exec(
		`INSERT OR IGNORE INTO transfer_chunks (transfer_id, chunk_no) VALUES (?,?)`, id, n,
	); err != nil {
		return err
	}
	if _, err := tx.Exec(
		`UPDATE transfers SET updated_at=? WHERE id=?`, time.Now().Unix(), id,
	); err != nil {
		return err
	}
	return tx.Commit()
}

// HasChunk reports whether chunk n of this transfer has been received. An
// indexed point lookup — the caller must not load every chunk to answer it.
func (s *DB) HasChunk(id string, n int) (bool, error) {
	var found int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM transfer_chunks WHERE transfer_id=? AND chunk_no=?`, id, n,
	).Scan(&found)
	return found > 0, err
}

// ChunkNumbers returns the received chunk numbers, ascending. Used for the
// resume path, which genuinely needs the whole set; per-chunk callers want
// HasChunk instead.
func (s *DB) ChunkNumbers(id string) ([]int, error) {
	rows, err := s.db.Query(
		`SELECT chunk_no FROM transfer_chunks WHERE transfer_id=? ORDER BY chunk_no`, id,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int
	for rows.Next() {
		var n int
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// SetTransferStatus updates the status of a transfer and starts the history
// retention window when the session reaches a terminal state.
func (s *DB) SetTransferStatus(id, status string) error {
	terminalAt := int64(0)
	if status == "completed" || status == "failed" {
		terminalAt = time.Now().Unix()
	}
	_, err := s.db.Exec(`UPDATE transfers SET status=?, terminal_at=? WHERE id=?`, status, terminalAt, id)
	return err
}

// PruneTerminalTransfersBefore removes completed/failed transfer metadata
// older than cutoffUnix, together with its chunk rows, in one transaction.
// Open sessions and legacy terminal rows without a trustworthy terminal time
// are preserved.
func (s *DB) PruneTerminalTransfersBefore(cutoffUnix int64) (int, error) {
	const eligible = `status IN ('completed','failed') AND terminal_at>0 AND terminal_at<?`
	tx, err := s.db.Begin()
	if err != nil {
		return 0, fmt.Errorf("begin terminal transfer prune: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op after commit

	if _, err := tx.Exec(`DELETE FROM transfer_chunks WHERE transfer_id IN (
SELECT id FROM transfers WHERE `+eligible+`
)`, cutoffUnix); err != nil {
		return 0, err
	}
	res, err := tx.Exec(`DELETE FROM transfers WHERE `+eligible, cutoffUnix)
	if err != nil {
		return 0, err
	}
	deleted, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return int(deleted), nil
}

// DeleteTransfer removes a transfer row and its chunk rows (its own history,
// not the uploaded file). Used by the web companion to clear a session.
func (s *DB) DeleteTransfer(id string) error {
	// Chunk rows are keyed by transfer_id with no foreign key, so remove them
	// atomically with the session row or they can outlive a failed deletion.
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // no-op after commit

	res, err := tx.Exec(`DELETE FROM transfers WHERE id=?`, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	if _, err := tx.Exec(`DELETE FROM transfer_chunks WHERE transfer_id=?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

// ListTransfers returns the most recent transfer rows, newest first (rowid
// desc — the table has no created column, and rowid is monotonic with insert
// order). Capped at limit because the web companion only shows recent activity
// and the summary counts come from
// CountTransfersByStatus, not len() of this list. deviceID, if non-empty,
// restricts the rows to that device; username, if non-empty, restricts them
// to devices stamped with that login account; pass "" for no filter.
func (s *DB) ListTransfers(limit int, deviceID, username string) ([]Transfer, error) {
	query := `SELECT id,target_path,total_size,chunk_size,sha256,status,temp_path,total_chunks,device_id,updated_at,overwrite,
                 (SELECT COUNT(*) FROM transfer_chunks c WHERE c.transfer_id=transfers.id)
          FROM transfers`
	args := []any{}
	var where []string
	if deviceID != "" {
		where = append(where, `device_id=?`)
		args = append(args, deviceID)
	}
	if username != "" {
		where = append(where, `device_id IN (SELECT id FROM devices WHERE username=?)`)
		args = append(args, username)
	}
	if len(where) > 0 {
		query += ` WHERE ` + strings.Join(where, ` AND `)
	}
	query += ` ORDER BY rowid DESC LIMIT ?`
	args = append(args, limit)

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Transfer
	for rows.Next() {
		var t Transfer
		if err := rows.Scan(
			&t.ID, &t.TargetPath, &t.TotalSize, &t.ChunkSize, &t.SHA256,
			&t.Status, &t.TempPath, &t.TotalChunks, &t.DeviceID, &t.UpdatedAt, &t.Overwrite,
			&t.ReceivedCount,
		); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// CountTransfersByStatus returns how many transfer rows carry each status
// value (e.g. {"open": 1986, "completed": 3}). Feeds the web companion's
// summary cards so they reflect the whole table, not just the recent page
// ListTransfers returns.
func (s *DB) CountTransfersByStatus() (map[string]int, error) {
	rows, err := s.db.Query(`SELECT status, COUNT(*) FROM transfers GROUP BY status`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string]int)
	for rows.Next() {
		var status string
		var n int
		if err := rows.Scan(&status, &n); err != nil {
			return nil, err
		}
		out[status] = n
	}
	return out, rows.Err()
}

// ActiveTransferWindow bounds how recently a chunk must have landed for an
// "open" transfer to count as genuinely in-flight right now, as opposed to
// one of the thousands of stale never-finalized rows the table accumulates.
// ponytail: fixed window heuristic, not a decay/heartbeat model — revisit if
// slow (multi-minute-per-chunk) transfers need to still read as "active".
const ActiveTransferWindow = 30 * time.Second

// CountActiveTransfers returns the number of "open" transfers that received
// a chunk within ActiveTransferWindow — the web companion's "Active now" stat.
func (s *DB) CountActiveTransfers() (int, error) {
	cutoff := time.Now().Add(-ActiveTransferWindow).Unix()
	var n int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM transfers WHERE status='open' AND updated_at >= ?`,
		cutoff,
	).Scan(&n)
	return n, err
}

// TransferDevice is one entry in the Transfers page's device filter-chip row
// (and the table's Device/User columns).
type TransferDevice struct {
	ID       string
	Label    string
	Username string // "" if the device paired by code rather than login
}

// ListTransferDevices returns the distinct devices that own at least one
// transfer row, labeled via a join against devices (falling back to the raw
// ID if the device was since removed). Rows with no recorded device_id
// (pre-migration transfers) are excluded — the "All" chip already covers them.
func (s *DB) ListTransferDevices() ([]TransferDevice, error) {
	rows, err := s.db.Query(`
        SELECT DISTINCT t.device_id, COALESCE(d.label, t.device_id), COALESCE(d.username, '')
        FROM transfers t LEFT JOIN devices d ON d.id = t.device_id
        WHERE t.device_id != ''
        ORDER BY 2`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []TransferDevice
	for rows.Next() {
		var d TransferDevice
		if err := rows.Scan(&d.ID, &d.Label, &d.Username); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// scanTransfer reads a transfer row. ReceivedChunks is deliberately NOT
// populated: loading every chunk number to answer "is chunk n here?" is the
// quadratic behaviour PR-42 removed. Callers that need the set ask
// ChunkNumbers; ReceivedCount covers the rest.
func scanTransfer(row *sql.Row) (*Transfer, error) {
	var t Transfer
	err := row.Scan(
		&t.ID, &t.TargetPath, &t.TotalSize, &t.ChunkSize, &t.SHA256,
		&t.Status, &t.TempPath, &t.TotalChunks, &t.DeviceID, &t.UpdatedAt, &t.Overwrite,
		&t.ReceivedCount,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}
