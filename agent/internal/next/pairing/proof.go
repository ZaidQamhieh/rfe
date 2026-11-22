package pairing

import "github.com/ZaidQamhieh/rfe/agent/internal/security"

// defaultVerifySignature checks an Ed25519 signature with the old agent's code.
func defaultVerifySignature(pubKeyB64, message, sigB64 string) bool {
	return security.VerifyDeviceSignature(pubKeyB64, message, sigB64)
}

// defaultProofMessage is the v2 message a device signs: the agent's own
// certificate fingerprint plus the nonce, so a signature made for one agent is
// useless at another.
func defaultProofMessage(fingerprint, nonce string) string {
	return security.DeviceProofMessageV2(fingerprint, nonce)
}

// withDefaults fills unset password functions with the old agent's scheme.
func (p Passwords) withDefaults() Passwords {
	if p.Hash == nil {
		p.Hash = security.HashPassword
	}
	if p.Verify == nil {
		p.Verify = security.VerifyPassword
	}
	if p.Burn == nil {
		p.Burn = security.BurnPasswordCheck
	}
	if p.MaxBytes <= 0 {
		p.MaxBytes = security.MaxPasswordBytes
	}
	return p
}

// Challenge is what GET-ing a device-proof challenge returns: a single-use
// nonce the device must sign, the proof version, and the certificate
// fingerprint (public) the signature is bound to.
type Challenge struct {
	Nonce           string
	Proof           string
	CertFingerprint string
}

// Challenge mints a nonce for the next pair, login or register call, within
// the per-source challenge limit.
func (s *Service) Challenge(remote string) (Challenge, error) {
	if !s.challengeLim.Allow(sourceKey(remote)) {
		return Challenge{}, ErrRateLimited
	}
	nonce, err := s.randomHex(challengeNonceBytes)
	if err != nil {
		return Challenge{}, err
	}
	now := s.o.Now()
	s.nmu.Lock()
	defer s.nmu.Unlock()
	if len(s.nonces) >= maxOutstandingNonces {
		for n, exp := range s.nonces {
			if !now.Before(exp) {
				delete(s.nonces, n)
			}
		}
		if len(s.nonces) >= maxOutstandingNonces {
			return Challenge{}, ErrRateLimited
		}
	}
	s.nonces[nonce] = now.Add(NonceTTL)
	return Challenge{Nonce: nonce, Proof: deviceProofVersion, CertFingerprint: s.o.Fingerprint}, nil
}

// consumeNonce reports whether nonce was minted here and is unexpired, and
// forgets it either way, so it works once.
func (s *Service) consumeNonce(nonce string) bool {
	s.nmu.Lock()
	defer s.nmu.Unlock()
	exp, ok := s.nonces[nonce]
	delete(s.nonces, nonce)
	return ok && s.o.Now().Before(exp)
}

// keyPolicy says what to do when a device id already has another key pinned.
type keyPolicy int

const (
	// rePinOnKeyChange trusts the new key: a one-time code or an owner approval
	// is a stronger event than the pinned key (a reinstall gets a new key).
	rePinOnKeyChange keyPolicy = iota
	// rejectOnKeyChange refuses a key change: a password alone is weak proof, so
	// a changed key under a known device id must be settled by re-pairing.
	rejectOnKeyChange
)

// Proof is the device-identity proof sent with pair, register and login: the
// device's Ed25519 public key (base64) and its signature over the nonce.
type Proof struct {
	DeviceID  string
	PublicKey string
	Nonce     string
	Signature string
}

// verifyProof checks that the presented key signed a nonce this agent just
// minted (bound to its certificate fingerprint) and applies the key policy. It
// runs before a code is spent: a stale nonce is the phone's slip, not the
// code's fault.
func (s *Service) verifyProof(p Proof, policy keyPolicy) error {
	if p.PublicKey == "" || p.Nonce == "" || p.Signature == "" {
		return ErrDeviceKeyRequired
	}
	if !s.consumeNonce(p.Nonce) {
		return ErrInvalidNonce
	}
	if s.o.Fingerprint == "" || !s.o.VerifySignature(p.PublicKey, s.o.ProofMessage(s.o.Fingerprint, p.Nonce), p.Signature) {
		return ErrInvalidSignature
	}
	if policy == rePinOnKeyChange {
		return nil
	}
	d, err := s.o.Store.DeviceByClientID(p.DeviceID)
	if err != nil {
		return err
	}
	if d != nil && d.PublicKey != "" && d.PublicKey != p.PublicKey {
		return ErrDeviceKeyMismatch
	}
	return nil
}
