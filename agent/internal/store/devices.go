package store

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// --------- devices ---------

// Device represents a paired device.
type Device struct {
	ID          string
	Label       string
	TokenHash   string
	Created     time.Time
	LastSeen    time.Time
	Revoked     bool
	LastAddress string
	LastVersion string
	JailRoot    string
	ReadOnly    bool
	PublicKey   string
	ViaLogin    bool
	ViewApps    bool
	LaunchApps  bool
	CanBrowse   bool
	CanDownload bool
	CanUpload   bool
	CanModify   bool
	CanDelete   bool
	CanShare    bool
}

// CreateDevice inserts a new device row. token is the raw bearer token —
// only its SHA-256 hash is stored.
func (s *DB) CreateDevice(id, label, token string) error {
	hash := hashToken(token)
	now := time.Now().Unix()
	_, err := s.db.Exec(
		`INSERT INTO devices (id,label,token_hash,created,last_seen,revoked) VALUES (?,?,?,?,?,0)`,
		id, label, hash, now, now,
	)
	return err
}

// DevicePublicKeyByClientID returns the pinned public key for the device
// with the given hardware-stable clientID, or "" if no device (or no pinned
// key yet) exists for it. Callers use this to detect a device-key mismatch
// *before* calling UpsertDevice — see pairHandler/loginHandler.
func (s *DB) DevicePublicKeyByClientID(clientID string) (string, error) {
	if clientID == "" {
		return "", nil
	}
	var key string
	err := s.db.QueryRow(`SELECT public_key FROM devices WHERE client_id=?`, clientID).Scan(&key)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return key, err
}

// ClientDevice is the existing device row a hardware client id maps to.
type ClientDevice struct {
	ID        string
	Label     string
	PublicKey string
	Revoked   bool
}

// ClientDeviceByID returns the device already paired under clientID; ok is false for a new client.
func (s *DB) ClientDeviceByID(clientID string) (ClientDevice, bool, error) {
	if clientID == "" {
		return ClientDevice{}, false, nil
	}
	var d ClientDevice
	var revoked int
	err := s.db.QueryRow(`SELECT id,label,public_key,revoked FROM devices WHERE client_id=?`, clientID).
		Scan(&d.ID, &d.Label, &d.PublicKey, &revoked)
	if errors.Is(err, sql.ErrNoRows) {
		return ClientDevice{}, false, nil
	}
	d.Revoked = revoked == 1
	return d, err == nil, err
}

// ResetDeviceAccess puts a device back to what a freshly paired one gets: browse only, no app access,
// no share links. The jail root and read-only flag are kept, they only ever narrow access.
func (s *DB) ResetDeviceAccess(id string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`UPDATE devices SET can_browse=1,can_download=0,can_upload=0,can_modify=0,can_delete=0,can_share=0,view_apps=0,launch_apps=0 WHERE id=?`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM share_tokens WHERE device_id=?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

// UpsertDevice pairs a device, deduplicating by the hardware-stable clientID.
// If clientID is non-empty and a device with that clientID already exists, its
// token is rotated, it is un-revoked, and its existing id is returned — so a
// phone that re-pairs (after clearing app data, reinstalling, or losing its
// token) reuses its row instead of creating a duplicate. Otherwise a fresh
// device with a new UUID is inserted. Returns the device id to hand back to the
// client. publicKey is pinned on the row (callers must have already checked
// it against DevicePublicKeyByClientID for a mismatch — this does not
// re-check). viaLogin marks the resulting token as owner-trusted (minted by
// /login or /register, i.e. the account password) vs an ordinary /pair
// device — see the via_login column comment in migrateBaseline().
func (s *DB) UpsertDevice(clientID, label, token, publicKey string, viaLogin bool) (string, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback() }()

	id, err := upsertDeviceTx(tx, clientID, label, token, publicKey, viaLogin)
	if err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return id, nil
}

