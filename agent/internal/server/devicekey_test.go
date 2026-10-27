package server

import (
	"crypto/ed25519"
	"encoding/base64"
	"github.com/ZaidQamhieh/rfe/agent/internal/security"
	"testing"
)

// signedDeviceProof mints a fresh nonce from nonces and signs it with a new
// Ed25519 keypair, returning the (publicKey, nonce, signature) triple that
// pair/login/register expect for device-identity proof-of-possession.
// testProofFp is the certificate fingerprint tests configure on the agent.
const testProofFp = "sha256-fp"

func signedDeviceProof(t *testing.T, nonces *nonceStore) (publicKey, nonce, signature string) {
	t.Helper()
	return signedDeviceProofFor(t, nonces, testProofFp)
}

// signedDeviceProofFor signs the v2 proof bound to certificate fingerprint fp.
func signedDeviceProofFor(t *testing.T, nonces *nonceStore, fp string) (publicKey, nonce, signature string) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	nonce, err = nonces.Mint()
	if err != nil {
		t.Fatalf("mint nonce: %v", err)
	}
	sig := ed25519.Sign(priv, []byte(security.DeviceProofMessageV2(fp, nonce)))
	return base64.StdEncoding.EncodeToString(pub), nonce, base64.StdEncoding.EncodeToString(sig)
}
