package store

import (
	"database/sql"
	"errors"
	"fmt"
	"sync"
)

// Errors the pairing, registration and login writes return. Callers test with
// errors.Is; none of them wraps a SQL error, so none can be mistaken for one.
var (
	ErrCodeInvalid         = errors.New("pairing code missing, used or expired")
	ErrCodeExists          = errors.New("pairing code hash already stored")
	ErrCodeCannotRegister  = errors.New("pairing code cannot create an account")
	ErrAccountExists       = errors.New("an account already exists")
	ErrUsernameTaken       = errors.New("username already registered")
	ErrLoginAccountChanged = errors.New("account changed during login")
	ErrDeviceNotFound      = errors.New("device not found")
	ErrTooManyPairRequests = errors.New("too many pending pair requests")
	ErrPairRequestGone     = errors.New("pair request not found or already decided")
)

// Pair request statuses. committed: only a hash of the phone's nonce is known
// and the owner is not asked yet; pending: revealed and waiting for the owner;
// approved: decided, token not yet collected; rejected; done: token collected.
const (
	PairCommitted = "committed"
	PairPending   = "pending"
	PairApproved  = "approved"
	PairRejected  = "rejected"
	PairDone      = "done"
)

// pairingSchemaSQL adds the two short-lived pairing tables. They are created on
// first use (the main schema file is not touched) and hold nothing the import
// from the old database needs: codes and requests expire within minutes.
const pairingSchemaSQL = `
CREATE TABLE IF NOT EXISTS pairing_codes (
    code_hash    TEXT PRIMARY KEY,
    expires      INTEGER NOT NULL,
    jail_root    TEXT NOT NULL,
    read_only    INTEGER NOT NULL,
    can_register INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS pair_requests (
    id           TEXT PRIMARY KEY,
    label        TEXT NOT NULL,
    client_id    TEXT NOT NULL,
    public_key   TEXT NOT NULL,
    client_nonce TEXT NOT NULL,
    nonce_commit TEXT NOT NULL,
    remote_ip    TEXT NOT NULL,
    status       TEXT NOT NULL CHECK (status IN ('committed','pending','approved','rejected','done')),
    created      INTEGER NOT NULL,
    expires      INTEGER NOT NULL
);
`

// pairingReady remembers which open databases already have the pairing tables,
// so the CREATE statements run once per database and not per call.
var pairingReady sync.Map

// EnsurePairingSchema creates the pairing tables when they are missing. Every
// pairing method calls it, so calling it first is only a way to fail early.
func (s *DB) EnsurePairingSchema() error {
	if _, ok := pairingReady.Load(s); ok {
		return nil
	}
	if _, err := s.db.Exec(pairingSchemaSQL); err != nil {
		return fmt.Errorf("create pairing tables: %w", err)
	}
	pairingReady.Store(s, struct{}{})
	return nil
}

// PairingCode is one stored one-time code. Only the hash of the code is kept,
// so a copy of the database file does not reveal a code that is still valid.
// JailRoot and ReadOnly are the guest limits the device created with it gets.
type PairingCode struct {
	Hash        string
	Expires     int64
	JailRoot    string
	ReadOnly    bool
	CanRegister bool
}

