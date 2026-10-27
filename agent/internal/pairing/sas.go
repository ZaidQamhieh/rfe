package pairing

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
)

// SAS is the short match code both sides derive during a pair request, so the
// owner can tell at the PC that the phone reached this agent and not a
// machine relaying for it. It is keyed by the TLS certificate fingerprint the
// caller actually saw, so a relay presenting its own certificate makes the two
// sides derive different codes. The fingerprint itself is never displayed.
//
// The client nonce is fixed (and sent) before the request id exists, and the
// certificate is fixed before either, so a relay cannot search for a
// certificate that yields a chosen code.
//
// Format: 8 decimal digits, "1234 5678".
func SAS(fingerprintHex, clientNonceHex, requestID string) (string, error) {
	fp, err := hex.DecodeString(fingerprintHex)
	if err != nil || len(fp) != sha256.Size {
		return "", errors.New("bad fingerprint")
	}
	nonce, err := hex.DecodeString(clientNonceHex)
	if err != nil || len(nonce) != 16 {
		return "", errors.New("bad client nonce")
	}
	mac := hmac.New(sha256.New, fp)
	mac.Write([]byte("rfe-pair-sas\n"))
	mac.Write(nonce)
	mac.Write([]byte(requestID))
	n := binary.BigEndian.Uint64(mac.Sum(nil)[:8]) % 100_000_000
	return fmt.Sprintf("%04d %04d", n/10_000, n%10_000), nil
}
