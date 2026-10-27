// Package server — username/password login handler.
//
// Login is an additional way to obtain a device token, alongside the
// existing one-time pairing code (POST /v1/pair) — it does not replace it.
// One account per agent; logging in with it grants exactly the same access
// any paired device already has (there is no separate "user session" — the
// response is a normal device token, so every existing authenticated route
// works unchanged).
package server

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/security"
	"github.com/ZaidQamhieh/rfe/agent/internal/store"
)

// loginRateLimit mirrors pairRateLimit — a password is lower-entropy than a
// pairing code, so throttling matters at least as much here.
const (
	loginRateLimitAttempts = 10
	loginRateLimitWindow   = time.Minute
	// A per-account cap on failures from any address, so guesses spread over many sources still run out. It is
	// generous and counts failures only, so a stranger cannot lock the owner out with a few attempts.
	loginAccountFailures      = 30
	loginAccountFailureWindow = 15 * time.Minute
)

type loginRequest struct {
	Username    string `json:"username"`
	Password    string `json:"password"`
	DeviceLabel string `json:"deviceLabel"`
	DeviceID    string `json:"deviceId"`
	// DevicePublicKey/Nonce/Signature — same device-identity proof as
	// pairRequest (see pair.go and device_identity.go).
	DevicePublicKey string `json:"devicePublicKey"`
	Nonce           string `json:"nonce"`
	Signature       string `json:"signature"`
}

func loginHandler(cfg Config, db *store.DB, nonces *nonceStore) http.HandlerFunc {
	limiter := newKeyedLimiter(loginRateLimitAttempts, loginRateLimitWindow)
	accountFails := newKeyedLimiter(loginAccountFailures, loginAccountFailureWindow)
	return func(w http.ResponseWriter, r *http.Request) {
		if !limiter.Allow(clientIP(r)) {
			writeError(w, http.StatusTooManyRequests, "RATE_LIMITED", "too many login attempts, try again later")
			return
		}
		var req loginRequest
		if !decodeJSONBody(w, r, &req) {
			return
		}
		if req.Username == "" || req.Password == "" {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "username and password required")
			return
		}

		account := strings.ToLower(req.Username)
		if accountFails.Exhausted(account) {
			writeError(w, http.StatusTooManyRequests, "RATE_LIMITED", "too many failed logins for this account, try again later")
			return
		}
		user, err := db.GetUserByUsername(req.Username)
		if err != nil {
			writeInternal(w, "login", err)
			return
		}
		// Same error for "no such user" and "wrong password" — don't leak
		// which one it was.
		if user == nil {
			security.BurnPasswordCheck(req.Password)
		}
		if user == nil || !security.VerifyPassword(user.PasswordHash, req.Password) {
			// Records the attempted username, not whether it exists — the
			// response still doesn't distinguish the two, but the host owner
			// reading their own audit trail should see what was tried.
			accountFails.Allow(account)
			auditAs(db, req.Username, store.AuditLoginFailed, "", "from "+clientIP(r))
			writeError(w, http.StatusUnauthorized, "INVALID_CREDENTIALS", "invalid username or password")
			return
		}
		if err := verifyDeviceProof(db, nonces, cfg.CertFingerprint, req.DeviceID, req.DevicePublicKey, req.Nonce, req.Signature, w, rejectOnKeyChange, acceptLegacyProof); err != nil {
			return // verifyDeviceProof already wrote the error response
		}

		if req.DeviceLabel == "" {
			req.DeviceLabel = "unnamed-device"
		}
		token, err := randomToken(32)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "INTERNAL", "failed to generate token")
			return
		}
		// Device row + the account that authenticated it, in one transaction
		// (PR-45) — a failure between them used to leave a working token whose
		// device had no username recorded.
		deviceID, err := db.LoginDevice(req.DeviceID, req.DeviceLabel, token, req.DevicePublicKey, req.Username, user.PasswordHash)
		if err != nil {
			if errors.Is(err, store.ErrLoginAccountChanged) {
				auditAs(db, req.Username, store.AuditLoginFailed, req.DeviceLabel, "account changed during login")
				writeError(w, http.StatusUnauthorized, "INVALID_CREDENTIALS", "invalid username or password")
				return
			}
			writeInternal(w, "login", err)
			return
		}

		auditAs(db, req.Username, store.AuditLogin, req.DeviceLabel, "from "+clientIP(r))

		writePairResponse(w, r, pairResponse{
			DeviceToken:      token,
			DeviceID:         deviceID,
			AgentName:        cfg.Name,
			CertFingerprint:  cfg.CertFingerprint,
			Address:          cfg.Address,
			TailscaleAddress: cfg.TailscaleAddress,
		})
	}
}
