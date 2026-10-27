// Package server — approve-on-PC pairing.
//
// A phone that can reach the agent but holds no code asks to be paired; the
// owner approves or rejects at the PC (desktop notification, `rfe pair
// accept`, or an admin session). The phone polls until the decision. The two
// sides show a short match code (pairing.SAS) so the owner can tell the
// request came from the phone in front of them.
package server

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/pairing"
	"github.com/ZaidQamhieh/rfe/agent/internal/store"
	"github.com/go-chi/chi/v5"
)

// pairRequestTTL is how long the owner has to answer.
const pairRequestTTL = 2 * time.Minute

const (
	pairRequestRateAttempts = 6
	pairRequestRateWindow   = time.Minute
	// Polling is cheap and legitimate (every ~2s for up to the TTL).
	pairPollRateAttempts = 120
)

// PairPrompt is what the owner is asked to approve.
type PairPrompt struct {
	ID    string
	Label string
	SAS   string
	IP    string
	TTL   time.Duration
	// Replaces names the already paired device this approval would take over (new key, new token,
	// access reset to browse-only); empty for a new phone.
	Replaces string
}

type pairRequestBody struct {
	DeviceLabel     string `json:"deviceLabel"`
	DeviceID        string `json:"deviceId"`
	DevicePublicKey string `json:"devicePublicKey"`
	Nonce           string `json:"nonce"`
	Signature       string `json:"signature"`
	// ClientNonceCommit is the hex SHA-256 of the 16-byte client nonce the phone
	// will reveal after it has the request id. Committing first means a relay cannot
	// pick the request id after seeing the nonce, so it cannot grind an id that makes
	// the two match codes agree.
	ClientNonceCommit string `json:"clientNonceCommit"`
}

// revealBody is POST /v1/pair/request/{id}/reveal. The nonce seeds the match code
// and is the secret that authorises polling for the result.
type revealBody struct {
	ClientNonce string `json:"clientNonce"`
}

func validNonceCommit(s string) bool {
	b, err := hex.DecodeString(s)
	return err == nil && len(b) == sha256.Size
}

func validClientNonce(s string) bool {
	b, err := hex.DecodeString(s)
	return err == nil && len(b) == 16
}

// createPairRequestHandler implements POST /v1/pair/request.
func createPairRequestHandler(cfg Config, db *store.DB, nonces *nonceStore) http.HandlerFunc {
	limiter := newKeyedLimiter(pairRequestRateAttempts, pairRequestRateWindow)
	return func(w http.ResponseWriter, r *http.Request) {
		if !limiter.Allow(clientIP(r)) {
			writeError(w, http.StatusTooManyRequests, "RATE_LIMITED", "too many pairing attempts, try again later")
			return
		}
		var req pairRequestBody
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid request body")
			return
		}
		if !validNonceCommit(req.ClientNonceCommit) {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "clientNonceCommit (hex SHA-256 of the client nonce) is required — update the app")
			return
		}
		if err := verifyDeviceProof(db, nonces, cfg.CertFingerprint, req.DeviceID, req.DevicePublicKey, req.Nonce, req.Signature, w, rePinOnKeyChange, requireBoundProof); err != nil {
			return
		}
		label := sanitizeLabel(req.DeviceLabel)
		id, err := randomToken(16)
		if err != nil {
			writeInternal(w, "pair request", err)
			return
		}
		now := time.Now()
		err = db.CreatePairRequest(store.PairRequest{
			ID: id, Label: label, ClientID: req.DeviceID, PublicKey: req.DevicePublicKey,
			NonceCommit: req.ClientNonceCommit, RemoteIP: clientIP(r),
			Created: now.Unix(), Expires: now.Add(pairRequestTTL).Unix(),
		})
		if errors.Is(err, store.ErrTooManyPairRequests) {
			writeError(w, http.StatusTooManyRequests, "PAIR_BUSY", "other pairing requests are waiting for approval on the computer")
			return
		}
		if err != nil {
			writeInternal(w, "pair request", err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"requestId":        id,
			"expiresInSeconds": int(pairRequestTTL.Seconds()),
		})
	}
}

