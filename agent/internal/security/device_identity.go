// Package security — per-device Ed25519 identity.
//
// Alongside the account password, every device (phone install, browser
// profile) holds a permanent Ed25519 keypair generated on first use. Pair and
// login prove possession of the private key by signing a server-issued
// nonce, and the resulting public key is pinned to the device row (see
// store.UpsertDevice) — so a leaked bearer token alone can't let an attacker
// re-mint a token as that device from a different key, and a device's key
// changing unexpectedly (id reused with a new key) is rejected rather than
// silently re-trusted. This mirrors SSH host-key pinning, applied to the
// client side.
package security

import (
	"crypto/ed25519"
	"encoding/base64"
	"strings"
)

// DeviceProofMessageV2 is what a v2 client signs instead of the bare nonce: the agent's own TLS
// certificate fingerprint (lowercase hex SHA-256) and the nonce, under a fixed label. A signature
// made for one agent is then worthless at another, so a malicious agent cannot pass a nonce it
// fetched from a second agent to the device and replay the signature there.
func DeviceProofMessageV2(certFingerprint, nonce string) string {
	return "rfe-device-proof-v2\n" + strings.ToLower(certFingerprint) + "\n" + nonce
}

// VerifyDeviceSignature reports whether sig is a valid Ed25519 signature over
// message by the key encoded in pubKeyB64 (standard base64, raw 32-byte
// Ed25519 public key). Returns false (not an error) for any malformed input —
// callers treat that identically to "signature didn't verify".
func VerifyDeviceSignature(pubKeyB64, message, sigB64 string) bool {
	pubKey, err := base64.StdEncoding.DecodeString(pubKeyB64)
	if err != nil || len(pubKey) != ed25519.PublicKeySize {
		return false
	}
	sig, err := base64.StdEncoding.DecodeString(sigB64)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return false
	}
	return ed25519.Verify(ed25519.PublicKey(pubKey), []byte(message), sig)
}
