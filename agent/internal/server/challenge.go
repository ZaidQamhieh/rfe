// Package server — challenge nonces for device-signature proof-of-possession.
package server

import (
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/security"
	"github.com/ZaidQamhieh/rfe/agent/internal/store"
)

// nonceTTL is how long a minted nonce stays valid. Short-lived: a client
// fetches a challenge and signs it in the same request round-trip, this just
// bounds how long a captured nonce could theoretically be replayed before
// Consume rejects it (it's single-use regardless).
const nonceTTL = 2 * time.Minute

// nonceStore mints and single-use-consumes challenge nonces that pair/login
// require a valid device signature over. In-memory and process-local is
// sufficient — unlike pairing codes, a nonce is always minted and consumed by
// the same running daemon within seconds, never across a restart or from a
// separate CLI process.
type nonceStore struct {
	mu     sync.Mutex
	nonces map[string]time.Time
}

func newNonceStore() *nonceStore {
	return &nonceStore{nonces: make(map[string]time.Time)}
}

// Mint generates a new nonce and records it as valid until nonceTTL elapses.
func (s *nonceStore) Mint() (string, error) {
	nonce, err := randomToken(24)
	if err != nil {
		return "", err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweep()
	s.nonces[nonce] = time.Now().Add(nonceTTL)
	return nonce, nil
}

// Consume reports whether nonce is known and unexpired, removing it either
// way (single-use).
func (s *nonceStore) Consume(nonce string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	expiry, ok := s.nonces[nonce]
	delete(s.nonces, nonce)
	return ok && time.Now().Before(expiry)
}

// sweep drops expired nonces. Called opportunistically from Mint so the map
// doesn't grow unbounded; callers hold s.mu.
func (s *nonceStore) sweep() {
	now := time.Now()
	for n, exp := range s.nonces {
		if now.After(exp) {
			delete(s.nonces, n)
		}
	}
}

// challengeRateLimit caps unauthenticated /v1/auth/challenge minting — it's
// cheap per-call but unbounded minting would grow the nonce map and could be
// used to probe for timing side channels, so it gets the same treatment as
// /v1/pair and /v1/login.
const (
	challengeRateLimitAttempts = 30
	challengeRateLimitWindow   = time.Minute
)

// proofVersion is advertised by /auth/challenge. The agent accepts the v2 proof and, for the
// browser sign-in that predates it, a signature over the bare nonce (never when pairing).
const proofVersion = "v2"

// challengeHandler mints a nonce that a device signs with its Ed25519
// identity key to prove possession of the private key on the next
// /v1/pair or /v1/login call.
func challengeHandler(nonces *nonceStore, certFingerprint string) http.HandlerFunc {
	limiter := newKeyedLimiter(challengeRateLimitAttempts, challengeRateLimitWindow)
	return func(w http.ResponseWriter, r *http.Request) {
		if !limiter.Allow(clientIP(r)) {
			writeError(w, http.StatusTooManyRequests, "RATE_LIMITED", "too many challenge requests, try again later")
			return
		}
		nonce, err := nonces.Mint()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "INTERNAL", "failed to mint nonce")
			return
		}
		// "proof" tells a client which proofs this agent verifies. v2 binds the signature to this
		// agent's certificate (security.DeviceProofMessageV2); a client that refuses to sign a bare
		// nonce needs it, and an agent without the field cannot be used by such a client.
		// certFingerprint is public (it is the TLS certificate's hash). It lets the browser companion,
		// which cannot read the certificate it is talking to, sign the same bound message.
		resp := map[string]string{"nonce": nonce, "proof": proofVersion}
		if certFingerprint != "" {
			resp["certFingerprint"] = strings.ToLower(certFingerprint)
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

// keyPinPolicy names how verifyDeviceProof treats a deviceID that already
// has a *different* key pinned than the one just presented. A bare bool
// here would let a future caller pass the wrong trust policy with nothing
// but a doc comment to catch the mistake — these two names force the
// caller to say which policy they mean.
type keyPinPolicy int

const (
	// rePinOnKeyChange silently trusts the newly-presented key (and updates
	// the pin) rather than rejecting. Used by pairHandler/registerHandler:
	// consuming a fresh one-time pairing code is itself a stronger trust
	// event (physical/terminal access to the host) than key pinning, so a
	// legitimate reinstall (new keypair, same clientID, same physical
	// access to mint a new code) re-pins rather than getting rejected —
	// see store.UpsertDevice's re-pair-reuses-the-row comment for the same
	// reasoning applied to tokens.
	rePinOnKeyChange keyPinPolicy = iota
	// rejectOnKeyChange fails the request with DEVICE_KEY_MISMATCH instead
	// of re-pinning. Used by loginHandler: a password alone is
	// comparatively low-entropy proof, so a key change on an already-known
	// device id is treated as suspicious (stolen credentials trying to
	// mint a token as an existing device) and must be resolved by
	// re-pairing instead.
	rejectOnKeyChange
)

// proofPolicy says whether verifyDeviceProof still accepts a signature over the bare nonce.
type proofPolicy int

const (
	// acceptLegacyProof also takes a bare-nonce signature, for the browser sign-in that predates v2.
	acceptLegacyProof proofPolicy = iota
	// requireBoundProof takes only the v2 proof. A bare-nonce signature can be replayed by a relay
	// that passes this agent's nonce to the phone; v2 names the certificate the phone actually saw.
	requireBoundProof
)

// verifyDeviceProof validates the device-identity signature shared by
// pairHandler, registerHandler, and loginHandler: the presented public key
// must have signed a nonce this server just minted. pinPolicy controls what
// happens when deviceID already has a different key pinned — see
// keyPinPolicy.
//
// On failure it writes the appropriate error response itself and returns a
// non-nil error; callers should return immediately without writing anything
// else.
func verifyDeviceProof(db *store.DB, nonces *nonceStore, certFingerprint, deviceID, publicKey, nonce, signature string, w http.ResponseWriter, pinPolicy keyPinPolicy, proofs proofPolicy) error {
	if publicKey == "" || nonce == "" || signature == "" {
		writeError(w, http.StatusBadRequest, "DEVICE_KEY_REQUIRED", "devicePublicKey, nonce, and signature are required")
		return errDeviceProofFailed
	}
	if !nonces.Consume(nonce) {
		writeError(w, http.StatusUnauthorized, "INVALID_NONCE", "nonce missing, already used, or expired — fetch a fresh one from /v1/auth/challenge")
		return errDeviceProofFailed
	}
	// v2 (bound to this agent's certificate) first, then the bare nonce older clients sign.
	bound := certFingerprint != "" &&
		security.VerifyDeviceSignature(publicKey, security.DeviceProofMessageV2(certFingerprint, nonce), signature)
	if !bound && (proofs == requireBoundProof || !security.VerifyDeviceSignature(publicKey, nonce, signature)) {
		writeError(w, http.StatusUnauthorized, "INVALID_SIGNATURE", "signature does not match devicePublicKey")
		return errDeviceProofFailed
	}
	if pinPolicy == rePinOnKeyChange {
		return nil
	}
	pinned, err := db.DevicePublicKeyByClientID(deviceID)
	if err != nil {
		writeInternal(w, "verify device proof", err)
		return err
	}
	if pinned != "" && pinned != publicKey {
		writeError(w, http.StatusUnauthorized, "DEVICE_KEY_MISMATCH", "this device id is already bound to a different key — re-pair via QR code to reset trust")
		return errDeviceProofFailed
	}
	return nil
}

// errDeviceProofFailed is a sentinel: verifyDeviceProof always writes the
// real error itself, callers only need to know whether to stop.
var errDeviceProofFailed = errors.New("device proof failed")