// upsertDeviceTx is UpsertDevice's body, factored out so the account
// workflows can run it inside their own transaction rather than as a separate
// one (PR-45).
//
// PR-44: the find-or-create is one transaction so two concurrent pairings of
// the same client_id can't both insert (the partial unique index is the
// DB-level backstop).
func upsertDeviceTx(tx *sql.Tx, clientID, label, token, publicKey string, viaLogin bool) (string, error) {
	hash := hashToken(token)
	now := time.Now().Unix()
	viaLoginInt := 0
	if viaLogin {
		viaLoginInt = 1
	}

	if clientID != "" {
		var existingID string
		err := tx.QueryRow(
			`SELECT id FROM devices WHERE client_id=?`, clientID,
		).Scan(&existingID)
		if err == nil {
			// Same phone re-pairing: rotate token, clear revoked, refresh label.
			if _, err := tx.Exec(
				`UPDATE devices SET token_hash=?, label=?, last_seen=?, revoked=0, public_key=?, via_login=?,
can_browse=CASE WHEN ?=1 THEN 1 WHEN via_login=1 THEN 1 ELSE can_browse END,
can_download=CASE WHEN ?=1 THEN 1 WHEN via_login=1 THEN 0 ELSE can_download END,
can_upload=CASE WHEN ?=1 THEN 1 WHEN via_login=1 THEN 0 ELSE can_upload END,
can_modify=CASE WHEN ?=1 THEN 1 WHEN via_login=1 THEN 0 ELSE can_modify END,
can_delete=CASE WHEN ?=1 THEN 1 WHEN via_login=1 THEN 0 ELSE can_delete END,
can_share=CASE WHEN ?=1 THEN 1 WHEN via_login=1 THEN 0 ELSE can_share END
WHERE id=?`,
				hash, label, now, publicKey, viaLoginInt,
				viaLoginInt, viaLoginInt, viaLoginInt, viaLoginInt, viaLoginInt, viaLoginInt, existingID,
			); err != nil {
				return "", err
			}
			return existingID, nil
		}
		if err != sql.ErrNoRows {
			return "", err
		}
	}

	id := uuid.New().String()
	browse, download, upload, modify, del, share := 1, 0, 0, 0, 0, 0
	if viaLogin {
		download, upload, modify, del, share = 1, 1, 1, 1, 1
	}
	if _, err := tx.Exec(
		`INSERT INTO devices (id,label,token_hash,created,last_seen,revoked,client_id,public_key,via_login,
can_browse,can_download,can_upload,can_modify,can_delete,can_share) VALUES (?,?,?,?,?,0,?,?,?,?,?,?,?,?,?)`,
		id, label, hash, now, now, clientID, publicKey, viaLoginInt, browse, download, upload, modify, del, share,
	); err != nil {
		return "", err
	}
	return id, nil
}

// ErrAccountExists is returned by RegisterAccount when this computer already
// has a login account. RFE allows exactly one.
var ErrAccountExists = errors.New("an account already exists")

// RegisterAccount creates the computer's single login account and the
// registering device together, in one transaction (PR-45).
//
// Split across separate calls, as this used to be, the "exactly one account"
// rule was decided by a SELECT that another registration could invalidate
// before the INSERT landed, and a failure between the two left an account with
// no device — with the pairing code already burned, so nobody could retry.
func (s *DB) RegisterAccount(clientID, label, token, publicKey, username, passwordHash string) (string, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback() }()

	var users int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&users); err != nil {
		return "", err
	}
	if users > 0 {
		return "", ErrAccountExists
	}
	if _, err := tx.Exec(
		`INSERT INTO users (username,password_hash,created) VALUES (?,?,?)`,
		username, passwordHash, time.Now().Unix(),
	); err != nil {
		return "", err
	}
	deviceID, err := upsertDeviceTx(tx, clientID, label, token, publicKey, true)
	if err != nil {
		return "", err
	}
	if _, err := tx.Exec(`UPDATE devices SET username=? WHERE id=?`, username, deviceID); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return deviceID, nil
}

// LoginDevice records a successful password login: the device row and the
// account that authenticated it land together, so a failure can't leave a
// usable token stamped with no username (PR-45).
func (s *DB) LoginDevice(clientID, label, token, publicKey, username, passwordHash string) (string, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback() }()

	// The account may have been deleted or replaced after the handler checked
	// the password. Bind token creation to the same credential row atomically.
	var accountMatches int
	if err := tx.QueryRow(
		`SELECT EXISTS(SELECT 1 FROM users WHERE username=? AND password_hash=?)`,
		username, passwordHash,
	).Scan(&accountMatches); err != nil {
		return "", err
	}
	if accountMatches == 0 {
		return "", ErrLoginAccountChanged
	}

	deviceID, err := upsertDeviceTx(tx, clientID, label, token, publicKey, true)
	if err != nil {
		return "", err
	}
	if _, err := tx.Exec(`UPDATE devices SET username=? WHERE id=?`, username, deviceID); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return deviceID, nil
}

