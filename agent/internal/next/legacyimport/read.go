package legacyimport

import (
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	nstore "github.com/ZaidQamhieh/rfe/agent/internal/next/store"
	oldstore "github.com/ZaidQamhieh/rfe/agent/internal/store"
)

// roBusyTimeoutMs is how long the read-only open of the old file waits for a
// lock if the old agent is still writing it.
const roBusyTimeoutMs = 5000

// Settings keys the old agent writes (agent/internal/settings), used to check
// each value's shape. Other keys are copied unchecked, with a warning.
const (
	keyReadOnly     = "readOnly"
	keyAllowSharing = "allowSharing"
	keyMaxUpload    = "maxUploadBytesPerSec"
	keyMaxDownload  = "maxDownloadBytesPerSec"
	keyRoots        = "roots"
	keyAgentName    = "agentName"
	keyPhotoRoot    = "photoBackupRoot"
)

// ErrInvalidSetting is wrapped by the error for a config row whose value the
// new agent could not use; the message names the key.
var ErrInvalidSetting = errors.New("invalid setting")

// roDSN is a read-only SQLite URI for path. url.URL escapes '?', '#' and '%'
// so the query part that follows is not cut short.
func roDSN(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	p := filepath.ToSlash(abs)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p // Windows drive paths: file:///C:/...
	}
	u := url.URL{Scheme: "file", Path: p}
	return fmt.Sprintf("%s?mode=ro&_pragma=busy_timeout(%d)", u.String(), roBusyTimeoutMs), nil
}

// openRO opens a SQLite file read-only on a single connection.
func openRO(path string) (*sql.DB, error) {
	dsn, err := roDSN(path)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	return db, nil
}

// snapshot copies the old database to dst with VACUUM INTO over a read-only
// connection, which yields a consistent single file (any write-ahead log
// content included) and never writes the original. It checks the original's
// integrity first and returns its PRAGMA user_version.
func snapshot(oldPath, dst string) (int, error) {
	if _, err := os.Stat(oldPath); err != nil {
		return 0, err
	}
	db, err := openRO(oldPath)
	if err != nil {
		return 0, err
	}
	defer db.Close()
	var quick string
	if err := db.QueryRow("PRAGMA quick_check").Scan(&quick); err != nil {
		return 0, fmt.Errorf("quick_check: %w", err)
	}
	if quick != "ok" {
		return 0, fmt.Errorf("old database failed quick_check: %s", quick)
	}
	var version int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return 0, fmt.Errorf("read user_version: %w", err)
	}
	if _, err := db.Exec("VACUUM INTO ?", dst); err != nil {
		return 0, fmt.Errorf("vacuum into copy: %w", err)
	}
	return version, nil
}

// migrateCopy runs the old store's own migrations on the scratch copy
// (named agent.db inside dir), so every old version reads as the latest one.
func migrateCopy(dir string) error {
	db, err := oldstore.Open(dir)
	if err != nil {
		return err
	}
	return db.Close()
}

// readLegacy reads every importable table of the migrated scratch copy at
// path and converts it to the new shapes. dataDir is where the upload temp
// files live (in transfers/).
func (im *importer) readLegacy(path, dataDir string) (nstore.ImportData, error) {
	var d nstore.ImportData
	db, err := openRO(path)
	if err != nil {
		return d, err
	}
	defer db.Close()

	if d.Users, err = readUsers(db); err != nil {
		return d, err
	}
	accounts := map[string]bool{}
	for _, u := range d.Users {
		accounts[u.Username] = true
	}
	if d.Devices, err = im.readDevices(db, accounts); err != nil {
		return d, err
	}
	if d.ShareLinks, err = readShareLinks(db); err != nil {
		return d, err
	}
	if d.ShareEvents, err = readShareEvents(db); err != nil {
		return d, err
	}
	if d.Uploads, err = im.readUploads(db, dataDir); err != nil {
		return d, err
	}
	if d.Audit, err = readAudit(db); err != nil {
		return d, err
	}
	if d.Settings, err = im.readSettings(db); err != nil {
		return d, err
	}
	seqs, err := readSequences(db)
	if err != nil {
		return d, err
	}
	d.AuditSeq, d.ShareEventSeq = seqs["audit_log"], seqs["share_log"]
	for _, t := range []string{"pairing_codes", "pair_requests"} {
		var n int
		if err := db.QueryRow("SELECT COUNT(*) FROM " + t).Scan(&n); err != nil {
			return d, fmt.Errorf("count %s: %w", t, err)
		}
		im.rep.NotImported[t] = n
	}
	d.Meta = metaFor(im.rep.OldUserVersion, im.now())
	return d, nil
}

