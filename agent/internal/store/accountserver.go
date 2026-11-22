package store

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"database/sql"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/security"
)

// --------- account server ---------
//
// An agent may opt in to the account-server role: it signs login assertions with its own Ed25519 key,
// kept in its own file beside agent.db (never the TLS key). A member agent pins that server's public key
// once (trusted_account_server) and then accepts its assertions at POST /v1/login. See
// internal/security/assertion.go for the assertion format.

// AccountKeyFile is the signing key's file name in the data directory (PKCS#8 PEM, mode 0600).
const AccountKeyFile = "account-server.key"

// Config keys of the account-server role.
const (
	configAccountServerEnabled = "account_server_enabled"
	configAccountServerName    = "account_server_name"
)

// ErrAccountServerOff is returned when the signing key is requested while the role is disabled.
var ErrAccountServerOff = errors.New("account server role is off")

// ErrDeviceRevoked is returned by LoginDeviceFederatedAtEpoch when the device's row on this computer is
// revoked: an account-server assertion must not undo a local revoke.
var ErrDeviceRevoked = errors.New("device is revoked on this computer")

// ErrNoTrustedAccountServer is returned when a federated login names an issuer that is not the one
// currently pinned.
var ErrNoTrustedAccountServer = errors.New("issuer is not the trusted account server")

// accountKeyMu serialises creation of the key file inside this process; a second process (the admin CLI)
// is handled by creating the file with a link that fails if it already exists.
var accountKeyMu sync.Mutex

// AccountServer is this agent's own account-server role settings.
type AccountServer struct {
	Enabled bool
	// Name is the display name peers see; empty means "use the agent's own name".
	Name string
}

// AccountServer returns the role settings (disabled by default).
func (s *DB) AccountServer() (AccountServer, error) {
	enabled, err := s.GetConfig(configAccountServerEnabled)
	if err != nil {
		return AccountServer{}, err
	}
	name, err := s.GetConfig(configAccountServerName)
	if err != nil {
		return AccountServer{}, err
	}
	return AccountServer{Enabled: enabled == "1", Name: name}, nil
}

// SetAccountServer turns the role on or off and sets its display name. Enabling first makes sure the
// signing key exists, so a failure to create it leaves the role off. Disabling keeps the key, so peers
// that pinned it keep working when the role is switched back on.
func (s *DB) SetAccountServer(enabled bool, name string) error {
	if enabled {
		if _, err := s.loadOrCreateAccountKey(); err != nil {
			return err
		}
	}
	flag := ""
	if enabled {
		flag = "1"
	}
	if err := s.SetConfig(configAccountServerName, name); err != nil {
		return err
	}
	return s.SetConfig(configAccountServerEnabled, flag)
}

// AccountSigningKey returns the account-server private key, or ErrAccountServerOff while the role is off.
func (s *DB) AccountSigningKey() (ed25519.PrivateKey, error) {
	cfg, err := s.AccountServer()
	if err != nil {
		return nil, err
	}
	if !cfg.Enabled {
		return nil, ErrAccountServerOff
	}
	return s.loadOrCreateAccountKey()
}

// loadOrCreateAccountKey reads the key file, creating it (mode 0600, written to a temp file and linked
// into place so a half-written key is never visible) when it does not exist yet.
func (s *DB) loadOrCreateAccountKey() (ed25519.PrivateKey, error) {
	accountKeyMu.Lock()
	defer accountKeyMu.Unlock()
	path := filepath.Join(s.dir, AccountKeyFile)
	key, err := readAccountKey(path)
	if err == nil {
		return key, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	priv, tmpName, err := newAccountKeyTemp(s.dir)
	if err != nil {
		return nil, err
	}
	defer os.Remove(tmpName)
	if err := os.Link(tmpName, path); err != nil {
		if errors.Is(err, fs.ErrExist) { // another process created it first
			return readAccountKey(path)
		}
		return nil, fmt.Errorf("install account key: %w", err)
	}
	return priv, nil
}

// newAccountKeyTemp generates an Ed25519 key and writes it as PKCS#8 PEM to a fresh temp file in dir
// (created mode 0600, synced), returning the key and the file's name. The caller installs or removes the file.
func newAccountKeyTemp(dir string) (ed25519.PrivateKey, string, error) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, "", fmt.Errorf("generate account key: %w", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return nil, "", err
	}
	tmp, err := os.CreateTemp(dir, ".account-key-*") // created 0600
	if err != nil {
		return nil, "", fmt.Errorf("create account key: %w", err)
	}
	_, werr := tmp.Write(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
	if serr := tmp.Sync(); werr == nil {
		werr = serr
	}
	if cerr := tmp.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		_ = os.Remove(tmp.Name())
		return nil, "", fmt.Errorf("write account key: %w", werr)
	}
	return priv, tmp.Name(), nil
}

