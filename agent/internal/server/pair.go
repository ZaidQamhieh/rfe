// Package server — pair handler.
package server

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/pairing"
	"github.com/ZaidQamhieh/rfe/agent/internal/settings"
	"github.com/ZaidQamhieh/rfe/agent/internal/store"
	"github.com/skip2/go-qrcode"
)

// pairRateLimit caps unauthenticated /v1/pair attempts. The pairing code is
// only ~40 bits, so without throttling it's brute-forceable; 10/min is ample
// for legitimate use (a human typing/scanning a code) on a single-user agent.
const (
	pairRateLimitAttempts = 10
	pairRateLimitWindow   = time.Minute
)

type pairRequest struct {
	PairingCode string `json:"pairingCode"`
	DeviceLabel string `json:"deviceLabel"`
	// DeviceID is a hardware-stable client identifier (Android ID). When
	// present it deduplicates pairings so the same phone reuses its device row.
	DeviceID string `json:"deviceId"`
	// DevicePublicKey is the device's permanent Ed25519 identity key
	// (standard base64), Nonce a value freshly minted by POST /v1/auth/challenge,
	// and Signature that nonce signed with the matching private key — proof of
	// possession, pinned to the device row on success. See device_identity.go.
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

// writePairResponse preserves the bearer-token response for native clients.
// The embedded browser receives the credential only as an HttpOnly cookie so
// page scripts cannot read or export it.
func writePairResponse(w http.ResponseWriter, r *http.Request, response pairResponse) {
	if isWebSessionRequest(r) {
		setWebSessionCookie(w, response.DeviceToken)
		response.DeviceToken = ""
	}
	writeJSON(w, http.StatusOK, response)
}

// logoutHandler clears the browser cookie and kills the session's token server-side, so a copy of the cookie value
// (from disk or a proxy) is dead after logout. A password-login session has its device row deleted; a paired browser
// is revoked (its row stays, visible on the Devices page) and must pair again.
func logoutHandler(db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if c, err := r.Cookie(webSessionCookie); err == nil && c.Value != "" && validWebSessionRequest(r) {
			if d, err := db.DeviceByToken(c.Value); err == nil && d != nil {
				if d.ViaLogin {
					_ = db.DeleteDevice(d.ID)
				} else {
					_ = db.RevokeDevice(d.ID)
				}
			}
		}
		clearWebSessionCookie(w)
		w.WriteHeader(http.StatusNoContent)
	}
}

func pairHandler(cfg Config, db *store.DB, pm *pairing.Manager, nonces *nonceStore) http.HandlerFunc {
	limiter := newKeyedLimiter(pairRateLimitAttempts, pairRateLimitWindow)
	return func(w http.ResponseWriter, r *http.Request) {
		if !limiter.Allow(clientIP(r)) {
			writeError(w, http.StatusTooManyRequests, "RATE_LIMITED", "too many pairing attempts, try again later")
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
		if err := verifyDeviceProof(db, nonces, cfg.CertFingerprint, req.DeviceID, req.DevicePublicKey, req.Nonce, req.Signature, w, rePinOnKeyChange, requireBoundProof); err != nil {
			return // verifyDeviceProof already wrote the error response
		}
		codeInfo := pm.Consume(req.PairingCode)
		if !codeInfo.Valid {
			writeError(w, http.StatusUnauthorized, "INVALID_CODE", "invalid or expired pairing code")
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

		// Decided before the upsert overwrites the row: pairing onto an
		// existing id with a different key (or a revoked row) is a takeover
		// and must not inherit that device's grants.
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
				_ = db.RevokeDevice(deviceID)
				writeInternal(w, "pair", err)
				return
			}
		}
		if codeInfo.ReadOnly {
			if err := db.SetDeviceReadOnly(deviceID, true); err != nil {
				// The device row already exists with a live token; without its guest limits it must not stay usable.
				_ = db.RevokeDevice(deviceID)
				writeInternal(w, "pair", err)
				return
			}
		}
		auditAs(db, req.DeviceLabel, store.AuditPair, deviceID, "from "+clientIP(r))

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
		_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, maxJSONBody)).Decode(&req) // body is optional; default TTL below
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
		if png, qrErr := qrcode.Encode(payload.JSON(), qrcode.Medium, 240); qrErr == nil {
			resp["qrPngBase64"] = base64.StdEncoding.EncodeToString(png)
		} else {
			log.Printf("pairing: QR encode failed (non-fatal, code still valid): %v", qrErr)
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

func randomToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
