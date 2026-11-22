package store

// PublishIntent records that an upload session began copying its verified bytes into a hidden temp file in
// its destination folder. CreatedAt is Unix seconds. The row exists from before the copy until the final
// name exists (or the temp is gone), so a row that outlives a restart marks a temp the sweep must remove.
type PublishIntent struct {
	SessionID string
	DestDir   string
	TempName  string
	CreatedAt int64
}

// PutPublishIntent records (or, for a retried complete of the same session, replaces) the publish intent of
// sessionID. destDir is the resolved folder that holds the temp and tempName its bare file name.
func (s *DB) PutPublishIntent(sessionID, destDir, tempName string, createdAt int64) error {
	_, err := s.db.Exec(`
INSERT INTO publish_intents(session_id,dest_dir,temp_name,created_at) VALUES(?,?,?,?)
ON CONFLICT(session_id) DO UPDATE SET dest_dir=excluded.dest_dir, temp_name=excluded.temp_name, created_at=excluded.created_at`,
		sessionID, destDir, tempName, createdAt)
	return err
}

// DeletePublishIntent removes the intent of sessionID. A session with no intent is not an error.
func (s *DB) DeletePublishIntent(sessionID string) error {
	_, err := s.db.Exec(`DELETE FROM publish_intents WHERE session_id=?`, sessionID)
	return err
}

// ListPublishIntentsBefore returns the intents created strictly before cutoffUnix, oldest first.
func (s *DB) ListPublishIntentsBefore(cutoffUnix int64) ([]PublishIntent, error) {
	rows, err := s.db.Query(`
SELECT session_id,dest_dir,temp_name,created_at FROM publish_intents WHERE created_at<? ORDER BY created_at ASC, session_id ASC`, cutoffUnix)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PublishIntent
	for rows.Next() {
		var p PublishIntent
		if err := rows.Scan(&p.SessionID, &p.DestDir, &p.TempName, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