// revealPairRequestHandler implements POST /v1/pair/request/{id}/reveal. Only now,
// with the nonce fixed after the request id, is the match code derived and the
// owner asked.
func revealPairRequestHandler(cfg Config, db *store.DB) http.HandlerFunc {
	limiter := newKeyedLimiter(pairRequestRateAttempts*4, pairRequestRateWindow)
	return func(w http.ResponseWriter, r *http.Request) {
		if !limiter.Allow(clientIP(r)) {
			writeError(w, http.StatusTooManyRequests, "RATE_LIMITED", "too many pairing attempts, try again later")
			return
		}
		var body revealBody
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10)).Decode(&body); err != nil || !validClientNonce(body.ClientNonce) {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "clientNonce must be 16 random bytes, hex encoded")
			return
		}
		id := chi.URLParam(r, "id")
		pr, ok, err := db.GetPairRequest(id)
		if err != nil {
			writeInternal(w, "pair reveal", err)
			return
		}
		raw, _ := hex.DecodeString(body.ClientNonce)
		sum := sha256.Sum256(raw)
		// One error for unknown id and wrong nonce so ids cannot be probed.
		if !ok || pr.Status != store.PairCommitted || subtle.ConstantTimeCompare([]byte(hex.EncodeToString(sum[:])), []byte(pr.NonceCommit)) != 1 {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "pairing request not found or expired")
			return
		}
		sas, err := pairing.SAS(cfg.CertFingerprint, body.ClientNonce, id)
		if err != nil {
			writeInternal(w, "pair reveal", err)
			return
		}
		replaces, err := pairReplaces(db, pr.ClientID, pr.PublicKey)
		if err != nil {
			writeInternal(w, "pair reveal", err)
			return
		}
		pr, err = db.RevealPairRequest(id, body.ClientNonce)
		if errors.Is(err, store.ErrPairRequestGone) {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "pairing request not found or expired")
			return
		}
		if errors.Is(err, store.ErrTooManyPairRequests) {
			writeError(w, http.StatusTooManyRequests, "PAIR_BUSY", "too many pairing requests are waiting for approval; try again shortly")
			return
		}
		if err != nil {
			writeInternal(w, "pair reveal", err)
			return
		}
		if cfg.OnPairRequest != nil {
			cfg.OnPairRequest(PairPrompt{ID: id, Label: pr.Label, SAS: sas, IP: clientIP(r), TTL: time.Until(time.Unix(pr.Expires, 0)), Replaces: replaces})
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "pending"})
	}
}

// pairReplaces names the paired device that approving a request for clientID with publicKey would
// overwrite: one whose pinned key differs, or that was revoked. A phone re-pairing with its own key
// is not a replacement, it only lost its token.
func pairReplaces(db *store.DB, clientID, publicKey string) (string, error) {
	d, ok, err := db.ClientDeviceByID(clientID)
	if err != nil || !ok {
		return "", err
	}
	if d.Revoked || (d.PublicKey != "" && d.PublicKey != publicKey) {
		return d.Label, nil
	}
	return "", nil
}

// sanitizeLabel bounds and cleans the device name shown in the prompt, which an
// unauthenticated peer controls.
func sanitizeLabel(s string) string {
	out := make([]rune, 0, 40)
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			continue
		}
		out = append(out, r)
		if len(out) == 40 {
			break
		}
	}
	if len(out) == 0 {
		return "unnamed-device"
	}
	return string(out)
}

