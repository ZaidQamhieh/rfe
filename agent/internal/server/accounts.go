// Package server — optional account-server role.
//
// An agent can opt in (default off) to signing short-lived login assertions with its own Ed25519 key, and
// a member agent can pin one such server's key and accept its assertions at POST /v1/login instead of a
// password. Passwords never leave the account server. See docs/account-server-design.md.
package server

import (
	"crypto/ed25519"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/ZaidQamhieh/rfe/agent/internal/security"
	"github.com/ZaidQamhieh/rfe/agent/internal/store"
)

const (
	// assertionIssueTTL is how long an issued assertion lives. A member consumes it within seconds of
	// receiving it; the hard cap (security.AssertionMaxTTL) is enforced by verifiers.
	assertionIssueTTL = 2 * time.Minute
	// assertionIssueRate caps assertions one device may request per minute.
	assertionIssueRate = 30

	maxAccountNameLen    = 64
	maxAssertionSubject  = 128
	maxAssertionNonceLen = 256
	accountServerOffMsg  = "this agent is not an account server"
	accountDisabledCode  = "ACCOUNT_SERVER_DISABLED"
	// federatedActorFpLen is how many hex characters of the issuer fingerprint the audit actor carries.
	federatedActorFpLen   = store.AuditFingerprintLen
	federatedForbiddenMsg = "a session from an account server cannot do this; use the PC or a password session"
)

// constEq compares two secrets-or-claims in constant time (length differences are not hidden; the
// compared values are fixed-length hex or base64 in every caller).
func constEq(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// accountIdentity is this agent's account-server role as it stands now.
type accountIdentity struct {
	enabled bool
	name    string
	pubB64  string
	fp      string
}

// loadAccountIdentity reads the role settings and, when the role is on, the public half of its key.
func loadAccountIdentity(cfg Config, db *store.DB) (accountIdentity, ed25519.PrivateKey, error) {
	as, err := db.AccountServer()
	if err != nil || !as.Enabled {
		return accountIdentity{}, nil, err
	}
	priv, err := db.AccountSigningKey()
	if err != nil {
		return accountIdentity{}, nil, err
	}
	pub := priv.Public().(ed25519.PublicKey)
	name := as.Name
	if name == "" && cfg.Settings != nil {
		name = cfg.Settings.AgentName()
	}
	return accountIdentity{
		enabled: true, name: name,
		pubB64: base64.StdEncoding.EncodeToString(pub), fp: security.KeyFingerprint(pub),
	}, priv, nil
}

// --------- GET /accounts/key ---------

func accountKeyHandler(cfg Config, db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, _, err := loadAccountIdentity(cfg, db)
		if err != nil {
			writeInternal(w, "account key", err)
			return
		}
		if !id.enabled {
			writeError(w, http.StatusForbidden, accountDisabledCode, accountServerOffMsg)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"name": id.name, "publicKey": id.pubB64, "keyFingerprint": id.fp, "algorithm": "Ed25519",
		})
	}
}

// --------- POST /accounts/assertion ---------

type assertionRequest struct {
	Audience        string `json:"audience"`
	DevicePublicKey string `json:"devicePublicKey"`
	MemberNonce     string `json:"memberNonce"`
}

