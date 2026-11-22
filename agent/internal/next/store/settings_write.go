package store

import "fmt"

// SeedSettings writes each key of kv that has no row yet and leaves existing
// rows untouched (an empty stored value still counts as present, which is how
// "roots set to nothing" stays distinct from "roots never set"). All inserts
// happen in one transaction.
func (s *DB) SeedSettings(kv map[string]string) error {
	return s.writeSettings(`INSERT OR IGNORE INTO settings (key,value) VALUES (?,?)`, kv)
}

// SetSettings creates or replaces each key of kv. The whole map is written in
// one transaction, so a reader and a crash see either every change or none.
func (s *DB) SetSettings(kv map[string]string) error {
	return s.writeSettings(`INSERT INTO settings (key,value) VALUES (?,?)
ON CONFLICT(key) DO UPDATE SET value = excluded.value`, kv)
}

// writeSettings runs one prepared statement per key of kv inside a single
// transaction, rolling back on the first failure.
func (s *DB) writeSettings(query string, kv map[string]string) error {
	if len(kv) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for k, v := range kv {
		if _, err := tx.Exec(query, k, v); err != nil {
			return fmt.Errorf("write setting %q: %w", k, err)
		}
	}
	return tx.Commit()
}