// readUsers copies the accounts unchanged.
func readUsers(db *sql.DB) ([]nstore.User, error) {
	rows, err := db.Query(`SELECT username,password_hash,created FROM users ORDER BY username`)
	if err != nil {
		return nil, fmt.Errorf("read users: %w", err)
	}
	defer rows.Close()
	var out []nstore.User
	for rows.Next() {
		var u nstore.User
		if err := rows.Scan(&u.Username, &u.PasswordHash, &u.Created); err != nil {
			return nil, fmt.Errorf("read users: %w", err)
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// readDevices copies every device row as it is (no grant defaults, no
// merging on client_id). The only conversions: via_login=1 becomes origin
// 'login', anything else 'code'; and a username with no matching account
// becomes empty (the new schema's foreign key allows only real accounts), with
// a warning. Such a login device then fails the admin check as it did before.
func (im *importer) readDevices(db *sql.DB, accounts map[string]bool) ([]nstore.Device, error) {
	rows, err := db.Query(`SELECT id,label,token_hash,created,last_seen,revoked,client_id,last_address,last_version,jail_root,
read_only,public_key,via_login,username,view_apps,launch_apps,can_browse,can_download,can_upload,can_modify,can_delete,can_share
FROM devices ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("read devices: %w", err)
	}
	defer rows.Close()
	var out []nstore.Device
	for rows.Next() {
		var d nstore.Device
		var revoked, ro, via, va, la, br, dl, up, mod, del, sh int
		if err := rows.Scan(&d.ID, &d.Label, &d.TokenHash, &d.Created, &d.LastSeen, &revoked, &d.ClientID, &d.LastAddress,
			&d.LastVersion, &d.JailRoot, &ro, &d.PublicKey, &via, &d.Username, &va, &la, &br, &dl, &up, &mod, &del, &sh); err != nil {
			return nil, fmt.Errorf("read devices: %w", err)
		}
		d.Revoked, d.ReadOnly, d.ViewApps, d.LaunchApps = revoked != 0, ro != 0, va != 0, la != 0
		d.CanBrowse, d.CanDownload, d.CanUpload = br != 0, dl != 0, up != 0
		d.CanModify, d.CanDelete, d.CanShare = mod != 0, del != 0, sh != 0
		d.Origin = nstore.OriginCode
		if via != 0 {
			d.Origin = nstore.OriginLogin
		}
		if d.Username != "" && !accounts[d.Username] {
			im.warnf("device %s names account %q which does not exist; stored without an account", d.ID, d.Username)
			d.Username = ""
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// readShareLinks converts share_tokens to share_links (unconsumed).
func readShareLinks(db *sql.DB) ([]nstore.ShareLink, error) {
	rows, err := db.Query(`SELECT token_hash,path,created,expires,COALESCE(device_id,'') FROM share_tokens ORDER BY token_hash`)
	if err != nil {
		return nil, fmt.Errorf("read share_tokens: %w", err)
	}
	defer rows.Close()
	var out []nstore.ShareLink
	for rows.Next() {
		var l nstore.ShareLink
		if err := rows.Scan(&l.TokenHash, &l.Path, &l.Created, &l.Expires, &l.DeviceID); err != nil {
			return nil, fmt.Errorf("read share_tokens: %w", err)
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// readShareEvents converts share_log to share_events, ids kept.
func readShareEvents(db *sql.DB) ([]nstore.ShareEvent, error) {
	rows, err := db.Query(`SELECT id,token_hash,path,minted_at,expires_at,served_at,requester_ip FROM share_log ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("read share_log: %w", err)
	}
	defer rows.Close()
	var out []nstore.ShareEvent
	for rows.Next() {
		var e nstore.ShareEvent
		var served sql.NullInt64
		var ip sql.NullString
		if err := rows.Scan(&e.ID, &e.TokenHash, &e.Path, &e.MintedAt, &e.ExpiresAt, &served, &ip); err != nil {
			return nil, fmt.Errorf("read share_log: %w", err)
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

// readAudit copies the audit log with ids preserved, so the `before` paging
// cursor of the audit endpoint keeps working.
func readAudit(db *sql.DB) ([]nstore.AuditEntry, error) {
	rows, err := db.Query(`SELECT id,at,action,actor,target,detail FROM audit_log ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("read audit_log: %w", err)
	}
	defer rows.Close()
	var out []nstore.AuditEntry
	for rows.Next() {
		var a nstore.AuditEntry
		if err := rows.Scan(&a.ID, &a.At, &a.Action, &a.Actor, &a.Target, &a.Detail); err != nil {
			return nil, fmt.Errorf("read audit_log: %w", err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// readSequences returns the old AUTOINCREMENT counters by table name.
func readSequences(db *sql.DB) (map[string]int64, error) {
	rows, err := db.Query(`SELECT name,seq FROM sqlite_sequence`)
	if err != nil {
		return nil, fmt.Errorf("read sqlite_sequence: %w", err)
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var name string
		var seq int64
		if err := rows.Scan(&name, &seq); err != nil {
			return nil, err
		}
		out[name] = seq
	}
	return out, rows.Err()
}

// readSettings copies the config table to settings, validating each value.
// An invalid value stops the import with an error naming the key.
func (im *importer) readSettings(db *sql.DB) (map[string]string, error) {
	rows, err := db.Query(`SELECT key,value FROM config ORDER BY key`)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, fmt.Errorf("read config: %w", err)
		}
		if err := validateSetting(k, v); err != nil {
			return nil, err
		}
		switch k {
		case keyReadOnly, keyAllowSharing, keyMaxUpload, keyMaxDownload, keyRoots, keyAgentName, keyPhotoRoot:
		default:
			im.warnf("config key %q is not a known setting; copied as it is", k)
		}
		out[k] = v
	}
	return out, rows.Err()
}

// validateSetting checks a config value the way the old agent reads it: the
// two switches hold "true" or "false", the bandwidth limits a non-negative
// whole number, and every value must be valid UTF-8 without NUL. Empty means
// "unset" to the old agent and is accepted.
func validateSetting(key, value string) error {
	bad := func(why string) error {
		return fmt.Errorf("%w: config key %q has value %q: %s", ErrInvalidSetting, key, value, why)
	}
	if !utf8.ValidString(value) || strings.ContainsRune(value, 0) {
		return bad("not valid text")
	}
	if value == "" {
		return nil
	}
	switch key {
	case keyReadOnly, keyAllowSharing:
		if value != "true" && value != "false" {
			return bad(`expected "true" or "false"`)
		}
	case keyMaxUpload, keyMaxDownload:
		if n, err := strconv.ParseInt(value, 10, 64); err != nil || n < 0 {
			return bad("expected a whole number of bytes per second (0 = unlimited)")
		}
	}
	return nil
}