// assertionHandler signs a login assertion for the account the calling (password-authenticated) device
// belongs to. Must run behind authMiddleware and adminOnly, so a code-paired device never gets here.
func assertionHandler(cfg Config, db *store.DB) http.HandlerFunc {
	limiter := newKeyedLimiter(assertionIssueRate, time.Minute)
	return func(w http.ResponseWriter, r *http.Request) {
		dev := deviceFromContext(r)
		if forbidUnlessAdmin(w, dev) {
			return
		}
		id, priv, err := loadAccountIdentity(cfg, db)
		if err != nil {
			writeInternal(w, "account assertion", err)
			return
		}
		if !id.enabled {
			writeError(w, http.StatusForbidden, accountDisabledCode, accountServerOffMsg)
			return
		}
		if !limiter.Allow(dev.ID) {
			writeError(w, http.StatusTooManyRequests, "RATE_LIMITED", "too many assertion requests, try again later")
			return
		}
		var req assertionRequest
		if !decodeJSONBody(w, r, &req) {
			return
		}
		aud := strings.ToLower(req.Audience)
		if raw, err := hex.DecodeString(aud); err != nil || len(raw) != 32 {
			writeError(w, http.StatusBadRequest, codeBadRequest, "audience must be the member agent's certificate fingerprint (64 hex characters)")
			return
		}
		reqKey, err := base64.StdEncoding.DecodeString(req.DevicePublicKey)
		if err != nil || len(reqKey) != ed25519.PublicKeySize {
			writeError(w, http.StatusBadRequest, codeBadRequest, "devicePublicKey must be a base64 Ed25519 public key")
			return
		}
		// An assertion binds a device key to the account, so it is only issued for the key this very device
		// proved at login: otherwise any session could mint a login for a key it does not hold.
		pinnedKey, perr := base64.StdEncoding.DecodeString(dev.PublicKey)
		if perr != nil || len(pinnedKey) != ed25519.PublicKeySize || subtle.ConstantTimeCompare(pinnedKey, reqKey) != 1 {
			writeError(w, http.StatusForbidden, "FORBIDDEN", "devicePublicKey must be the calling device's own key")
			return
		}
		if req.MemberNonce == "" || len(req.MemberNonce) > maxAssertionNonceLen {
			writeError(w, http.StatusBadRequest, codeBadRequest, "memberNonce required")
			return
		}
		// The subject is the account this device logged in with here. A device that an account server
		// vouched for holds no account of its own on this agent, so it cannot vouch for one.
		username, issuer, err := db.DeviceAccount(dev.ID)
		if err != nil {
			writeInternal(w, "account assertion", err)
			return
		}
		if username == "" || issuer != "" {
			writeError(w, http.StatusForbidden, "FORBIDDEN", "a password-authenticated session is required")
			return
		}
		// The account must still exist, and the assertion carries the epoch at signing time so a member can
		// drop sessions minted before a revocation. A deletion that lands inside the assertion's lifetime
		// (at most assertionIssueTTL) is caught by the epoch bump the deletion itself makes.
		epoch, found, err := db.AccountEpochFor(username)
		if err != nil {
			writeInternal(w, "account assertion", err)
			return
		}
		if !found {
			writeError(w, http.StatusForbidden, "FORBIDDEN", "the account no longer exists")
			return
		}
		exp := time.Now().Add(assertionIssueTTL).Unix()
		token, err := security.SignAssertion(priv, security.AssertionClaims{
			Iss: id.fp, Sub: username, Aud: aud, DevicePublicKey: req.DevicePublicKey, Nonce: req.MemberNonce, Exp: exp, Epoch: epoch,
		})
		if err != nil {
			writeInternal(w, "account assertion", err)
			return
		}
		// Never the assertion itself: it is a bearer credential for the next two minutes.
		audit(db, r, store.AuditAccountServer, username, "assertion issued for agent "+aud[:12])
		writeJSON(w, http.StatusOK, map[string]any{"assertion": token, "exp": exp})
	}
}

// --------- federated login (POST /login with an assertion) ---------

