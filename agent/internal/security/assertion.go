// Package security — account-server login assertions.
//
// An agent that opts in to the account-server role signs short-lived assertions with its own Ed25519
// key (never the TLS key). A member agent that pinned that key accepts one at POST /v1/login instead of
// a password. The assertion is a compact "payload.signature" string, both halves base64url without padding.
package security

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

const (
	// AssertionMaxTTL is the longest an assertion may live; the issuer never signs a later expiry and a
	// verifier refuses one beyond it (plus AssertionClockSkew).
	AssertionMaxTTL = 5 * time.Minute
	// AssertionClockSkew is how far apart the issuer's and the verifier's clocks may be.
	AssertionClockSkew = 2 * time.Minute

	// maxAssertionLen bounds what a verifier will parse; real assertions are a few hundred bytes.
	maxAssertionLen = 4096
	// assertionDomain separates assertion signatures from every other thing the same key could sign.
	assertionDomain = "rfe-account-assertion-v1\n"
)

// AssertionClaims is what an account server vouches for: account Sub signed in from the device holding
// DevicePublicKey, at the agent whose certificate fingerprint is Aud, answering that agent's challenge
// Nonce, until Exp (unix seconds). Iss is the fingerprint of the signing key. Epoch is the issuer's account
// epoch when it signed: a member that has seen a higher one refuses the assertion, and a member seeing a
// higher one than it had ends sessions minted under lower ones.
type AssertionClaims struct {
	Iss             string `json:"iss"`
	Sub             string `json:"sub"`
	Aud             string `json:"aud"`
	DevicePublicKey string `json:"devicePublicKey"`
	Nonce           string `json:"nonce"`
	Exp             int64  `json:"exp"`
	Epoch           int64  `json:"epoch"`
}

// KeyFingerprint is the lowercase hex SHA-256 of a raw Ed25519 public key: the short value an owner
// compares on both machines before trusting an account server.
func KeyFingerprint(pub ed25519.PublicKey) string {
	sum := sha256.Sum256(pub)
	return hex.EncodeToString(sum[:])
}

// SignAssertion encodes claims and signs them with priv, returning the "payload.signature" string.
func SignAssertion(priv ed25519.PrivateKey, claims AssertionClaims) (string, error) {
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	enc := base64.RawURLEncoding.EncodeToString(payload)
	sig := ed25519.Sign(priv, []byte(assertionDomain+enc))
	return enc + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

var errBadAssertion = errors.New("invalid assertion")

// VerifyAssertionSignature checks token's signature against pub and returns its claims. It does not
// judge the claims (audience, expiry, nonce...); the caller does that against its own state.
func VerifyAssertionSignature(pub ed25519.PublicKey, token string) (AssertionClaims, error) {
	var claims AssertionClaims
	if len(token) == 0 || len(token) > maxAssertionLen || len(pub) != ed25519.PublicKeySize {
		return claims, errBadAssertion
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return claims, errBadAssertion
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || len(sig) != ed25519.SignatureSize {
		return claims, errBadAssertion
	}
	if !ed25519.Verify(pub, []byte(assertionDomain+parts[0]), sig) {
		return claims, errBadAssertion
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil || json.Unmarshal(payload, &claims) != nil {
		return AssertionClaims{}, errBadAssertion
	}
	return claims, nil
}
