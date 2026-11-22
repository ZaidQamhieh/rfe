package store

import (
	"database/sql"
	"fmt"
)

// User is one login account; PasswordHash is stored exactly as given.
type User struct {
	Username     string
	PasswordHash string
	Created      int64
}

// Device is one paired phone. Username is "" when the device has no account
// (stored as SQL NULL). The grant fields are stored exactly as given.
type Device struct {
	ID, Label, TokenHash string
	Origin               string
	Username             string
	ClientID, PublicKey  string
	Created, LastSeen    int64
	LastAddress          string
	LastVersion          string
	Revoked              bool
	JailRoot             string
	ReadOnly             bool
	CanBrowse            bool
	CanDownload          bool
	CanUpload            bool
	CanModify            bool
	CanDelete            bool
	CanShare             bool
	ViewApps             bool
	LaunchApps           bool
}

// IsAdmin reports whether the device was paired by login or registration;
// the caller must also check that the account still exists (DeviceByToken does).
func (d *Device) IsAdmin() bool { return d.Origin == OriginLogin || d.Origin == OriginRegister }

// ShareLink is one one-time share link, identified by its token hash.
// DeviceID is "" for links that predate owner tracking; ConsumedAt is nil
// while the link is unused.
type ShareLink struct {
	TokenHash, Path string
	DeviceID        string
	Created         int64
	Expires         int64
	ConsumedAt      *int64
}

// ShareEvent is one share mint/serve history row.
type ShareEvent struct {
	ID          int64
	TokenHash   string
	Path        string
	MintedAt    int64
	ExpiresAt   int64
	ServedAt    *int64
	RequesterIP *string
}

// Upload is one resumable upload session with the chunk numbers received.
type Upload struct {
	ID, DeviceID, TargetPath string
	TotalSize                int64
	ChunkSize                int
	TotalChunks              int
	SHA256                   string
	Overwrite                bool
	Status                   string
	TempName                 string // file name under <data>/transfers/
	UpdatedAt, TerminalAt    int64
	Chunks                   []int
}

// AuditEntry is one recorded account/device/share event.
type AuditEntry struct {
	ID                    int64
	At                    int64
	Action, Actor, Target string
	Detail                string
}

// ImportData is everything the one-time import of the old database writes.
// Sequences keep the AUTOINCREMENT counters at least as high as the old
// ones, so an id that was once used is not handed out again.
type ImportData struct {
	Users         []User
	Devices       []Device
	ShareLinks    []ShareLink
	ShareEvents   []ShareEvent
	Uploads       []Upload
	Audit         []AuditEntry
	Settings      map[string]string
	Meta          map[string]string
	AuditSeq      int64
	ShareEventSeq int64
}

// ImportAll writes d into an empty database in a single transaction, parents
// before children (users before devices, uploads before their chunks). Any
// error rolls everything back.
func (s *DB) ImportAll(d ImportData) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := importRows(tx, d); err != nil {
		return err
	}
	return tx.Commit()
}