// validAssertionSubject reports whether s is usable as an account name copied onto a device row and
// into the audit log.
func validAssertionSubject(s string) bool {
	if s == "" || len(s) > maxAssertionSubject || !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

// federatedLogin completes POST /login for a request that carries an assertion. The caller has already
// applied the per-address rate limit. Every rejection is the same 401 INVALID_CREDENTIALS and an audit
// row that never contains the assertion.
func federatedLogin(w http.ResponseWriter, r *http.Request, cfg Config, db *store.DB, nonces *nonceStore, req loginRequest) {
	reject := func(actor, why string) {
		auditAs(db, actor, store.AuditLoginFailed, "", "account server assertion rejected ("+why+") from "+clientIP(r))
		writeError(w, http.StatusUnauthorized, "INVALID_CREDENTIALS", "invalid assertion")
	}
	trusted, err := db.TrustedAccountServer()
	if err != nil {
		writeInternal(w, "login", err)
		return
	}
	if trusted == nil {
		reject("", "no account server trusted")
		return
	}
	// fedActor is how this login is named in the audit log: the account plus the issuing key's fingerprint, so
	// two account servers' "alice" cannot be mistaken for each other.
	fedActor := func(sub string) string { return federatedActor(sub, trusted.Fingerprint) }
	raw, err := base64.StdEncoding.DecodeString(trusted.PublicKey)
	if err != nil || len(raw) != ed25519.PublicKeySize {
		writeInternal(w, "login", errors.New("stored account server key is malformed"))
		return
	}
	claims, err := security.VerifyAssertionSignature(ed25519.PublicKey(raw), req.Assertion)
	if err != nil {
		reject("", "bad signature")
		return
	}
	ownFp := strings.ToLower(cfg.CertFingerprint)
	switch {
	case !constEq(claims.Iss, trusted.Fingerprint):
		reject("", "wrong issuer")
		return
	case ownFp == "" || !constEq(claims.Aud, ownFp):
		reject("", "wrong audience")
		return
	}
	now := time.Now()
	exp := time.Unix(claims.Exp, 0)
	if exp.Before(now.Add(-security.AssertionClockSkew)) || exp.After(now.Add(security.AssertionMaxTTL+security.AssertionClockSkew)) {
		reject("", "expired or not yet valid")
		return
	}
	if !validAssertionSubject(claims.Sub) {
		reject("", "bad subject")
		return
	}
	if req.DevicePublicKey == "" || req.Nonce == "" ||
		!constEq(claims.DevicePublicKey, req.DevicePublicKey) || !constEq(claims.Nonce, req.Nonce) {
		reject(fedActor(claims.Sub), "assertion is for another device or challenge")
		return
	}
	// Proves the device holds the key the assertion names, and consumes the nonce: a second use of the
	// same assertion finds it gone.
	if err := verifyDeviceProof(db, nonces, cfg.CertFingerprint, req.DeviceID, req.DevicePublicKey, req.Nonce, req.Signature, w, rejectOnKeyChange); err != nil {
		// verifyDeviceProof already wrote the error response; record the failure like every other rejection.
		auditAs(db, fedActor(claims.Sub), store.AuditLoginFailed, "", "account server assertion rejected (device proof failed) from "+clientIP(r))
		return
	}
	if req.DeviceLabel == "" {
		req.DeviceLabel = unnamedDeviceLabel
	}
	token, err := randomToken(tokenBytes)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL", tokenGenerateFailedMsg)
		return
	}
	deviceID, err := db.LoginDeviceFederatedAtEpoch(req.DeviceID, req.DeviceLabel, token, req.DevicePublicKey, claims.Sub, trusted.Fingerprint, claims.Epoch)
	if err != nil {
		if errors.Is(err, store.ErrStaleAccountEpoch) {
			reject(fedActor(claims.Sub), "assertion older than the issuer's current account epoch")
			return
		}
		if errors.Is(err, store.ErrNoTrustedAccountServer) {
			reject(fedActor(claims.Sub), "account server no longer trusted")
			return
		}
		if errors.Is(err, store.ErrAccountDenied) {
			auditAs(db, fedActor(claims.Sub), store.AuditLoginFailed, "", "account server assertion refused (account is on the deny list) from "+clientIP(r))
			writeError(w, http.StatusForbidden, "FORBIDDEN", deniedAccountLoginMsg)
			return
		}
		if errors.Is(err, store.ErrDeviceRevoked) {
			auditAs(db, fedActor(claims.Sub), store.AuditLoginFailed, "", "account server assertion refused (device is revoked) from "+clientIP(r))
			writeError(w, http.StatusConflict, codePairReplaces, revokedDeviceLoginMsg)
			return
		}
		writeInternal(w, "login", err)
		return
	}
	auditAs(db, fedActor(claims.Sub), store.AuditLogin, req.DeviceLabel, "via account server "+trusted.Name+" from "+clientIP(r))
	writePairResponse(w, r, newPairResponse(cfg, token, deviceID))
}

// --------- /settings/account-server ---------

type trustedServerBody struct {
	Name           string `json:"name"`
	PublicKey      string `json:"publicKey"`
	KeyFingerprint string `json:"keyFingerprint"`
}

type accountServerSettingsBody struct {
	Enabled       *bool              `json:"enabled"`
	Name          *string            `json:"name"`
	TrustedServer *trustedServerBody `json:"trustedServer"`
	// Password is the owner's account password, re-entered to prove the PC owner is present; required
	// only together with trustedServer.
	Password string `json:"password"`
}

func accountServerSettingsJSON(cfg Config, db *store.DB) (map[string]any, error) {
	id, _, err := loadAccountIdentity(cfg, db)
	if err != nil {
		return nil, err
	}
	as, err := db.AccountServer()
	if err != nil {
		return nil, err
	}
	trusted, err := db.TrustedAccountServer()
	if err != nil {
		return nil, err
	}
	out := map[string]any{"enabled": as.Enabled, "name": as.Name, "trustedServer": nil}
	if id.enabled {
		out["name"] = id.name
		out["keyFingerprint"] = id.fp
	}
	if trusted != nil {
		out["trustedServer"] = map[string]any{
			"name": trusted.Name, "publicKey": trusted.PublicKey, "keyFingerprint": trusted.Fingerprint,
		}
	}
	return out, nil
}

