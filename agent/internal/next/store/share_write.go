package store

import (
	"database/sql"
	"errors"
	"fmt"
)

// shareLinkCols lists the share_links columns in the order scanShareLinkRow
// reads them.
const shareLinkCols = `token_hash,path,device_id,created,expires,consumed_at`

// scanShareLinkRow reads one share_links row selected with shareLinkCols.
func scanShareLinkRow(sc interface{ Scan(...any) error }) (ShareLink, error) {
	var l ShareLink
	var dev sql.NullString
	var consumed sql.NullInt64
	if err := sc.Scan(&l.TokenHash, &l.Path, &dev, &l.Created, &l.Expires, &consumed); err != nil {
		return l, err
	}
	l.DeviceID = dev.String
	if consumed.Valid {
		v := consumed.Int64
		l.ConsumedAt = &v
	}
	return l, nil
}

// InsertShareLink stores a new link and its mint history row in one
// transaction, so a link never exists without its history row (or the other
// way round). ev.ID is ignored: the database assigns it. A link with the same
// token hash already existing is an error.
func (s *DB) InsertShareLink(l ShareLink, ev ShareEvent) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO share_links (`+shareLinkCols+`) VALUES (?,?,?,?,?,?)`,
		l.TokenHash, l.Path, nullStr(l.DeviceID), l.Created, l.Expires, l.ConsumedAt); err != nil {
		return fmt.Errorf("insert share link: %w", err)
	}
	if _, err := tx.Exec(`INSERT INTO share_events (token_hash,path,minted_at,expires_at,served_at,requester_ip) VALUES (?,?,?,?,?,?)`,
		ev.TokenHash, ev.Path, ev.MintedAt, ev.ExpiresAt, ev.ServedAt, ev.RequesterIP); err != nil {
		return fmt.Errorf("insert share event: %w", err)
	}
	return tx.Commit()
}

// ShareLinkByHash returns the link with the given token hash, or nil, nil when
// there is none.
func (s *DB) ShareLinkByHash(hash string) (*ShareLink, error) {
	l, err := scanShareLinkRow(s.db.QueryRow(`SELECT `+shareLinkCols+` FROM share_links WHERE token_hash=?`, hash))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &l, nil
}

// ConsumeShareLink marks the link used at time now if, and only if, it is
// still unused and does not expire before now (both Unix seconds). The single
// UPDATE decides a race between two requests: exactly one of them gets true.
func (s *DB) ConsumeShareLink(hash string, now int64) (bool, error) {
	res, err := s.db.Exec(`UPDATE share_links SET consumed_at=? WHERE token_hash=? AND consumed_at IS NULL AND expires>=?`,
		now, hash, now)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// ActiveShareLinks returns the links that are unused and not yet expired at
// time now (Unix seconds), oldest first. deviceID limits the result to the
// links that device minted; "" returns every device's links.
func (s *DB) ActiveShareLinks(deviceID string, now int64) ([]ShareLink, error) {
	query := `SELECT ` + shareLinkCols + ` FROM share_links WHERE consumed_at IS NULL AND expires>=?`
	args := []any{now}
	if deviceID != "" {
		query += ` AND device_id=?`
		args = append(args, deviceID)
	}
	rows, err := s.db.Query(query+` ORDER BY created,token_hash`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ShareLink
	for rows.Next() {
		l, err := scanShareLinkRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// DeleteShareLink removes the link (a revoke) and reports whether one existed.
// Its history rows stay.
func (s *DB) DeleteShareLink(hash string) (bool, error) {
	res, err := s.db.Exec(`DELETE FROM share_links WHERE token_hash=?`, hash)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// RecordShareServed stamps the newest not-yet-served history row of the token
// hash with the time (Unix seconds) and the requester's address.
func (s *DB) RecordShareServed(hash, requesterIP string, at int64) error {
	_, err := s.db.Exec(`UPDATE share_events SET served_at=?, requester_ip=?
WHERE id=(SELECT id FROM share_events WHERE token_hash=? AND served_at IS NULL ORDER BY id DESC LIMIT 1)`,
		at, requesterIP, hash)
	return err
}

// SweepShareLinks deletes every link that expired before time now (Unix
// seconds), used or not, and returns how many it removed.
func (s *DB) SweepShareLinks(now int64) (int, error) {
	res, err := s.db.Exec(`DELETE FROM share_links WHERE expires<?`, now)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	return int(n), err
}

// ShareOwnerDevice returns the device with the given id, revoked or not, for
// the check that a link's minting device may still share. Like DeviceByToken it
// returns nil, nil when there is none, and also when the device is an admin one
// whose account no longer exists.
func (s *DB) ShareOwnerDevice(id string) (*Device, error) {
	row := s.db.QueryRow(`SELECT `+deviceCols+` FROM devices WHERE id=? AND
(origin NOT IN ('login','register') OR (username IS NOT NULL AND EXISTS (SELECT 1 FROM users WHERE users.username=devices.username)))`, id)
	d, err := scanDevice(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &d, nil
}