// importRows runs the inserts of ImportAll inside tx.
func importRows(tx *sql.Tx, d ImportData) error {
	for _, u := range d.Users {
		if _, err := tx.Exec(`INSERT INTO users (username,password_hash,created) VALUES (?,?,?)`,
			u.Username, u.PasswordHash, u.Created); err != nil {
			return fmt.Errorf("insert user %q: %w", u.Username, err)
		}
	}
	for _, v := range d.Devices {
		if _, err := tx.Exec(`INSERT INTO devices (`+deviceCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			v.ID, v.Label, v.TokenHash, v.Origin, nullStr(v.Username), v.ClientID, v.PublicKey, v.Created, v.LastSeen,
			v.LastAddress, v.LastVersion, b2i(v.Revoked), v.JailRoot, b2i(v.ReadOnly), b2i(v.CanBrowse),
			b2i(v.CanDownload), b2i(v.CanUpload), b2i(v.CanModify), b2i(v.CanDelete), b2i(v.CanShare),
			b2i(v.ViewApps), b2i(v.LaunchApps)); err != nil {
			return fmt.Errorf("insert device %q: %w", v.ID, err)
		}
	}
	for _, l := range d.ShareLinks {
		if _, err := tx.Exec(`INSERT INTO share_links (`+shareLinkCols+`) VALUES (?,?,?,?,?,?)`,
			l.TokenHash, l.Path, nullStr(l.DeviceID), l.Created, l.Expires, l.ConsumedAt); err != nil {
			return fmt.Errorf("insert share link for %q: %w", l.Path, err)
		}
	}
	for _, e := range d.ShareEvents {
		if _, err := tx.Exec(`INSERT INTO share_events (id,token_hash,path,minted_at,expires_at,served_at,requester_ip) VALUES (?,?,?,?,?,?,?)`,
			e.ID, e.TokenHash, e.Path, e.MintedAt, e.ExpiresAt, e.ServedAt, e.RequesterIP); err != nil {
			return fmt.Errorf("insert share event %d: %w", e.ID, err)
		}
	}
	for _, u := range d.Uploads {
		if _, err := tx.Exec(`INSERT INTO uploads (`+uploadColumns+`)
VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
			u.ID, u.DeviceID, u.TargetPath, u.TotalSize, u.ChunkSize, u.TotalChunks, u.SHA256, b2i(u.Overwrite),
			u.Status, u.TempName, u.UpdatedAt, u.TerminalAt); err != nil {
			return fmt.Errorf("insert upload %q: %w", u.ID, err)
		}
		for _, n := range u.Chunks {
			if _, err := tx.Exec(`INSERT INTO upload_chunks (upload_id,chunk_no) VALUES (?,?)`, u.ID, n); err != nil {
				return fmt.Errorf("insert chunk %d of upload %q: %w", n, u.ID, err)
			}
		}
	}
	for _, a := range d.Audit {
		if _, err := tx.Exec(`INSERT INTO audit_log (id,at,action,actor,target,detail) VALUES (?,?,?,?,?,?)`,
			a.ID, a.At, a.Action, a.Actor, a.Target, a.Detail); err != nil {
			return fmt.Errorf("insert audit entry %d: %w", a.ID, err)
		}
	}
	for k, v := range d.Settings {
		if _, err := tx.Exec(`INSERT INTO settings (key,value) VALUES (?,?)`, k, v); err != nil {
			return fmt.Errorf("insert setting %q: %w", k, err)
		}
	}
	for k, v := range d.Meta {
		if _, err := tx.Exec(`INSERT INTO meta (key,value) VALUES (?,?)`, k, v); err != nil {
			return fmt.Errorf("insert meta %q: %w", k, err)
		}
	}
	return raiseSequences(tx, map[string]int64{"audit_log": d.AuditSeq, "share_events": d.ShareEventSeq})
}

// raiseSequences lifts each table's AUTOINCREMENT counter to at least the
// given value (a counter only ever grows).
func raiseSequences(tx *sql.Tx, seqs map[string]int64) error {
	for table, want := range seqs {
		var have int64
		err := tx.QueryRow(`SELECT seq FROM sqlite_sequence WHERE name=?`, table).Scan(&have)
		switch {
		case err == sql.ErrNoRows:
			if want <= 0 {
				continue
			}
			_, err = tx.Exec(`INSERT INTO sqlite_sequence (name,seq) VALUES (?,?)`, table, want)
		case err != nil:
		case have < want:
			_, err = tx.Exec(`UPDATE sqlite_sequence SET seq=? WHERE name=?`, want, table)
		}
		if err != nil {
			return fmt.Errorf("raise %s sequence: %w", table, err)
		}
	}
	return nil
}

// deviceCols lists the devices columns in the order scanDevice reads them.
const deviceCols = `id,label,token_hash,origin,username,client_id,public_key,created,last_seen,last_address,last_version,
revoked,jail_root,read_only,can_browse,can_download,can_upload,can_modify,can_delete,can_share,view_apps,launch_apps`

// scanDevice reads one devices row selected with deviceCols.
func scanDevice(sc interface{ Scan(...any) error }) (Device, error) {
	var d Device
	var username sql.NullString
	var revoked, ro, br, dl, up, mod, del, sh, va, la int
	err := sc.Scan(&d.ID, &d.Label, &d.TokenHash, &d.Origin, &username, &d.ClientID, &d.PublicKey, &d.Created, &d.LastSeen,
		&d.LastAddress, &d.LastVersion, &revoked, &d.JailRoot, &ro, &br, &dl, &up, &mod, &del, &sh, &va, &la)
	if err != nil {
		return d, err
	}
	d.Username = username.String
	d.Revoked, d.ReadOnly, d.CanBrowse, d.CanDownload, d.CanUpload = revoked != 0, ro != 0, br != 0, dl != 0, up != 0
	d.CanModify, d.CanDelete, d.CanShare, d.ViewApps, d.LaunchApps = mod != 0, del != 0, sh != 0, va != 0, la != 0
	return d, nil
}

// DeviceByToken finds the device whose token hashes to the stored value.
// Like the old store it returns nil, nil when there is none, and also when
// the device is an admin one (login/register) whose account no longer exists.
// A revoked device is still returned: the caller refuses it.
func (s *DB) DeviceByToken(token string) (*Device, error) {
	row := s.db.QueryRow(`SELECT `+deviceCols+` FROM devices WHERE token_hash=? AND
(origin NOT IN ('login','register') OR (username IS NOT NULL AND EXISTS (SELECT 1 FROM users WHERE users.username=devices.username)))`,
		HashToken(token))
	d, err := scanDevice(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// Users returns every account, ordered by username.
func (s *DB) Users() ([]User, error) {
	rows, err := s.db.Query(`SELECT username,password_hash,created FROM users ORDER BY username`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.Username, &u.PasswordHash, &u.Created); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// Devices returns every device (revoked ones too), ordered by id.
func (s *DB) Devices() ([]Device, error) {
	rows, err := s.db.Query(`SELECT ` + deviceCols + ` FROM devices ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Device
	for rows.Next() {
		d, err := scanDevice(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// ShareLinks returns every share link, ordered by token hash.
func (s *DB) ShareLinks() ([]ShareLink, error) {
	rows, err := s.db.Query(`SELECT ` + shareLinkCols + ` FROM share_links ORDER BY token_hash`)
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

// ShareEvents returns the share history, ordered by id.
func (s *DB) ShareEvents() ([]ShareEvent, error) {
	rows, err := s.db.Query(`SELECT id,token_hash,path,minted_at,expires_at,served_at,requester_ip FROM share_events ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ShareEvent
	for rows.Next() {
		var e ShareEvent
		var served sql.NullInt64
		var ip sql.NullString
		if err := rows.Scan(&e.ID, &e.TokenHash, &e.Path, &e.MintedAt, &e.ExpiresAt, &served, &ip); err != nil {
			return nil, err
		}
		if served.Valid {
			v := served.Int64
			e.ServedAt = &v
		}
		if ip.Valid {
			v := ip.String
			e.RequesterIP = &v
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// Uploads returns every upload session with its received chunk numbers
// (ascending), ordered by id.
func (s *DB) Uploads() ([]Upload, error) {
	rows, err := s.db.Query(`SELECT id,device_id,target_path,total_size,chunk_size,total_chunks,sha256,overwrite,status,temp_name,updated_at,terminal_at
FROM uploads ORDER BY id`)
	if err != nil {
		return nil, err
	}
	var out []Upload
	idx := map[string]int{}
	for rows.Next() {
		u, err := scanUpload(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		idx[u.ID] = len(out)
		out = append(out, u)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	cr, err := s.db.Query(`SELECT upload_id,chunk_no FROM upload_chunks ORDER BY upload_id,chunk_no`)
	if err != nil {
		return nil, err
	}
	defer cr.Close()
	for cr.Next() {
		var id string
		var n int
		if err := cr.Scan(&id, &n); err != nil {
			return nil, err
		}
		i := idx[id]
		out[i].Chunks = append(out[i].Chunks, n)
	}
	return out, cr.Err()
}

// AuditEntries returns the whole audit log, oldest id first.
func (s *DB) AuditEntries() ([]AuditEntry, error) {
	rows, err := s.db.Query(`SELECT id,at,action,actor,target,detail FROM audit_log ORDER BY id`)
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

// Settings returns every settings row.
func (s *DB) Settings() (map[string]string, error) { return s.keyValues("settings") }

// MetaValues returns every meta row (schema version, import provenance).
func (s *DB) MetaValues() (map[string]string, error) { return s.keyValues("meta") }

// keyValues reads a two-column key/value table (the name is a fixed literal).
func (s *DB) keyValues(table string) (map[string]string, error) {
	rows, err := s.db.Query(`SELECT key,value FROM ` + table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}

// countedTables are the tables Counts reports (fixed names, never user input).
var countedTables = []string{"users", "devices", "share_links", "share_events", "uploads", "upload_chunks", "audit_log", "settings"}

// Counts returns the number of rows in each data table.
func (s *DB) Counts() (map[string]int, error) {
	out := map[string]int{}
	for _, t := range countedTables {
		var n int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM ` + t).Scan(&n); err != nil {
			return nil, fmt.Errorf("count %s: %w", t, err)
		}
		out[t] = n
	}
	return out, nil
}