func getAccountServerSettingsHandler(cfg Config, db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if forbidUnlessAdmin(w, deviceFromContext(r)) {
			return
		}
		out, err := accountServerSettingsJSON(cfg, db)
		if err != nil {
			writeInternal(w, "get account server", err)
			return
		}
		writeJSON(w, http.StatusOK, out)
	}
}

// federatedActor names a federated account in the audit log: the account name, "@", and the first
// federatedActorFpLen hex characters of the vouching key's fingerprint.
func federatedActor(sub, issuerFingerprint string) string {
	if len(issuerFingerprint) > federatedActorFpLen {
		issuerFingerprint = issuerFingerprint[:federatedActorFpLen]
	}
	return sub + "@" + issuerFingerprint
}

// forbidFederated writes the 403 for a session an account server vouched for and reports whether it did.
// Such a session has the limited store.FederatedGrantProfile and is not an admin device; this refusal is
// defence in depth for the host administration routes (pairing, other devices' access, global settings,
// accounts, the account-server role). A lookup failure also refuses.
func forbidFederated(w http.ResponseWriter, db *store.DB, d *store.Device) bool {
	if d == nil {
		return false
	}
	_, issuer, err := db.DeviceAccount(d.ID)
	if err != nil {
		writeInternal(w, "federated session check", err)
		return true
	}
	if issuer == "" {
		return false
	}
	writeError(w, http.StatusForbidden, "FORBIDDEN", federatedForbiddenMsg)
	return true
}