// RotateAccountKey replaces the signing key with a new one, written to a temp file (mode 0600) and renamed
// over the old file so a reader sees either whole key. An existing, readable key is required; the role's
// enabled state and name are untouched. Every member that pinned the old key must re-pin the new one.
func (s *DB) RotateAccountKey() (ed25519.PrivateKey, error) {
	accountKeyMu.Lock()
	defer accountKeyMu.Unlock()
	path := filepath.Join(s.dir, AccountKeyFile)
	if _, err := readAccountKey(path); err != nil {
		return nil, fmt.Errorf("no usable account key to rotate: %w", err)
	}
	priv, tmpName, err := newAccountKeyTemp(s.dir)
	if err != nil {
		return nil, err
	}
	defer os.Remove(tmpName)
	if err := os.Rename(tmpName, path); err != nil {
		return nil, fmt.Errorf("install rotated account key: %w", err)
	}
	return priv, nil
}

// readAccountKey parses the PEM key file. On Unix it refuses a file other users can read.
func readAccountKey(path string) (ed25519.PrivateKey, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("account key %s is readable by other users (mode %v); chmod 600 it", path, info.Mode().Perm())
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(raw)
	if block == nil {
		return nil, fmt.Errorf("account key %s is not PEM", path)
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("account key %s: %w", path, err)
	}
	key, ok := parsed.(ed25519.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("account key %s is not Ed25519", path)
	}
	return key, nil
}

// TrustedAccountServer is the one account server this agent accepts assertions from.
type TrustedAccountServer struct {
	Name        string
	PublicKey   string // standard base64 of the raw 32-byte Ed25519 public key
	Fingerprint string // security.KeyFingerprint of PublicKey
	Created     time.Time
	// HighestEpoch is the highest account epoch seen in a login from this server; an assertion with a lower
	// one is refused, and a higher one ends every session minted under a lower one.
	HighestEpoch int64
}