// pollPairRequestHandler implements GET /v1/pair/request/{id}?nonce=<clientNonce>.
func pollPairRequestHandler(cfg Config, db *store.DB) http.HandlerFunc {
	limiter := newKeyedLimiter(pairPollRateAttempts, pairRequestRateWindow)
	return func(w http.ResponseWriter, r *http.Request) {
		if !limiter.Allow(clientIP(r)) {
			writeError(w, http.StatusTooManyRequests, "RATE_LIMITED", "too many requests, try again later")
			return
		}
		id := chi.URLParam(r, "id")
		pr, ok, err := db.GetPairRequest(id)
		if err != nil {
			writeInternal(w, "pair poll", err)
			return
		}
		// One error for unknown id and wrong nonce so ids cannot be probed. A request that
		// has not revealed its nonce yet has none to match.
		if !ok || pr.ClientNonce == "" || subtle.ConstantTimeCompare([]byte(pr.ClientNonce), []byte(r.URL.Query().Get("nonce"))) != 1 {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "pairing request not found or expired")
			return
		}
		switch pr.Status {
		case store.PairPending:
			writeJSON(w, http.StatusOK, map[string]any{"status": "pending"})
		case store.PairRejected:
			writeJSON(w, http.StatusOK, map[string]any{"status": "rejected"})
		case store.PairApproved:
			claimed, ok, err := db.ClaimApprovedPairRequest(id)
			if err != nil {
				writeInternal(w, "pair poll", err)
				return
			}
			if !ok {
				writeError(w, http.StatusNotFound, "NOT_FOUND", "pairing request not found or expired")
				return
			}
			token, err := randomToken(32)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "INTERNAL", "failed to generate token")
				return
			}
			// Decided now, against the device as it is at approval time, before the upsert overwrites it.
			replaces, err := pairReplaces(db, claimed.ClientID, claimed.PublicKey)
			if err != nil {
				writeInternal(w, "pair poll", err)
				return
			}
			deviceID, err := db.UpsertDevice(claimed.ClientID, claimed.Label, token, claimed.PublicKey, false)
			if err != nil {
				writeInternal(w, "pair poll", err)
				return
			}
			detail := "approved at the computer, from " + claimed.RemoteIP
			if replaces != "" {
				// A takeover of an existing row must not inherit that device's grants.
				if err := db.ResetDeviceAccess(deviceID); err != nil {
					writeInternal(w, "pair poll", err)
					return
				}
				detail += "; replaced device " + strconv.Quote(replaces) + " (access reset to browse-only)"
			}
			auditAs(db, claimed.Label, store.AuditPair, deviceID, detail)
			resp := pairResponse{
				DeviceToken:      token,
				DeviceID:         deviceID,
				AgentName:        cfg.Name,
				CertFingerprint:  cfg.CertFingerprint,
				Address:          cfg.Address,
				TailscaleAddress: cfg.TailscaleAddress,
			}
			if isWebSessionRequest(r) {
				setWebSessionCookie(w, token)
				resp.DeviceToken = ""
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"status": "approved", "deviceToken": resp.DeviceToken, "deviceId": resp.DeviceID,
				"agentName": resp.AgentName, "certFingerprint": resp.CertFingerprint,
				"address": resp.Address, "tailscaleAddress": resp.TailscaleAddress,
			})
		default:
			writeError(w, http.StatusNotFound, "NOT_FOUND", "pairing request not found or expired")
		}
	}
}

// listPairRequestsHandler implements GET /v1/pair/requests (admin).
func listPairRequestsHandler(cfg Config, db *store.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !isAdminDevice(deviceFromContext(r)) {
			writeError(w, http.StatusForbidden, "FORBIDDEN", "answering pairing requests requires an admin (login) session or the PC")
			return
		}
		list, err := db.ListPendingPairRequests()
		if err != nil {
			writeInternal(w, "list pair requests", err)
			return
		}
		out := make([]map[string]any, 0, len(list))
		for _, pr := range list {
			sas, _ := pairing.SAS(cfg.CertFingerprint, pr.ClientNonce, pr.ID)
			replaces, err := pairReplaces(db, pr.ClientID, pr.PublicKey)
			if err != nil {
				writeInternal(w, "list pair requests", err)
				return
			}
			out = append(out, map[string]any{
				"id": pr.ID, "label": pr.Label, "matchCode": sas, "remoteIp": pr.RemoteIP, "replaces": replaces,
				"expiresAt": time.Unix(pr.Expires, 0).UTC().Format(time.RFC3339),
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{"requests": out})
	}
}

// decidePairRequestHandler implements POST /v1/pair/requests/{id}/{approve|reject} (admin).
func decidePairRequestHandler(db *store.DB, approve bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !isAdminDevice(deviceFromContext(r)) {
			writeError(w, http.StatusForbidden, "FORBIDDEN", "answering pairing requests requires an admin (login) session or the PC")
			return
		}
		err := db.DecidePairRequest(chi.URLParam(r, "id"), approve)
		if errors.Is(err, store.ErrPairRequestGone) {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "pairing request not found, expired or already answered")
			return
		}
		if err != nil {
			writeInternal(w, "decide pair request", err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
