package security

import (
	"crypto/ed25519"
	"strings"
	"testing"
	"time"
)

func testClaims() AssertionClaims {
	return AssertionClaims{
		Iss: "issuer-fp", Sub: "alice", Aud: "member-fp", DevicePublicKey: "dev-key", Nonce: "n-1",
		Exp: time.Now().Add(time.Minute).Unix(),
	}
}

func TestAssertionRoundTrip(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	want := testClaims()
	token, err := SignAssertion(priv, want)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	got, err := VerifyAssertionSignature(pub, token)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if got != want {
		t.Fatalf("claims = %+v, want %+v", got, want)
	}
}

func TestAssertionRejectsWrongKeyAndTampering(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(nil)
	otherPub, _, _ := ed25519.GenerateKey(nil)
	token, err := SignAssertion(priv, testClaims())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyAssertionSignature(otherPub, token); err == nil {
		t.Error("accepted a signature from another key")
	}
	parts := strings.Split(token, ".")
	forged, _ := SignAssertion(priv, AssertionClaims{Sub: "mallory", Exp: 1})
	forgedPayload := strings.Split(forged, ".")[0]
	for name, bad := range map[string]string{
		"swapped payload": forgedPayload + "." + parts[1],
		"no separator":    parts[0],
		"empty":           "",
		"three parts":     token + ".x",
		"not base64":      "!!!.???",
		"oversized":       strings.Repeat("A", maxAssertionLen+1),
	} {
		if _, err := VerifyAssertionSignature(pub, bad); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestKeyFingerprintIsStableLowerHex(t *testing.T) {
	pub, _, _ := ed25519.GenerateKey(nil)
	fp := KeyFingerprint(pub)
	if len(fp) != 64 || fp != strings.ToLower(fp) || fp != KeyFingerprint(pub) {
		t.Fatalf("fingerprint %q is not 64 stable lowercase hex chars", fp)
	}
}