// SetTrustedAccountServer pins the server's public key (standard base64), replacing any earlier pin.
// Sessions minted from a different key are revoked, and the share links they made deleted, in the same
// transaction; re-pinning the same key leaves them (and the highest epoch seen) alone, a different key starts
// the epoch count again at 0.
func (s *DB) SetTrustedAccountServer(name, publicKeyB64 string) (TrustedAccountServer, error) {
	raw, err := base64.StdEncoding.DecodeString(publicKeyB64)
	if err != nil || len(raw) != ed25519.PublicKeySize {
		return TrustedAccountServer{}, errors.New("account server key must be a base64 Ed25519 public key")
	}
	name = strings.TrimSpace(name)
	fp := security.KeyFingerprint(ed25519.PublicKey(raw))
	now := time.Now().Unix()
	tx, err := s.db.Begin()
	if err != nil {
		return TrustedAccountServer{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`INSERT INTO trusted_account_server (id,name,public_key,fingerprint,created) VALUES (1,?,?,?,?)
ON CONFLICT(id) DO UPDATE SET name=excluded.name, public_key=excluded.public_key,
highest_epoch=CASE WHEN trusted_account_server.fingerprint=excluded.fingerprint THEN trusted_account_server.highest_epoch ELSE 0 END,
fingerprint=excluded.fingerprint, created=excluded.created`, name, publicKeyB64, fp, now); err != nil {
		return TrustedAccountServer{}, err
	}
	var highest int64
	if err := tx.QueryRow(`SELECT highest_epoch FROM trusted_account_server WHERE id=1`).Scan(&highest); err != nil {
		return TrustedAccountServer{}, err
	}
	// Drop the share links of the sessions being revoked in the same transaction, over exactly the same devices.
	if _, err := tx.Exec(`DELETE FROM share_tokens WHERE device_id IN (SELECT id FROM devices WHERE login_issuer<>'' AND login_issuer<>?)`, fp); err != nil {
		return TrustedAccountServer{}, err
	}
	if _, err := tx.Exec(`UPDATE devices SET revoked=1 WHERE login_issuer<>'' AND login_issuer<>?`, fp); err != nil {
		return TrustedAccountServer{}, err
	}
	if err := tx.Commit(); err != nil {
		return TrustedAccountServer{}, err
	}
	return TrustedAccountServer{Name: name, PublicKey: publicKeyB64, Fingerprint: fp, Created: time.Unix(now, 0), HighestEpoch: highest}, nil
}

// TrustedAccountServer returns the pinned account server, or (nil,nil) when none is pinned.
func (s *DB) TrustedAccountServer() (*TrustedAccountServer, error) {
	var t TrustedAccountServer
	var created int64
	err := s.db.QueryRow(`SELECT name,public_key,fingerprint,created,highest_epoch FROM trusted_account_server WHERE id=1`).
		Scan(&t.Name, &t.PublicKey, &t.Fingerprint, &created, &t.HighestEpoch)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	t.Created = time.Unix(created, 0)
	return &t, nil
}

// RemoveTrustedAccountServer forgets the pinned server, revokes every session it vouched for and deletes the
// share links those sessions made, all in one transaction.
func (s *DB) RemoveTrustedAccountServer() error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`DELETE FROM trusted_account_server WHERE id=1`); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM share_tokens WHERE device_id IN (SELECT id FROM devices WHERE login_issuer<>'')`); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE devices SET revoked=1 WHERE login_issuer<>''`); err != nil {
		return err
	}
	return tx.Commit()
}

// existingDeviceForFederatedLogin looks up the device row with this client id (if any) and reports its id,
// whether it is revoked in a way an assertion must not undo and whether it is a local (non-federated, no
// account-server issuer) device. That is a revoked local device, or a federated one the owner revoked
// (revoked_by_owner); a federated row revoked only by expiry is not reported: a fresh assertion is exactly
// what renews it. An empty clientID or no matching row returns zero values.
func existingDeviceForFederatedLogin(tx *sql.Tx, clientID string) (id string, revoked, local bool, err error) {
	if clientID == "" {
		return "", false, false, nil
	}
	var rev, byOwner int
	var issuer string
	err = tx.QueryRow(`SELECT id, revoked, revoked_by_owner, login_issuer FROM devices WHERE client_id=?`, clientID).Scan(&id, &rev, &byOwner, &issuer)
	if err == sql.ErrNoRows {
		return "", false, false, nil
	}
	if err != nil {
		return "", false, false, err
	}
	return id, rev == 1 && (issuer == "" || byOwner == 1), issuer == "", nil
}

// LoginDeviceFederated is LoginDeviceFederatedAtEpoch for an assertion that carries no epoch (epoch 0).
func (s *DB) LoginDeviceFederated(clientID, label, token, publicKey, username, issuerFingerprint string) (string, error) {
	return s.LoginDeviceFederatedAtEpoch(clientID, label, token, publicKey, username, issuerFingerprint, 0)
}

