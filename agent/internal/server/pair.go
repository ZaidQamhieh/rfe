// Package server — pair handler.
package server

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/pairing"
	"github.com/ZaidQamhieh/rfe/agent/internal/settings"
	"github.com/ZaidQamhieh/rfe/agent/internal/store"
	"github.com/skip2/go-qrcode"
)

// pairRateLimit caps unauthenticated /v1/pair attempts per source address. The pairing code is
// only ~40 bits, so without throttling it's brute-forceable; 10/min is ample
// for legitimate use (a human typing/scanning a code) on a single-user agent.
// A global budget of failed guesses (pair_budget.go) covers callers with many addresses.
const (
	pairRateLimitAttempts = 10
	pairRateLimitWindow   = time.Minute
)

// tokenGenerateFailedMsg is the 500 message when a device token cannot be minted.
const tokenGenerateFailedMsg = "failed to generate token"

// pairRateLimitedMsg is the 429 message when a source exceeds a pairing rate limit.
const pairRateLimitedMsg = "too many pairing attempts, try again later"

// pairQRSize is the side length, in pixels, of the pairing QR code PNG.
const pairQRSize = 240

type pairRequest struct {
	PairingCode string `json:"pairingCode"`
	DeviceLabel string `json:"deviceLabel"`
	// DeviceID is a hardware-stable client identifier (Android ID). When
	// present it deduplicates pairings so the same phone reuses its device row.
	DeviceID string `json:"deviceId"`
	// DevicePublicKey is the device's permanent Ed25519 identity key
	// (standard base64), Nonce a value freshly minted by POST /v1/auth/challenge,
	// and Signature that nonce signed with the matching private key — proof of
	// possession, pinned to the device row on success. See verifyDeviceProof in challenge.go.
	DevicePublicKey string `json:"devicePublicKey"`
	Nonce           string `json:"nonce"`
	Signature       string `json:"signature"`
}

type pairResponse struct {
	DeviceToken      string `json:"deviceToken,omitempty"`
	DeviceID         string `json:"deviceId"`
	AgentName        string `json:"agentName"`
	CertFingerprint  string `json:"certFingerprint"`
	Address          string `json:"address"`
	TailscaleAddress string `json:"tailscaleAddress,omitempty"`
}

// newPairResponse is what every successful pairing, registration or login tells the device about this agent.
func newPairResponse(cfg Config, token, deviceID string) pairResponse {
	return pairResponse{
		DeviceToken:      token,
		DeviceID:         deviceID,
		AgentName:        cfg.Name,
		CertFingerprint:  cfg.CertFingerprint,
		Address:          cfg.Address,
		TailscaleAddress: cfg.TailscaleAddress,
	}
}

// forSession keeps the bearer token in the response for native clients. The embedded browser receives the credential
// only as an HttpOnly cookie, so page scripts cannot read or export it.
func (response pairResponse) forSession(w http.ResponseWriter, r *http.Request) pairResponse {
	if isWebSessionRequest(r) {
		setWebSessionCookie(w, response.DeviceToken)
		response.DeviceToken = ""
	}
	return response
}

// writePairResponse sends the response with the session credential handled by forSession.
func writePairResponse(w http.ResponseWriter, r *http.Request, response pairResponse) {
	writeJSON(w, http.StatusOK, response.forSession(w, r))
}

// logoutHandler clears the browser cookie and kills the session's token server-side, so a copy of the cookie value
// (from disk or a proxy) is dead after logout. A password-login session has its device row deleted; a paired browser
// is revoked (its row stays, visible on the Devices page) and must pair again.
func logoutHandler(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie(webSessionCookie); err == nil && c.Value != "" && validWebSessionRequest(r) {
			if d, err := db.DeviceByToken(c.Value); err == nil && d != nil {
				var endErr error
				if d.ViaLogin {
					endErr = db.DeleteDevice(d.ID)
				} else {
					endErr = db.RevokeDeviceSession(d.ID)
				}
				if endErr != nil {
					log.Printf("logout: ending session for device %s failed: %v", d.ID, endErr)
				}
			}
		}
		clearWebSessionCookie(w)
		w.WriteHeader(http.StatusNoContent)
	}
}

func pairHandler(cfg Config, db *store.DB, pm *pairing.Manager, nonces *nonceStore) http.HandlerFunc {
	return pairHandlerClock(cfg, db, pm, nonces, time.Now)
}

