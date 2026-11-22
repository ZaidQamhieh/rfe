package store

import (
	"database/sql"
	"fmt"
)

// uploadColumns is the column list of the uploads table in the order
// scanUpload reads it.
const uploadColumns = `id,device_id,target_path,total_size,chunk_size,total_chunks,sha256,overwrite,status,temp_name,updated_at,terminal_at`

// rowScanner is the Scan method shared by *sql.Row and *sql.Rows.
type rowScanner interface{ Scan(dest ...any) error }

// scanUpload reads one uploads row (columns as in uploadColumns) into an
// Upload with no Chunks.
func scanUpload(r rowScanner) (Upload, error) {
	var u Upload
	var ow int
	err := r.Scan(&u.ID, &u.DeviceID, &u.TargetPath, &u.TotalSize, &u.ChunkSize, &u.TotalChunks, &u.SHA256,
		&ow, &u.Status, &u.TempName, &u.UpdatedAt, &u.TerminalAt)
	u.Overwrite = ow != 0
	return u, err
}

// OpenUploads returns every upload whose status is open, ordered by id, without
// their Chunks.
func (s *DB) OpenUploads() ([]Upload, error) {
	rows, err := s.db.Query(`SELECT `+uploadColumns+` FROM uploads WHERE status = ? ORDER BY id`, UploadOpen)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Upload
	for rows.Next() {
		u, err := scanUpload(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// UploadByID returns the upload with the given id without its Chunks, or nil,
// nil when there is none.
func (s *DB) UploadByID(id string) (*Upload, error) {
	u, err := scanUpload(s.db.QueryRow(`SELECT `+uploadColumns+` FROM uploads WHERE id = ?`, id))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// InsertUpload adds a new upload row. A row with the same id already existing
// is an error. Chunks on u are ignored: record them with AddChunk.
func (s *DB) InsertUpload(u Upload) error {
	_, err := s.db.Exec(`INSERT INTO uploads (`+uploadColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		u.ID, u.DeviceID, u.TargetPath, u.TotalSize, u.ChunkSize, u.TotalChunks, u.SHA256,
		b2i(u.Overwrite), u.Status, u.TempName, u.UpdatedAt, u.TerminalAt)
	if err != nil {
		return fmt.Errorf("insert upload %q: %w", u.ID, err)
	}
	return nil
}

// ChunkNumbers returns the chunk numbers recorded for an upload, ascending.
func (s *DB) ChunkNumbers(id string) ([]int, error) {
	rows, err := s.db.Query(`SELECT chunk_no FROM upload_chunks WHERE upload_id = ? ORDER BY chunk_no`, id)
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

// HasChunk reports whether chunk n is recorded for the upload.
func (s *DB) HasChunk(id string, n int) (bool, error) {
	var one int
	err := s.db.QueryRow(`SELECT 1 FROM upload_chunks WHERE upload_id = ? AND chunk_no = ?`, id, n).Scan(&one)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// AddChunk records chunk n of an upload. Recording one that is already there
// changes nothing and is not an error; an unknown upload id is an error.
func (s *DB) AddChunk(id string, n int) error {
	_, err := s.db.Exec(`INSERT OR IGNORE INTO upload_chunks (upload_id,chunk_no) VALUES (?,?)`, id, n)
	if err != nil {
		return fmt.Errorf("add chunk %d of upload %q: %w", n, id, err)
	}
	return nil
}

// DeleteChunks removes the recorded chunks with the given numbers, all in one
// transaction. Numbers that are not recorded are skipped.
func (s *DB) DeleteChunks(id string, nums []int) error {
	if len(nums) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, n := range nums {
		if _, err := tx.Exec(`DELETE FROM upload_chunks WHERE upload_id = ? AND chunk_no = ?`, id, n); err != nil {
			return fmt.Errorf("delete chunk %d of upload %q: %w", n, id, err)
		}
	}
	return tx.Commit()
}

// TouchUpload sets updated_at of an upload that is still open. A missing or
// finished row is left alone and is not an error.
func (s *DB) TouchUpload(id string, at int64) error {
	_, err := s.db.Exec(`UPDATE uploads SET updated_at = ? WHERE id = ? AND status = ?`, at, id, UploadOpen)
	return err
}

// FinishUpload moves an upload to a final status (completed or failed) and
// sets both updated_at and terminal_at to at. Any other status is refused; a
// missing id is not an error.
func (s *DB) FinishUpload(id, status string, at int64) error {
	if status != UploadCompleted && status != UploadFailed {
		return fmt.Errorf("finish upload %q: status %q is not final", id, status)
	}
	_, err := s.db.Exec(`UPDATE uploads SET status = ?, updated_at = ?, terminal_at = ? WHERE id = ?`, status, at, at, id)
	return err
}

// DeleteUpload removes an upload row and its chunk rows in one transaction. A
// missing id is not an error.
func (s *DB) DeleteUpload(id string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM upload_chunks WHERE upload_id = ?`, id); err != nil {
		return fmt.Errorf("delete chunks of upload %q: %w", id, err)
	}
	if _, err := tx.Exec(`DELETE FROM uploads WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete upload %q: %w", id, err)
	}
	return tx.Commit()
}

// DeleteTerminalBefore removes the completed and failed uploads whose
// terminal_at is before cutoff, with their chunk rows, and returns how many
// uploads it removed. Open uploads are never touched.
func (s *DB) DeleteTerminalBefore(cutoff int64) (int, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	const old = `status IN (?,?) AND terminal_at < ?`
	if _, err := tx.Exec(`DELETE FROM upload_chunks WHERE upload_id IN (SELECT id FROM uploads WHERE `+old+`)`,
		UploadCompleted, UploadFailed, cutoff); err != nil {
		return 0, fmt.Errorf("delete old chunks: %w", err)
	}
	res, err := tx.Exec(`DELETE FROM uploads WHERE `+old, UploadCompleted, UploadFailed, cutoff)
	if err != nil {
		return 0, fmt.Errorf("delete old uploads: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return int(n), nil
}