// LoginDeviceFederatedAtEpoch records a login an account server vouched for: a device stamped with the
// server's account name, the issuer key fingerprint, the login time (every fresh assertion renews the session
// for FederatedSessionTTL) and the account epoch it was minted under. The device is never an owner
// (via_login stays 0); it gets FederatedGrantProfile. No local account or password hash is involved.
// issuerFingerprint must be the currently pinned server's, checked in the same transaction. An epoch lower
// than the highest seen from that server returns ErrStaleAccountEpoch and changes nothing; a higher one is
// remembered and, in the same transaction, deletes every federated device (and its share links) of that
// server minted under a lower epoch, because the issuer has revoked access since. A revoked local (code- or owner-paired) device row
// or a federated device the owner revoked is refused with ErrDeviceRevoked and left untouched; an account on the
// deny list is refused with ErrAccountDenied before anything is written; a code- or owner-paired row loses its app, wake and
// file grants (resetDeviceAccessTx) when the login takes it over.
func (s *DB) LoginDeviceFederatedAtEpoch(clientID, label, token, publicKey, username, issuerFingerprint string, epoch int64) (string, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback() }()

	var highest int64
	err = tx.QueryRow(`SELECT highest_epoch FROM trusted_account_server WHERE fingerprint=?`, issuerFingerprint).Scan(&highest)
	if err == sql.ErrNoRows || (err == nil && issuerFingerprint == "") {
		return "", ErrNoTrustedAccountServer
	}
	if err != nil {
		return "", err
	}
	if epoch < highest {
		return "", ErrStaleAccountEpoch
	}
	if denied, err := accountDeniedTx(tx, issuerFingerprint, username); err != nil {
		return "", err
	} else if denied {
		return "", ErrAccountDenied
	}
	// A row already on this computer under clientID decides two things: a revoked local one is never revived by an
	// assertion (only the owner's approval on the PC does that), and one that was code- or owner-paired
	// (no issuer) is reset below so its app and wake rights do not carry into the federated session.
	existingID, wasRevoked, wasLocal, err := existingDeviceForFederatedLogin(tx, clientID)
	if err != nil {
		return "", err
	}
	if wasRevoked {
		return "", ErrDeviceRevoked
	}
	ended := int64(-1)
	if epoch > highest {
		if _, err := tx.Exec(`DELETE FROM share_tokens WHERE device_id IN (SELECT id FROM devices WHERE login_issuer=? AND federated_epoch<?)`, issuerFingerprint, epoch); err != nil {
			return "", err
		}
		res, err := tx.Exec(`DELETE FROM devices WHERE login_issuer=? AND federated_epoch<?`, issuerFingerprint, epoch)
		if err != nil {
			return "", err
		}
		if ended, err = res.RowsAffected(); err != nil {
			return "", err
		}
		if _, err := tx.Exec(`UPDATE trusted_account_server SET highest_epoch=? WHERE fingerprint=?`, epoch, issuerFingerprint); err != nil {
			return "", err
		}
	}
	deviceID, err := upsertDeviceTx(tx, clientID, label, token, publicKey, false)
	if err != nil {
		return "", err
	}
	if wasLocal && existingID == deviceID {
		if err := resetDeviceAccessTx(tx, deviceID); err != nil {
			return "", err
		}
	}
	browse, download, upload, modify, del, share := FederatedGrantProfile()
	if _, err := tx.Exec(`UPDATE devices SET username=?, login_issuer=?, federated_login_at=?, federated_epoch=?,
can_browse=?, can_download=?, can_upload=?, can_modify=?, can_delete=?, can_share=?, can_wake=0 WHERE id=?`,
		username, issuerFingerprint, time.Now().Unix(), epoch,
		boolInt(browse), boolInt(download), boolInt(upload), boolInt(modify), boolInt(del), boolInt(share), deviceID); err != nil {
		return "", err
	}
	if !share {
		if _, err := tx.Exec(`DELETE FROM share_tokens WHERE device_id=?`, deviceID); err != nil {
			return "", err
		}
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	if ended >= 0 {
		fp := issuerFingerprint
		if len(fp) > AuditFingerprintLen {
			fp = fp[:AuditFingerprintLen]
		}
		_ = s.AppendAudit(AuditAccountServer, username+"@"+fp, "", fmt.Sprintf("issuer account epoch %d seen: ended %d older federated sessions", epoch, ended))
	}
	return deviceID, nil
}

// DeviceAccount returns the account name stamped on a device and the account-server key fingerprint that
// vouched for it (empty for a password or code device). ("","",nil) when the device does not exist.
func (s *DB) DeviceAccount(id string) (username, issuer string, err error) {
	err = s.db.QueryRow(`SELECT username,login_issuer FROM devices WHERE id=?`, id).Scan(&username, &issuer)
	if err == sql.ErrNoRows {
		return "", "", nil
	}
	return username, issuer, err
}