// AddPairingCode stores a new code after dropping expired ones (now is Unix
// seconds). A hash that is already stored is ErrCodeExists, never replaced.
func (s *DB) AddPairingCode(c PairingCode, now int64) error {
	if err := s.EnsurePairingSchema(); err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM pairing_codes WHERE expires < ?`, now); err != nil {
		return fmt.Errorf("prune pairing codes: %w", err)
	}
	res, err := tx.Exec(`INSERT INTO pairing_codes (code_hash,expires,jail_root,read_only,can_register) VALUES (?,?,?,?,?)
ON CONFLICT(code_hash) DO NOTHING`, c.Hash, c.Expires, c.JailRoot, b2i(c.ReadOnly), b2i(c.CanRegister))
	if err != nil {
		return fmt.Errorf("insert pairing code: %w", err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return ErrCodeExists
	}
	return tx.Commit()
}

// codeInTx reads the live code with this hash inside tx; a missing or expired
// one is ErrCodeInvalid.
func codeInTx(tx *sql.Tx, hash string, now int64) (PairingCode, error) {
	c := PairingCode{Hash: hash}
	var ro, reg int
	err := tx.QueryRow(`SELECT expires,jail_root,read_only,can_register FROM pairing_codes WHERE code_hash=?`, hash).
		Scan(&c.Expires, &c.JailRoot, &ro, &reg)
	if errors.Is(err, sql.ErrNoRows) {
		return PairingCode{}, ErrCodeInvalid
	}
	if err != nil {
		return PairingCode{}, err
	}
	if now > c.Expires {
		return PairingCode{}, ErrCodeInvalid
	}
	c.ReadOnly, c.CanRegister = ro != 0, reg != 0
	return c, nil
}

// spendCode deletes the code inside tx; losing a race for it is ErrCodeInvalid.
func spendCode(tx *sql.Tx, hash string) error {
	res, err := tx.Exec(`DELETE FROM pairing_codes WHERE code_hash=?`, hash)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n != 1 {
		return ErrCodeInvalid
	}
	return nil
}

// PeekPairingCode reports what a code allows without using it up; a missing or
// expired code is ErrCodeInvalid.
func (s *DB) PeekPairingCode(hash string, now int64) (PairingCode, error) {
	if err := s.EnsurePairingSchema(); err != nil {
		return PairingCode{}, err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return PairingCode{}, err
	}
	defer tx.Rollback()
	return codeInTx(tx, hash, now)
}

// DeviceSpec describes the device row a pairing, registration, login or
// approval writes. Now is Unix seconds. ID is used only when a new row is
// inserted. JailRoot and ReadOnly only ever narrow: an empty root or false
// leaves the existing value alone.
type DeviceSpec struct {
	ID        string
	ClientID  string
	Label     string
	TokenHash string
	PublicKey string
	Origin    string
	Username  string
	JailRoot  string
	ReadOnly  bool
	Now       int64
}

// SavedDevice is the outcome of writing a DeviceSpec. Replaced is true when an
// existing row was taken over (revoked, or paired under a different key): such
// a row loses the grants it had.
type SavedDevice struct {
	ID       string
	Replaced bool
}

// grantsBrowseOnly and grantsAllFile are the grant assignments a new or reset
// device gets. They are constants with no caller input, so they are safe to
// splice into SQL.
const (
	grantsBrowseOnly = `can_browse=1,can_download=0,can_upload=0,can_modify=0,can_delete=0,can_share=0`
	grantsAllFile    = `can_browse=1,can_download=1,can_upload=1,can_modify=1,can_delete=1,can_share=1`
	grantsNoApps     = `view_apps=0,launch_apps=0`
)

// deleteShareLinksForDeviceSQL deletes every share link of one device; the
// device id is the only argument.
const deleteShareLinksForDeviceSQL = `DELETE FROM share_links WHERE device_id=?`

// isAdminOrigin reports whether the origin is one that needs an account.
func isAdminOrigin(origin string) bool { return origin == OriginLogin || origin == OriginRegister }

// upsertDeviceTx finds the row for sp.ClientID (the phone's stable id) or
// inserts a new one, inside tx. A code or approval gives browse-only access; a
// login or registration gives the six file grants. Re-pairing a row that was
// revoked or carried another key resets every grant and drops its share links.
func upsertDeviceTx(tx *sql.Tx, sp DeviceSpec) (SavedDevice, error) {
	admin := isAdminOrigin(sp.Origin)
	switch {
	case sp.ID == "" || sp.TokenHash == "":
		return SavedDevice{}, errors.New("device spec needs an id and a token hash")
	case sp.Origin != OriginCode && sp.Origin != OriginApprove && !admin:
		return SavedDevice{}, fmt.Errorf("unknown device origin %q", sp.Origin)
	case admin && sp.Username == "":
		return SavedDevice{}, errors.New("an account device needs a username")
	case !admin && sp.Username != "":
		return SavedDevice{}, errors.New("a code or approval device has no account")
	}
	if sp.ClientID != "" {
		var id, pub, origin string
		var revoked int
		err := tx.QueryRow(`SELECT id,public_key,origin,revoked FROM devices WHERE client_id=?`, sp.ClientID).
			Scan(&id, &pub, &origin, &revoked)
		if err == nil {
			replaced := revoked != 0 || (pub != "" && pub != sp.PublicKey)
			grants, dropShares := "", false
			switch {
			case admin:
				grants = grantsAllFile
			case replaced:
				grants, dropShares = grantsBrowseOnly+","+grantsNoApps, true
			case isAdminOrigin(origin):
				grants, dropShares = grantsBrowseOnly, true
			}
			q := `UPDATE devices SET token_hash=?,label=?,origin=?,username=?,public_key=?,last_seen=?,revoked=0,
jail_root=CASE WHEN ?<>'' THEN ? ELSE jail_root END,
read_only=CASE WHEN ?=1 THEN 1 ELSE read_only END`
			if grants != "" {
				q += "," + grants
			}
			if _, err := tx.Exec(q+` WHERE id=?`, sp.TokenHash, sp.Label, sp.Origin, nullStr(sp.Username), sp.PublicKey,
				sp.Now, sp.JailRoot, sp.JailRoot, b2i(sp.ReadOnly), id); err != nil {
				return SavedDevice{}, fmt.Errorf("update device: %w", err)
			}
			if dropShares {
				if _, err := tx.Exec(deleteShareLinksForDeviceSQL, id); err != nil {
					return SavedDevice{}, fmt.Errorf("drop share links: %w", err)
				}
			}
			return SavedDevice{ID: id, Replaced: replaced}, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return SavedDevice{}, err
		}
	}
	grants := [6]int{1, 0, 0, 0, 0, 0}
	if admin {
		grants = [6]int{1, 1, 1, 1, 1, 1}
	}
	_, err := tx.Exec(`INSERT INTO devices (`+deviceCols+`) VALUES (?,?,?,?,?,?,?,?,?,'','',0,?,?,?,?,?,?,?,?,0,0)`,
		sp.ID, sp.Label, sp.TokenHash, sp.Origin, nullStr(sp.Username), sp.ClientID, sp.PublicKey, sp.Now, sp.Now,
		sp.JailRoot, b2i(sp.ReadOnly), grants[0], grants[1], grants[2], grants[3], grants[4], grants[5])
	if err != nil {
		return SavedDevice{}, fmt.Errorf("insert device: %w", err)
	}
	return SavedDevice{ID: sp.ID}, nil
}

// PairWithCode spends the code and writes the device in one transaction, so a
// failure leaves the code usable and a success leaves no unspent code. The
// code's guest limits are applied to the device. sp.Origin must be OriginCode.
func (s *DB) PairWithCode(codeHash string, sp DeviceSpec) (PairingCode, SavedDevice, error) {
	if err := s.EnsurePairingSchema(); err != nil {
		return PairingCode{}, SavedDevice{}, err
	}
	if sp.Origin != OriginCode {
		return PairingCode{}, SavedDevice{}, errors.New("PairWithCode writes code devices only")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return PairingCode{}, SavedDevice{}, err
	}
	defer tx.Rollback()
	c, err := codeInTx(tx, codeHash, sp.Now)
	if err != nil {
		return PairingCode{}, SavedDevice{}, err
	}
	if err := spendCode(tx, codeHash); err != nil {
		return PairingCode{}, SavedDevice{}, err
	}
	sp.JailRoot, sp.ReadOnly = c.JailRoot, c.ReadOnly
	saved, err := upsertDeviceTx(tx, sp)
	if err != nil {
		return PairingCode{}, SavedDevice{}, err
	}
	return c, saved, tx.Commit()
}

// RegisterWithCode creates the single account and its device and spends an
// owner code, all in one transaction: a code that cannot register, a second
// account or a taken username leaves the code unspent. sp.Origin must be
// OriginRegister.
func (s *DB) RegisterWithCode(codeHash string, sp DeviceSpec, u User) (SavedDevice, error) {
	if err := s.EnsurePairingSchema(); err != nil {
		return SavedDevice{}, err
	}
	if sp.Origin != OriginRegister {
		return SavedDevice{}, errors.New("RegisterWithCode writes register devices only")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return SavedDevice{}, err
	}
	defer tx.Rollback()
	c, err := codeInTx(tx, codeHash, sp.Now)
	if err != nil {
		return SavedDevice{}, err
	}
	if !c.CanRegister {
		return SavedDevice{}, ErrCodeCannotRegister
	}
	var users, taken int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&users); err != nil {
		return SavedDevice{}, err
	}
	if users > 0 {
		return SavedDevice{}, ErrAccountExists
	}
	if err := tx.QueryRow(`SELECT COUNT(*) FROM users WHERE username=?`, u.Username).Scan(&taken); err != nil {
		return SavedDevice{}, err
	}
	if taken > 0 {
		return SavedDevice{}, ErrUsernameTaken
	}
	if _, err := tx.Exec(`INSERT INTO users (username,password_hash,created) VALUES (?,?,?)`, u.Username, u.PasswordHash, sp.Now); err != nil {
		return SavedDevice{}, fmt.Errorf("insert user: %w", err)
	}
	if err := spendCode(tx, codeHash); err != nil {
		return SavedDevice{}, err
	}
	sp.Username = u.Username
	saved, err := upsertDeviceTx(tx, sp)
	if err != nil {
		return SavedDevice{}, err
	}
	return saved, tx.Commit()
}

// LoginDevice writes the device of a successful password login, but only while
// the account still has the password hash the caller verified, so a deleted or
// replaced account cannot be used to mint a token. sp.Origin must be OriginLogin.
func (s *DB) LoginDevice(sp DeviceSpec, passwordHash string) (SavedDevice, error) {
	if sp.Origin != OriginLogin {
		return SavedDevice{}, errors.New("LoginDevice writes login devices only")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return SavedDevice{}, err
	}
	defer tx.Rollback()
	var match int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM users WHERE username=? AND password_hash=?`, sp.Username, passwordHash).Scan(&match); err != nil {
		return SavedDevice{}, err
	}
	if match == 0 {
		return SavedDevice{}, ErrLoginAccountChanged
	}
	saved, err := upsertDeviceTx(tx, sp)
	if err != nil {
		return SavedDevice{}, err
	}
	return saved, tx.Commit()
}

