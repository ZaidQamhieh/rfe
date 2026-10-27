// Package server — account registration.
//
// Unlike login, register is reachable with no account yet — so it needs its
// own gate against a stranger on the network creating an account (and thus a
// device token) before the owner does. It reuses the same one-time pairing
// code /v1/pair already requires (minted locally via `rfe pair`,
// physical/terminal access to the host): register both creates the account
// and pairs the calling device in one step, letting first-time setup happen
// entirely from the phone app or web companion instead of also needing
// `rfe adduser` at the terminal. adduser remains for headless setups.
package server

import (
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ZaidQamhieh/rfe/agent/internal/pairing"
	"github.com/ZaidQamhieh/rfe/agent/internal/security"
	"github.com/ZaidQamhieh/rfe/agent/internal/store"
)

const (
	registerRateLimitAttempts = 10
	registerRateLimitWindow   = time.Minute
)

type registerRequest struct {
	PairingCode string `json:"pairingCode"`
	Username    string `json:"username"`
	Password    string `json:"password"`
	DeviceLabel string `json:"deviceLabel"`
	DeviceID    string `json:"deviceId"`
	// DevicePublicKey/Nonce/Signature — same device-identity proof as
	// pairRequest/loginRequest (see device_identity.go).
	DevicePublicKey string `json:"devicePublicKey"`
	Nonce           string `json:"nonce"`
	Signature       string `json:"signature"`
}

func registerHandler(cfg Config, db *store.DB, pm *pairing.Manager, nonces *nonceStore) http.HandlerFunc {
	// Per source first, so one noisy address spends its own allowance and not everyone's; the global
	// window behind it still caps total guessing at the pairing code from many addresses (audit F-147).
	perIP := newKeyedLimiter(registerRateLimitAttempts, registerRateLimitWindow)
	limiter := newFixedWindowLimiter(registerRateLimitAttempts*4, registerRateLimitWindow)
	return func(w http.ResponseWriter, r *http.Request) {
		if !perIP.Allow(clientIP(r)) || !limiter.Allow() {
			writeError(w, http.StatusTooManyRequests, "RATE_LIMITED", "too many registration attempts, try again later")
			return
		}
		var req registerRequest
		if !decodeJSONBody(w, r, &req) {
			return
		}
		// Validate the cheap stuff and device-identity proof BEFORE consuming
		// the one-time pairing code: the code is precious (minted at the
		// terminal, single-use) while a weak password or a bad/expired nonce
		// is a recoverable client-side mistake — burning the code on that
		// would force a trip back to the PC for something that wasn't the
		// code's fault. See pairHandler for the same reasoning.
		if req.Username == "" || utf8.RuneCountInString(req.Password) < 8 || len(req.Password) > security.MaxPasswordBytes {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "username required and password must be 8 characters to 72 bytes")
			return
		}
		// One account per agent (see the `users` table doc comment in
		// store.go) — enforced here, not just documented, so a stranger
		// with a valid pairing code can't spawn additional accounts once
		// the owner has already registered. `rfe adduser` bypasses
		// this deliberately for headless/scripted setups.
		hasUser, err := db.HasAnyUser()
		if err != nil {
			writeInternal(w, "register", err)
			return
		}
		if hasUser {
			writeError(w, http.StatusConflict, "ACCOUNT_ALREADY_EXISTS", "this computer already has an account — log in instead, or ask the owner to add a device")
			return
		}
		if err := verifyDeviceProof(db, nonces, cfg.CertFingerprint, req.DeviceID, req.DevicePublicKey, req.Nonce, req.Signature, w, rePinOnKeyChange, requireBoundProof); err != nil {
			return // verifyDeviceProof already wrote the error response
		}
		// An ordinary code must not be spent by a register attempt it can never satisfy (audit F-07).
		if peek := pm.Peek(req.PairingCode); peek.Valid && !peek.CanRegister {
			writeError(w, http.StatusForbidden, "CODE_CANNOT_REGISTER", "this code can't create an account — run `rfe pair --owner` on the computer")
			return
		}
		codeInfo := pm.Consume(req.PairingCode)
		if !codeInfo.Valid {
			writeError(w, http.StatusUnauthorized, "INVALID_CODE", "invalid or expired pairing code — run `rfe pair --owner` on the computer to mint one")
			return
		}
		// Ordinary codes pair a device only; creating the owner account takes
		// a code minted for it, so a code given to a guest can't claim the PC.
		if !codeInfo.CanRegister {
			writeError(w, http.StatusForbidden, "CODE_CANNOT_REGISTER", "this code can't create an account — run `rfe pair --owner` on the computer")
			return
		}

		hash, err := security.HashPassword(req.Password)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "INTERNAL", "failed to hash password")
			return
		}

		if req.DeviceLabel == "" {
			req.DeviceLabel = "unnamed-device"
		}
		token, err := randomToken(32)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "INTERNAL", "failed to generate token")
			return
		}
		// One transaction for the account + its device: the HasAnyUser check
		// above is only an early, friendly 409 — RegisterAccount re-checks it
		// atomically, so two simultaneous registrations can't both win (PR-45).
		deviceID, err := db.RegisterAccount(req.DeviceID, req.DeviceLabel, token, req.DevicePublicKey, req.Username, hash)
		if err != nil {
			switch {
			case errors.Is(err, store.ErrAccountExists):
				writeError(w, http.StatusConflict, "ACCOUNT_ALREADY_EXISTS", "this computer already has an account — log in instead, or ask the owner to add a device")
			case strings.Contains(err.Error(), "UNIQUE constraint failed"):
				// A genuine username collision is a common, user-facing 409;
				// any other DB error (locked/corrupt DB, disk full) must not be
				// misreported as "taken".
				writeError(w, http.StatusConflict, "USERNAME_TAKEN", "that username is already registered on this computer")
			default:
				writeInternal(w, "register", err)
			}
			return
		}
		auditAs(db, req.Username, store.AuditRegister, req.DeviceLabel, "from "+clientIP(r))

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