// denyFederated is forbidFederated as router middleware; it must run after authMiddleware.
func denyFederated(db *store.DB) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if forbidFederated(w, db, deviceFromContext(r)) {
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// forbidAccountServerSession refuses a session an account server vouched for the right to change which
// server is trusted: otherwise a compromised account server could re-pin itself and outlive an untrust.
func forbidAccountServerSession(w http.ResponseWriter, db *store.DB, dev *store.Device) bool {
	_, issuer, err := db.DeviceAccount(dev.ID)
	if err != nil {
		writeInternal(w, "account server settings", err)
		return true
	}
	if issuer != "" {
		writeError(w, http.StatusForbidden, "FORBIDDEN", "a session from an account server cannot change which server is trusted")
		return true
	}
	return false
}

func validAccountLabel(s string) bool {
	if len(s) > maxAccountNameLen || !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

// requireOwnerPassword checks the password re-entered with a request that pins an account server against the
// account the calling device logged in with, so a stolen session token alone cannot pin a key. A missing
// password is a 403 CAPABILITY_DENIED, a wrong one a 401 INVALID_CREDENTIALS that counts toward the account's
// failed-login budget (the one POST /login uses; spent: 429). It reports whether the caller may go on.
func requireOwnerPassword(w http.ResponseWriter, r *http.Request, db *store.DB, dev *store.Device, password string, fails *keyedLimiter) bool {
	if password == "" {
		writeError(w, http.StatusForbidden, "CAPABILITY_DENIED", "pinning an account server needs the owner password: send \"password\"")
		return false
	}
	username, _, err := db.DeviceAccount(dev.ID)
	if err != nil {
		writeInternal(w, "account server settings", err)
		return false
	}
	if username == "" {
		writeError(w, http.StatusForbidden, "CAPABILITY_DENIED", "this device has no owner account to confirm the password against")
		return false
	}
	account := strings.ToLower(username)
	if fails.Exhausted(account) {
		writeError(w, http.StatusTooManyRequests, "RATE_LIMITED", "too many failed logins for this account, try again later")
		return false
	}
	user, err := db.GetUserByUsername(username)
	if err != nil {
		writeInternal(w, "account server settings", err)
		return false
	}
	if user == nil {
		security.BurnPasswordCheck(password)
	}
	if user == nil || !security.VerifyPassword(user.PasswordHash, password) {
		fails.Allow(account)
		auditAs(db, username, store.AuditLoginFailed, "", "account server pin from "+clientIP(r))
		writeError(w, http.StatusUnauthorized, "INVALID_CREDENTIALS", "invalid password")
		return false
	}
	return true
}

// putAccountServerSettingsHandler changes the account-server role (on/off, name) and the trusted server's
// pin. Setting the pin also needs the owner's password again (see requireOwnerPassword).
func putAccountServerSettingsHandler(cfg Config, db *store.DB, accountFails *keyedLimiter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		dev := deviceFromContext(r)
		if forbidUnlessAdmin(w, dev) {
			return
		}
		var b accountServerSettingsBody
		if !decodeJSONBody(w, r, &b) {
			return
		}
		if b.Enabled == nil && b.Name == nil && b.TrustedServer == nil {
			writeError(w, http.StatusBadRequest, codeBadRequest, "nothing to change: send enabled, name or trustedServer")
			return
		}
		// Validate everything before changing anything.
		var newName string
		if b.Name != nil {
			newName = strings.TrimSpace(*b.Name)
			if !validAccountLabel(newName) {
				writeError(w, http.StatusBadRequest, codeBadRequest, "name must be at most 64 characters without control characters")
				return
			}
		}
		var pin *trustedServerBody
		var pinFp string
		if b.TrustedServer != nil {
			if forbidAccountServerSession(w, db, dev) {
				return
			}
			pin = b.TrustedServer
			raw, err := base64.StdEncoding.DecodeString(pin.PublicKey)
			if err != nil || len(raw) != ed25519.PublicKeySize {
				writeError(w, http.StatusBadRequest, codeBadRequest, "trustedServer.publicKey must be a base64 Ed25519 public key")
				return
			}
			pinFp = security.KeyFingerprint(ed25519.PublicKey(raw))
			if pin.KeyFingerprint != "" && !constEq(strings.ToLower(pin.KeyFingerprint), pinFp) {
				writeError(w, http.StatusBadRequest, codeBadRequest, "trustedServer.keyFingerprint does not match publicKey")
				return
			}
			pin.Name = strings.TrimSpace(pin.Name)
			if pin.Name == "" {
				pin.Name = "account server"
			}
			if !validAccountLabel(pin.Name) {
				writeError(w, http.StatusBadRequest, codeBadRequest, "trustedServer.name must be at most 64 characters without control characters")
				return
			}
			if !requireOwnerPassword(w, r, db, dev, b.Password, accountFails) {
				return
			}
		}

		if b.Enabled != nil || b.Name != nil {
			cur, err := db.AccountServer()
			if err != nil {
				writeInternal(w, "put account server", err)
				return
			}
			enabled, name := cur.Enabled, cur.Name
			if b.Enabled != nil {
				enabled = *b.Enabled
			}
			if b.Name != nil {
				name = newName
			}
			if err := db.SetAccountServer(enabled, name); err != nil {
				writeInternal(w, "put account server", err)
				return
			}
			state := "disabled"
			if enabled {
				state = "enabled"
			}
			audit(db, r, store.AuditAccountServer, "", "account server role "+state)
		}
		if pin != nil {
			oldFp := "none"
			if old, err := db.TrustedAccountServer(); err != nil {
				writeInternal(w, "put account server", err)
				return
			} else if old != nil {
				oldFp = old.Fingerprint
			}
			if _, err := db.SetTrustedAccountServer(pin.Name, pin.PublicKey); err != nil {
				writeInternal(w, "put account server", err)
				return
			}
			audit(db, r, store.AuditAccountServer, pin.Name, "trusted account server key "+pinFp)
			audit(db, r, store.AuditAccountServerPinChanged, pin.Name, "old "+oldFp+" new "+pinFp)
		}
		out, err := accountServerSettingsJSON(cfg, db)
		if err != nil {
			writeInternal(w, "put account server", err)
			return
		}
		writeJSON(w, http.StatusOK, out)
	}
}

func deleteAccountServerTrustHandler(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		dev := deviceFromContext(r)
		if forbidUnlessAdmin(w, dev) || forbidAccountServerSession(w, db, dev) {
			return
		}
		trusted, err := db.TrustedAccountServer()
		if err != nil {
			writeInternal(w, "delete account server trust", err)
			return
		}
		if err := db.RemoveTrustedAccountServer(); err != nil {
			writeInternal(w, "delete account server trust", err)
			return
		}
		if trusted != nil {
			audit(db, r, store.AuditAccountServer, trusted.Name, "no longer trusting account server key "+trusted.Fingerprint)
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