// HasAnyUser reports whether an account exists.
func (s *DB) HasAnyUser() (bool, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n)
	return n > 0, err
}

// UserByUsername returns the account with exactly this user name, or nil, nil.
func (s *DB) UserByUsername(name string) (*User, error) {
	var u User
	err := s.db.QueryRow(`SELECT username,password_hash,created FROM users WHERE username=?`, name).
		Scan(&u.Username, &u.PasswordHash, &u.Created)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// DeviceByID returns the device with this id (revoked ones too), or nil, nil.
func (s *DB) DeviceByID(id string) (*Device, error) {
	return s.deviceWhere(`id=?`, id)
}

// DeviceByClientID returns the device paired under the phone's stable client
// id, or nil, nil (also for an empty id).
func (s *DB) DeviceByClientID(clientID string) (*Device, error) {
	if clientID == "" {
		return nil, nil
	}
	return s.deviceWhere(`client_id=?`, clientID)
}

// deviceWhere selects one device by a fixed condition with one argument.
func (s *DB) deviceWhere(cond, arg string) (*Device, error) {
	d, err := scanDevice(s.db.QueryRow(`SELECT `+deviceCols+` FROM devices WHERE `+cond, arg))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// TouchDevice records that the device was just used, from this address and app
// version. A missing device is ErrDeviceNotFound.
func (s *DB) TouchDevice(id, address, version string, now int64) error {
	res, err := s.db.Exec(`UPDATE devices SET last_seen=?,last_address=?,last_version=? WHERE id=?`, now, address, version, id)
	return oneRow(res, err)
}

// oneRow turns an exec result into ErrDeviceNotFound when no row changed.
func oneRow(res sql.Result, err error) error {
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrDeviceNotFound
	}
	return nil
}

// endDevice runs a statement that ends a device (revoke or delete) and drops
// its share links in the same transaction, so no link outlives the device.
func (s *DB) endDevice(id, stmt string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := oneRow(tx.Exec(stmt, id)); err != nil {
		return err
	}
	if _, err := tx.Exec(deleteShareLinksForDeviceSQL, id); err != nil {
		return err
	}
	return tx.Commit()
}

// RevokeDevice marks the device revoked so its token stops working.
func (s *DB) RevokeDevice(id string) error {
	return s.endDevice(id, `UPDATE devices SET revoked=1 WHERE id=?`)
}

// DeleteDevice removes the device row.
func (s *DB) DeleteDevice(id string) error {
	return s.endDevice(id, `DELETE FROM devices WHERE id=?`)
}

// SetDeviceJail sets the device's path jail; an empty root clears it.
func (s *DB) SetDeviceJail(id, jailRoot string) error {
	res, err := s.db.Exec(`UPDATE devices SET jail_root=? WHERE id=?`, jailRoot, id)
	return oneRow(res, err)
}

// SetDeviceReadOnly sets or clears the device's read-only flag.
func (s *DB) SetDeviceReadOnly(id string, ro bool) error {
	res, err := s.db.Exec(`UPDATE devices SET read_only=? WHERE id=?`, b2i(ro), id)
	return oneRow(res, err)
}

// SetDeviceAppGrants sets the app-catalog and app-launch grants together.
func (s *DB) SetDeviceAppGrants(id string, view, launch bool) error {
	res, err := s.db.Exec(`UPDATE devices SET view_apps=?,launch_apps=? WHERE id=?`, b2i(view), b2i(launch), id)
	return oneRow(res, err)
}

// SetDeviceFileGrants sets the six file grants in order browse, download,
// upload, modify, delete, share; turning share off also drops its share links.
func (s *DB) SetDeviceFileGrants(id string, g [6]bool) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := oneRow(tx.Exec(`UPDATE devices SET can_browse=?,can_download=?,can_upload=?,can_modify=?,can_delete=?,can_share=? WHERE id=?`,
		b2i(g[0]), b2i(g[1]), b2i(g[2]), b2i(g[3]), b2i(g[4]), b2i(g[5]), id)); err != nil {
		return err
	}
	if !g[5] {
		if _, err := tx.Exec(deleteShareLinksForDeviceSQL, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// PairRequest is a phone's request to be paired, waiting for the owner.
// ClientNonce is empty until the phone reveals it; NonceCommit and ClientNonce
// are stored in lowercase hex.
type PairRequest struct {
	ID, Label, ClientID, PublicKey string
	ClientNonce, NonceCommit       string
	RemoteIP, Status               string
	Created, Expires               int64
}

// PairRequestLimits bound how many requests can wait at once, so a stranger
// cannot bury the owner in prompts: undecided (owner is asked), and committed
// but unrevealed (owner not asked yet) per source address and in total.
// RetentionSecs is how long rejected and collected requests are kept.
type PairRequestLimits struct {
	MaxPending, MaxCommittedPerIP, MaxCommitted int
	RetentionSecs                               int64
}

const pairRequestCols = `id,label,client_id,public_key,client_nonce,nonce_commit,remote_ip,status,created,expires`

// scanPairRequest reads one pair_requests row selected with pairRequestCols.
func scanPairRequest(sc interface{ Scan(...any) error }) (PairRequest, error) {
	var r PairRequest
	err := sc.Scan(&r.ID, &r.Label, &r.ClientID, &r.PublicKey, &r.ClientNonce, &r.NonceCommit, &r.RemoteIP, &r.Status, &r.Created, &r.Expires)
	return r, err
}

// CreatePairRequest stores a new committed request after pruning expired and
// old finished ones; over a limit it returns ErrTooManyPairRequests. The
// request's Status, ClientNonce are ignored (committed, empty).
func (s *DB) CreatePairRequest(r PairRequest, now int64, lim PairRequestLimits) error {
	if err := s.EnsurePairingSchema(); err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM pair_requests WHERE expires < ? OR (status IN (?,?) AND created < ?)`,
		now, PairRejected, PairDone, now-lim.RetentionSecs); err != nil {
		return err
	}
	var pending, committed, fromIP int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM pair_requests WHERE status=? AND expires >= ?`, PairPending, now).Scan(&pending); err != nil {
		return err
	}
	if err := tx.QueryRow(`SELECT COUNT(*), COALESCE(SUM(remote_ip = ?), 0) FROM pair_requests WHERE status=? AND expires >= ?`,
		r.RemoteIP, PairCommitted, now).Scan(&committed, &fromIP); err != nil {
		return err
	}
	if pending >= lim.MaxPending || fromIP >= lim.MaxCommittedPerIP || committed >= lim.MaxCommitted {
		return ErrTooManyPairRequests
	}
	if _, err := tx.Exec(`INSERT INTO pair_requests (`+pairRequestCols+`) VALUES (?,?,?,?,?,?,?,?,?,?)`,
		r.ID, r.Label, r.ClientID, r.PublicKey, "", r.NonceCommit, r.RemoteIP, PairCommitted, r.Created, r.Expires); err != nil {
		return err
	}
	return tx.Commit()
}

// GetPairRequest returns the request with this id; ok is false when it is
// unknown or ran out of time before being decided or collected.
func (s *DB) GetPairRequest(id string, now int64) (PairRequest, bool, error) {
	if err := s.EnsurePairingSchema(); err != nil {
		return PairRequest{}, false, err
	}
	r, err := scanPairRequest(s.db.QueryRow(`SELECT `+pairRequestCols+` FROM pair_requests WHERE id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return PairRequest{}, false, nil
	}
	if err != nil {
		return PairRequest{}, false, err
	}
	live := r.Status == PairCommitted || r.Status == PairPending || r.Status == PairApproved
	if live && r.Expires < now {
		return PairRequest{}, false, nil
	}
	return r, true, nil
}

// ListPendingPairRequests returns the revealed, undecided, unexpired requests,
// oldest first.
func (s *DB) ListPendingPairRequests(now int64) ([]PairRequest, error) {
	if err := s.EnsurePairingSchema(); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(`SELECT `+pairRequestCols+` FROM pair_requests WHERE status=? AND expires >= ? ORDER BY created,id`, PairPending, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PairRequest
	for rows.Next() {
		r, err := scanPairRequest(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// DecidePairRequest approves or rejects a pending, unexpired request; any other
// request is ErrPairRequestGone.
func (s *DB) DecidePairRequest(id string, approve bool, now int64) error {
	if err := s.EnsurePairingSchema(); err != nil {
		return err
	}
	status := PairRejected
	if approve {
		status = PairApproved
	}
	res, err := s.db.Exec(`UPDATE pair_requests SET status=? WHERE id=? AND status=? AND expires >= ?`, status, id, PairPending, now)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return ErrPairRequestGone
	}
	return nil
}

// RevealPairRequest records the phone's nonce on a committed, unexpired request
// and makes it pending; it succeeds once. The caller has already checked the
// nonce against the commitment. At maxPending undecided requests it returns
// ErrTooManyPairRequests.
func (s *DB) RevealPairRequest(id, clientNonce string, now int64, maxPending int) (PairRequest, error) {
	if err := s.EnsurePairingSchema(); err != nil {
		return PairRequest{}, err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return PairRequest{}, err
	}
	defer tx.Rollback()
	var pending int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM pair_requests WHERE status=? AND expires >= ?`, PairPending, now).Scan(&pending); err != nil {
		return PairRequest{}, err
	}
	if pending >= maxPending {
		return PairRequest{}, ErrTooManyPairRequests
	}
	res, err := tx.Exec(`UPDATE pair_requests SET client_nonce=?,status=? WHERE id=? AND status=? AND expires >= ?`,
		clientNonce, PairPending, id, PairCommitted, now)
	if err != nil {
		return PairRequest{}, err
	}
	if n, err := res.RowsAffected(); err != nil {
		return PairRequest{}, err
	} else if n == 0 {
		return PairRequest{}, ErrPairRequestGone
	}
	r, err := scanPairRequest(tx.QueryRow(`SELECT `+pairRequestCols+` FROM pair_requests WHERE id=?`, id))
	if err != nil {
		return PairRequest{}, err
	}
	return r, tx.Commit()
}

// CompleteApprovedPairRequest collects an approved request exactly once and
// writes its device in the same transaction, so a failure leaves the request
// approved for the next poll instead of losing it. The device's client id,
// label and key come from the request; sp supplies the id, token hash and
// time. ok is false when the request is not approved and unexpired.
func (s *DB) CompleteApprovedPairRequest(id string, sp DeviceSpec) (PairRequest, SavedDevice, bool, error) {
	if err := s.EnsurePairingSchema(); err != nil {
		return PairRequest{}, SavedDevice{}, false, err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return PairRequest{}, SavedDevice{}, false, err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`UPDATE pair_requests SET status=? WHERE id=? AND status=? AND expires >= ?`, PairDone, id, PairApproved, sp.Now)
	if err != nil {
		return PairRequest{}, SavedDevice{}, false, err
	}
	if n, err := res.RowsAffected(); err != nil {
		return PairRequest{}, SavedDevice{}, false, err
	} else if n == 0 {
		return PairRequest{}, SavedDevice{}, false, nil
	}
	r, err := scanPairRequest(tx.QueryRow(`SELECT `+pairRequestCols+` FROM pair_requests WHERE id=?`, id))
	if err != nil {
		return PairRequest{}, SavedDevice{}, false, err
	}
	sp.ClientID, sp.Label, sp.PublicKey, sp.Origin, sp.Username = r.ClientID, r.Label, r.PublicKey, OriginApprove, ""
	saved, err := upsertDeviceTx(tx, sp)
	if err != nil {
		return PairRequest{}, SavedDevice{}, false, err
	}
	return r, saved, true, tx.Commit()
}

// DeviceUpdate is a set of changes to one device applied together; a nil field
// is left alone. File is browse, download, upload, modify, delete, share; App is
// view, launch.
type DeviceUpdate struct {
	Jail     *string
	ReadOnly *bool
	File     *[6]bool
	App      *[2]bool
}

// UpdateDevice applies every change in u to the device in one transaction, so a
// storage failure leaves the device exactly as it was. Turning share off drops
// the device's share links. An unknown device is ErrDeviceNotFound.
func (s *DB) UpdateDevice(id string, u DeviceUpdate) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := oneRow(tx.Exec(`UPDATE devices SET id=id WHERE id=?`, id)); err != nil {
		return err
	}
	if u.Jail != nil {
		if _, err := tx.Exec(`UPDATE devices SET jail_root=? WHERE id=?`, *u.Jail, id); err != nil {
			return err
		}
	}
	if u.ReadOnly != nil {
		if _, err := tx.Exec(`UPDATE devices SET read_only=? WHERE id=?`, b2i(*u.ReadOnly), id); err != nil {
			return err
		}
	}
	if g := u.File; g != nil {
		if _, err := tx.Exec(`UPDATE devices SET can_browse=?,can_download=?,can_upload=?,can_modify=?,can_delete=?,can_share=? WHERE id=?`,
			b2i(g[0]), b2i(g[1]), b2i(g[2]), b2i(g[3]), b2i(g[4]), b2i(g[5]), id); err != nil {
			return err
		}
		if !g[5] {
			if _, err := tx.Exec(deleteShareLinksForDeviceSQL, id); err != nil {
				return err
			}
		}
	}
	if a := u.App; a != nil {
		if _, err := tx.Exec(`UPDATE devices SET view_apps=?,launch_apps=? WHERE id=?`, b2i(a[0]), b2i(a[1]), id); err != nil {
			return err
		}
	}
	return tx.Commit()
}