// DeviceByToken returns the device whose token matches the given raw token,
// or (nil,nil) if not found.
func (s *DB) DeviceByToken(token string) (*Device, error) {
	hash := hashToken(token)
	row := s.db.QueryRow(
		`SELECT id,label,token_hash,created,last_seen,revoked,last_address,last_version,jail_root,read_only,public_key,
	via_login,view_apps,launch_apps,can_browse,can_download,can_upload,can_modify,can_delete,can_share FROM devices
WHERE token_hash=? AND (via_login=0 OR (via_login=1 AND username<>'' AND EXISTS(
    SELECT 1 FROM users WHERE users.username=devices.username
)))`, hash,
	)
	d, err := scanDevice(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return d, err
}

// TouchDevice updates last_seen, last_address, and last_version for the given
// id. Called on every authenticated request to record the caller's most
// recent network address and reported app version.
func (s *DB) TouchDevice(id, address, version string) error {
	// PR-41: skip the write if this device was touched within touchInterval.
	now := time.Now()
	s.touchMu.Lock()
	if last, ok := s.lastTouch[id]; ok && now.Sub(last) < touchInterval {
		s.touchMu.Unlock()
		return nil
	}
	s.lastTouch[id] = now
	s.touchMu.Unlock()

	_, err := s.db.Exec(
		`UPDATE devices SET last_seen=?, last_address=?, last_version=? WHERE id=?`,
		now.Unix(), address, version, id,
	)
	return err
}

func scanDevice(row *sql.Row) (*Device, error) {
	var d Device
	var created, lastSeen int64
	var revoked, readOnly, viaLogin, viewApps, launchApps, browse, download, upload, modify, del, share int
	err := row.Scan(&d.ID, &d.Label, &d.TokenHash, &created, &lastSeen, &revoked, &d.LastAddress, &d.LastVersion, &d.JailRoot, &readOnly, &d.PublicKey, &viaLogin, &viewApps, &launchApps, &browse, &download, &upload, &modify, &del, &share)
	if err != nil {
		return nil, err
	}
	d.Created = time.Unix(created, 0)
	d.LastSeen = time.Unix(lastSeen, 0)
	d.Revoked = revoked != 0
	d.ReadOnly = readOnly != 0
	d.ViaLogin = viaLogin != 0
	d.ViewApps = viewApps != 0
	d.LaunchApps = launchApps != 0
	d.CanBrowse, d.CanDownload, d.CanUpload = browse != 0, download != 0, upload != 0
	d.CanModify, d.CanDelete, d.CanShare = modify != 0, del != 0, share != 0
	return &d, nil
}

// rowScanner is satisfied by both *sql.Row and *sql.Rows.
type rowScanner interface {
	Scan(dest ...any) error
}

func scanDeviceFrom(sc rowScanner) (*Device, error) {
	var d Device
	var created, lastSeen int64
	var revoked, readOnly, viaLogin, viewApps, launchApps, browse, download, upload, modify, del, share int
	if err := sc.Scan(&d.ID, &d.Label, &d.TokenHash, &created, &lastSeen, &revoked, &d.LastAddress, &d.LastVersion, &d.JailRoot, &readOnly, &d.PublicKey, &viaLogin, &viewApps, &launchApps, &browse, &download, &upload, &modify, &del, &share); err != nil {
		return nil, err
	}
	d.Created = time.Unix(created, 0)
	d.LastSeen = time.Unix(lastSeen, 0)
	d.Revoked = revoked != 0
	d.ReadOnly = readOnly != 0
	d.ViaLogin = viaLogin != 0
	d.ViewApps = viewApps != 0
	d.LaunchApps = launchApps != 0
	d.CanBrowse, d.CanDownload, d.CanUpload = browse != 0, download != 0, upload != 0
	d.CanModify, d.CanDelete, d.CanShare = modify != 0, del != 0, share != 0
	return &d, nil
}

// ListDevices returns all paired devices (including revoked), oldest first.
func (s *DB) ListDevices() ([]Device, error) {
	rows, err := s.db.Query(
		`SELECT id,label,token_hash,created,last_seen,revoked,last_address,last_version,jail_root,read_only,public_key,via_login,view_apps,launch_apps,can_browse,can_download,can_upload,can_modify,can_delete,can_share FROM devices ORDER BY created`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Device
	for rows.Next() {
		d, err := scanDeviceFrom(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *d)
	}
	return out, rows.Err()
}

// ResolveDeviceID maps a full id or a unique id prefix to a full device id, for
// convenient CLI use (e.g. "revoke 9789"). Returns an error if the prefix
// matches no device or more than one.
func (s *DB) ResolveDeviceID(prefix string) (string, error) {
	if prefix == "" {
		return "", fmt.Errorf("empty device id")
	}
	devices, err := s.ListDevices()
	if err != nil {
		return "", err
	}
	var matches []string
	for _, d := range devices {
		if d.ID == prefix {
			return d.ID, nil // exact match wins immediately
		}
		if strings.HasPrefix(d.ID, prefix) {
			matches = append(matches, d.ID)
		}
	}
	switch len(matches) {
	case 0:
		return "", fmt.Errorf("no device matches %q", prefix)
	case 1:
		return matches[0], nil
	default:
		return "", fmt.Errorf("%q is ambiguous (%d devices match)", prefix, len(matches))
	}
}

// RevokeDevice marks a device revoked; its token is rejected by authMiddleware.
func (s *DB) RevokeDevice(id string) error {
	return s.execWithShareTokenPurge(id, `UPDATE devices SET revoked=1 WHERE id=?`)
}

// DeleteDevice permanently removes a device row. Used to clear out revoked
// devices so the paired-devices list doesn't accumulate stale entries.
func (s *DB) DeleteDevice(id string) error {
	return s.execWithShareTokenPurge(id, `DELETE FROM devices WHERE id=?`)
}

// execWithShareTokenPurge runs a device-ending statement and drops the
// device's outstanding share links in the same transaction, so a revoked or
// deleted device cannot leave a working link behind.
func (s *DB) execWithShareTokenPurge(id, stmt string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(stmt, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM share_tokens WHERE device_id=?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

// GetDeviceByID returns the device with the given id, or (nil,nil) if not found.
func (s *DB) GetDeviceByID(id string) (*Device, error) {
	row := s.db.QueryRow(
		`SELECT id,label,token_hash,created,last_seen,revoked,last_address,last_version,jail_root,read_only,public_key,via_login,view_apps,launch_apps,can_browse,can_download,can_upload,can_modify,can_delete,can_share FROM devices WHERE id=?`, id,
	)
	d, err := scanDevice(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return d, err
}

// SetDeviceJail sets (or clears, if jailRoot is "") the per-device path jail
// for device id (H2). The caller is responsible for validating jailRoot
// before calling this — see setDeviceJailHandler in the server package.
func (s *DB) SetDeviceJail(id, jailRoot string) error {
	_, err := s.db.Exec(`UPDATE devices SET jail_root=? WHERE id=?`, jailRoot, id)
	return err
}

// SetDeviceReadOnly sets (true) or clears (false) the per-device read-only
// flag for device id (#8). A read-only device may browse/download but every
// filesystem write is rejected with READ_ONLY.
func (s *DB) SetDeviceReadOnly(id string, ro bool) error {
	v := 0
	if ro {
		v = 1
	}
	_, err := s.db.Exec(`UPDATE devices SET read_only=? WHERE id=?`, v, id)
	return err
}

// SetDeviceAppPermissions updates a device's app-catalog and app-launch
// permissions together. The server validates the implication that launching
// also requires app visibility before calling this method.
func (s *DB) SetDeviceAppPermissions(id string, viewApps, launchApps bool) error {
	view := 0
	if viewApps {
		view = 1
	}
	launch := 0
	if launchApps {
		launch = 1
	}
	_, err := s.db.Exec(`UPDATE devices SET view_apps=?, launch_apps=? WHERE id=?`, view, launch, id)
	return err
}

// SetDeviceFilePermissions atomically updates the independent filesystem
// capabilities managed by the admin device settings endpoint.
func (s *DB) SetDeviceFilePermissions(id string, browse, download, upload, modify, del, share bool) error {
	values := []int{0, 0, 0, 0, 0, 0}
	for i, enabled := range []bool{browse, download, upload, modify, del, share} {
		if enabled {
			values[i] = 1
		}
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`UPDATE devices SET can_browse=?,can_download=?,can_upload=?,can_modify=?,can_delete=?,can_share=? WHERE id=?`,
		values[0], values[1], values[2], values[3], values[4], values[5], id); err != nil {
		return err
	}
	if !share {
		if _, err := tx.Exec(`DELETE FROM share_tokens WHERE device_id=?`, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// --------- pairing codes ---------

// CreatePairingCode stores a one-time pairing code valid until expires. Stored
// in the DB (not daemon memory) so the `rfe pair` CLI can mint a code the
// running daemon will accept, without a restart. Opportunistically clears
// already-expired codes. jailRoot/readOnly are guest-mode defaults applied to
// the device created when this code is redeemed; pass "", false for a normal
// (non-guest) code.
func (s *DB) CreatePairingCode(code string, expires time.Time, jailRoot string, readOnly bool) error {
	return s.CreatePairingCodeFor(code, expires, jailRoot, readOnly, false)
}

// CreatePairingCodeFor is CreatePairingCode with canRegister: whether the code
// may also create the owner account.
func (s *DB) CreatePairingCodeFor(code string, expires time.Time, jailRoot string, readOnly, canRegister bool) error {
	_, _ = s.db.Exec(`DELETE FROM pairing_codes WHERE expires < ?`, time.Now().Unix())
	ro, reg := 0, 0
	if readOnly {
		ro = 1
	}
	if canRegister {
		reg = 1
	}
	_, err := s.db.Exec(
		`INSERT OR REPLACE INTO pairing_codes (code, expires, jail_root, read_only, can_register) VALUES (?, ?, ?, ?, ?)`,
		code, expires.Unix(), jailRoot, ro, reg,
	)
	return err
}

// PairingCodeInfo describes a validated one-time pairing code: whether it was
// valid, and any guest-mode defaults (jailRoot/readOnly) to apply to the
// device created when redeeming it.
type PairingCodeInfo struct {
	Valid       bool
	JailRoot    string
	ReadOnly    bool
	CanRegister bool // may also create the owner account
}

// PeekPairingCode reports what a code would allow without using it up. /register uses it to refuse an
// ordinary pairing code before consuming it (audit F-07).
func (s *DB) PeekPairingCode(code string) PairingCodeInfo {
	if code == "" {
		return PairingCodeInfo{}
	}
	var expires int64
	var jailRoot string
	var readOnly, canRegister int
	err := s.db.QueryRow(
		`SELECT expires, jail_root, read_only, can_register FROM pairing_codes WHERE code=?`, code,
	).Scan(&expires, &jailRoot, &readOnly, &canRegister)
	if err != nil || time.Now().Unix() > expires {
		return PairingCodeInfo{}
	}
	return PairingCodeInfo{Valid: true, JailRoot: jailRoot, ReadOnly: readOnly != 0, CanRegister: canRegister != 0}
}

// ConsumePairingCode validates code and removes it (single-use).
func (s *DB) ConsumePairingCode(code string) PairingCodeInfo {
	if code == "" {
		return PairingCodeInfo{}
	}
	var expires int64
	var jailRoot string
	var readOnly, canRegister int
	// PR-10: consume the code atomically. DELETE ... RETURNING guarantees only
	// one concurrent caller can claim a single-use code — a SELECT-then-DELETE
	// race let two requests both validate before either deleted.
	err := s.db.QueryRow(
		`DELETE FROM pairing_codes WHERE code=? RETURNING expires, jail_root, read_only, can_register`, code,
	).Scan(&expires, &jailRoot, &readOnly, &canRegister)
	if err != nil {
		return PairingCodeInfo{}
	}
	if time.Now().Unix() > expires {
		return PairingCodeInfo{}
	}
	return PairingCodeInfo{Valid: true, JailRoot: jailRoot, ReadOnly: readOnly != 0, CanRegister: canRegister != 0}
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// --------- config ---------

// GetConfig retrieves a config value by key.
func (s *DB) GetConfig(key string) (string, error) {
	var val string
	err := s.db.QueryRow(`SELECT value FROM config WHERE key=?`, key).Scan(&val)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return val, err
}

// HasConfig reports whether a config key exists, including keys whose value is
// intentionally empty. This distinction is needed for settings where empty is
// a valid persisted value (for example, an explicitly unrestricted root list).
func (s *DB) HasConfig(key string) (bool, error) {
	var exists int
	err := s.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM config WHERE key=?)`, key).Scan(&exists)
	return exists != 0, err
}

// SetConfig upserts a config value.
func (s *DB) SetConfig(key, value string) error {
	_, err := s.db.Exec(
		`INSERT INTO config(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`,
		key, value,
	)
	return err
}