// pairHandlerClock is pairHandler with an injectable clock for the global failed-guess budget. Besides the
// per-address limit it refuses every request (429 + Retry-After) while the global failure budget is spent or
// pairing is locked, and writes one pair.locked audit row per lockout. The source is r.RemoteAddr only.
func pairHandlerClock(cfg Config, db *store.DB, pm *pairing.Manager, nonces *nonceStore, now func() time.Time) http.HandlerFunc {
	limiter := newKeyedLimiter(pairRateLimitAttempts, pairRateLimitWindow)
	budget := newPairBudget(now)
	return func(w http.ResponseWriter, r *http.Request) {
		if wait := budget.refusedFor(); wait > 0 {
			w.Header().Set("Retry-After", retryAfterSeconds(wait))
			writeError(w, http.StatusTooManyRequests, "RATE_LIMITED", pairLockedMsg)
			return
		}
		if !limiter.Allow(clientIP(r)) {
			writeError(w, http.StatusTooManyRequests, "RATE_LIMITED", pairRateLimitedMsg)
			return
		}
		var req pairRequest
		if !decodeJSONBody(w, r, &req) {
			return
		}
		// Validate device-identity proof BEFORE consuming the one-time
		// pairing code: the code is precious (minted at the terminal,
		// single-use) while a bad/expired nonce or signature is a
		// recoverable client-side hiccup — burning the code on that would
		// force a trip back to the PC for something that wasn't the code's
		// fault.
		if err := verifyDeviceProof(db, nonces, cfg.CertFingerprint, req.DeviceID, req.DevicePublicKey, req.Nonce, req.Signature, w, rePinOnKeyChange); err != nil {
			return // verifyDeviceProof already wrote the error response
		}
		// A valid code must not be able to replace another phone's pinned key: refuse before the code is spent,
		// and only for a code that is valid so an unauthenticated caller learns nothing about known device ids.
		if pm.Peek(req.PairingCode).Valid && refuseKeyReplacement(w, db, req.DeviceID, req.DevicePublicKey) {
			return
		}
		codeInfo := pm.Consume(req.PairingCode)
		if !codeInfo.Valid {
			if failures, locked := budget.recordFailure(); locked {
				auditAs(db, "agent", auditPairLocked, "", fmt.Sprintf("%d failed pairing attempts within %s; pairing refused for %s", failures, pairLockWindow, pairLockCooldown))
			}
			writeError(w, http.StatusUnauthorized, "INVALID_CODE", "invalid or expired pairing code")
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

		// Checked again now the code is spent: a concurrent pairing may have created the row since the first check.
		if refuseKeyReplacement(w, db, req.DeviceID, req.DevicePublicKey) {
			return
		}
		// Decided before the upsert overwrites the row: a revoked device
		// coming back must not inherit its old grants.
		replaces, err := pairReplaces(db, req.DeviceID, req.DevicePublicKey)
		if err != nil {
			writeInternal(w, "pair", err)
			return
		}
		deviceID, err := db.UpsertDevice(req.DeviceID, req.DeviceLabel, token, req.DevicePublicKey, false)
		if err != nil {
			writeInternal(w, "pair", err)
			return
		}
		if replaces != "" {
			if err := db.ResetDeviceAccess(deviceID); err != nil {
				writeInternal(w, "pair", err)
				return
			}
		}
		// Apply the pairing code's guest-mode defaults, if any, to the new
		// device. A normal (non-guest) code has JailRoot=="" and ReadOnly==
		// false, so this is a no-op for the common path.
		if codeInfo.JailRoot != "" {
			if err := db.SetDeviceJail(deviceID, codeInfo.JailRoot); err != nil {
				// The device row already exists with a live token; without its guest limits it must not stay usable.
				revokeAfterGuestLimitFailure(db, deviceID)
				writeInternal(w, "pair", err)
				return
			}
		}
		if codeInfo.ReadOnly {
			if err := db.SetDeviceReadOnly(deviceID, true); err != nil {
				// The device row already exists with a live token; without its guest limits it must not stay usable.
				revokeAfterGuestLimitFailure(db, deviceID)
				writeInternal(w, "pair", err)
				return
			}
		}
		auditAs(db, req.DeviceLabel, store.AuditPair, deviceID, "from "+clientIP(r))

		writePairResponse(w, r, newPairResponse(cfg, token, deviceID))
	}
}

// codePairReplaces is the error code for a pairing code that would replace a known device's pinned key.
const codePairReplaces = "PAIR_REPLACES"

// pairReplacesMsg tells the phone why the code was refused and what to do instead.
const pairReplacesMsg = "this computer already has a device with this id and a different key, so a pairing code cannot replace it — ask for approval on the computer instead, or remove the old device there first (rfe remove <id>)"

// revokedDeviceLoginMsg tells a phone why an account-server login was refused for a device the owner revoked.
const revokedDeviceLoginMsg = "this computer revoked this device, so an account-server login cannot bring it back — ask for approval on the computer instead, or remove the old device there first (rfe remove <id>)"

// deniedAccountLoginMsg tells a phone why an account-server login was refused for an account the owner denied.
const deniedAccountLoginMsg = "the owner of this computer has blocked this account"

// refuseKeyReplacement answers 409 PAIR_REPLACES (or 500 on a store error) and reports true when clientID is
// already paired with a public key other than publicKey. Replacing a pinned key needs the owner's approval on
// the PC (POST /pair/request), not just a pairing code. Same key, unknown id or a legacy row without a key pass.
func refuseKeyReplacement(w http.ResponseWriter, db *store.DB, clientID, publicKey string) bool {
	d, ok, err := db.ClientDeviceByID(clientID)
	if err != nil {
		writeInternal(w, "pair", err)
		return true
	}
	if !ok || d.PublicKey == "" || d.PublicKey == publicKey {
		return false
	}
	writeError(w, http.StatusConflict, codePairReplaces, pairReplacesMsg)
	return true
}

// generatePairingHandler implements POST /v1/pairing/generate: mints a new
// one-time pairing code from an authenticated session, so a new device can be
// paired without a trip to the PC terminal (`rfe pair`). Admin-only
// (see isAdminDevice) — an ordinary paired device (e.g. the phone app)
// minting fresh codes for arbitrary new devices is a materially bigger
// privilege than the read-only web-companion endpoints, so it is gated the
// same as managing other devices.
func generatePairingHandler(pm *pairing.Manager, st *settings.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !isAdminDevice(deviceFromContext(r)) {
			writeError(w, http.StatusForbidden, "FORBIDDEN", "minting pairing codes requires an admin (login) session or the PC")
			return
		}
		var req struct {
			TTLSeconds int    `json:"ttlSeconds"`
			Guest      bool   `json:"guest"`
			JailRoot   string `json:"jailRoot"`
		}
		// The body is optional (an empty one mints a default code), but a malformed one must not silently become that
		// default: a broken guest request would otherwise mint a full-access code.
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxJSONBody)).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			writeDecodeError(w, err, "invalid JSON body")
			return
		}
		ttl := pairing.DefaultTTL
		if req.TTLSeconds > 0 {
			ttl = time.Duration(req.TTLSeconds) * time.Second
		}

		var (
			code    string
			payload pairing.QRPayload
			err     error
		)
		if req.Guest {
			jailRoot, verr := ValidateJailRoot(req.JailRoot, st.Roots(), true)
			if verr != nil {
				writeError(w, http.StatusBadRequest, "BAD_REQUEST", verr.Error())
				return
			}
			if jailRoot == "" {
				writeError(w, http.StatusBadRequest, "BAD_REQUEST", "guest pairing codes require a jailRoot")
				return
			}
			code, payload, err = pm.MintGuest(ttl, jailRoot)
		} else {
			code, payload, err = pm.Mint(ttl)
		}
		if err != nil {
			writeInternal(w, "generate pairing", err)
			return
		}
		resp := map[string]any{
			"pairingCode":      code,
			"expiresInSeconds": int(ttl.Seconds()),
			"qrPayload":        payload,
		}
		if png, qrErr := qrcode.Encode(payload.JSON(), qrcode.Medium, pairQRSize); qrErr == nil {
			resp["qrPngBase64"] = base64.StdEncoding.EncodeToString(png)
		} else {
			log.Printf("pairing: QR encode failed (non-fatal, code still valid): %v", qrErr)
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

// revokeAfterGuestLimitFailure revokes a device whose row was just written but
// whose guest limits could not be applied, so it does not stay usable without them.
func revokeAfterGuestLimitFailure(db *store.DB, deviceID string) {
	if revErr := db.RevokeDevice(deviceID); revErr != nil {
		log.Printf("pair: revoking device %s after failed guest limits failed: %v", deviceID, revErr)
	}
}

func randomToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
