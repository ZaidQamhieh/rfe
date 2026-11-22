// Package pairing is agent-next's pairing, registration, login and device
// token service. It keeps the old agent's trust model: the phone pins the
// agent's self-signed certificate fingerprint when it pairs; a device is paired
// by a one-time code (owner, or guest with a jail) or by an approve-on-PC
// request (short match code shown on the PC, phone polls with a secret nonce);
// every device holds a revocable bearer token of which only the SHA-256 hash is
// stored; a device paired by code or approval starts browse-only, and a device
// that proved the account password (login or registration) bypasses the file
// grants. The package has no HTTP: it takes plain values and returns *Error
// values that carry the OpenAPI error code.
package pairing

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/next/audit"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/store"
)

// Code and token sizes. The code is eight characters of a 32-letter alphabet
// without look-alikes (about 40 bits), so the rate limits below are what keep
// it from being guessed.
const (
	CodeLength   = 8
	CodeAlphabet = "23456789ABCDEFGHJKLMNPQRSTUVWXYZ"

	tokenBytes          = 32 // bearer token: 256 random bits, hex encoded
	requestIDBytes      = 16
	challengeNonceBytes = 24
	clientNonceBytes    = 16 // the phone's nonce; its SHA-256 is the commitment
	uuidBytes           = 16
)

// Lifetimes. A code stays valid for DefaultCodeTTL unless the owner asks for
// another, up to MaxCodeTTL; a pair request must be answered within
// PairRequestTTL; a challenge nonce is single-use within NonceTTL.
const (
	DefaultCodeTTL = 60 * time.Minute
	MaxCodeTTL     = 30 * 24 * time.Hour
	PairRequestTTL = 2 * time.Minute
	NonceTTL       = 2 * time.Minute
)

// Rate limits (attempts per window per source address; the shared window that
// caps all sources together is GlobalRateFactor times larger).
const (
	RateWindow              = time.Minute
	GlobalRateFactor        = 4
	PairAttempts            = 10
	RegisterAttempts        = 10
	LoginAttempts           = 10
	PairRequestAttempts     = 6
	PairPollAttempts        = 120 // polling every ~2 s for the request's lifetime is legitimate
	ChallengeAttempts       = 30
	AccountFailures         = 30 // failed logins per account from any address
	AccountFailureWindow    = 15 * time.Minute
	maxLimiterKeys          = 10000
	maxOutstandingNonces    = 4096
	maxPairRequestBody      = 8 << 10
	MaxGenerateBodyBytes    = 1 << 20
	maxCodeInputBytes       = 64
	maxUsernameBytes        = 128
	MinPasswordRunes        = 8
	maxLabelRunes           = 40
	pairRequestRetention    = 10 * time.Minute
	unnamedDeviceLabel      = "unnamed-device"
	maxCodeMintAttempts     = 3
	deviceProofVersion      = "v2"
	pairRequestMaxPending   = 3
	pairRequestMaxPerIP     = 3
	pairRequestMaxCommitted = 30
)

// Auditor records account and device events; *audit.Log satisfies it. A nil
// Auditor records nothing.
type Auditor interface {
	Try(ev audit.Event)
}

// Passwords hashes and checks account passwords. The default is the old
// agent's scheme, so imported accounts keep working.
type Passwords struct {
	Hash     func(password string) (string, error)
	Verify   func(hash, password string) bool
	Burn     func(password string) // spend the time of a check when no account exists
	MaxBytes int                   // longest accepted password
}

// Options configure a Service. Store is required; every other field has a
// default.
type Options struct {
	Store *store.DB
	// Fingerprint is the agent's TLS certificate SHA-256 (hex). Device proofs
	// are bound to it, and the match code is keyed by it.
	Fingerprint      string
	AgentName        string
	Address          string
	TailscaleAddress string

	Now             func() time.Time // default time.Now
	Rand            io.Reader        // default crypto/rand
	Audit           Auditor
	OnPairRequest   func(Prompt) // asks the owner to approve; may be nil
	Passwords       Passwords
	VerifySignature func(pubKeyB64, message, sigB64 string) bool
	ProofMessage    func(fingerprint, nonce string) string
}

// Service is the pairing service. It is safe for concurrent use.
type Service struct {
	o Options

	pairLim, registerLim, requestLim, revealLim *dualLimiter
	loginLim, pollLim, challengeLim             *limiter
	accountFails                                *limiter

	nmu    sync.Mutex
	nonces map[string]time.Time
}

// New builds a Service, fills the defaults and makes sure the store has its
// pairing tables.
func New(o Options) (*Service, error) {
	if o.Store == nil {
		return nil, fmt.Errorf("pairing: a store is required")
	}
	if err := o.Store.EnsurePairingSchema(); err != nil {
		return nil, err
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Rand == nil {
		o.Rand = rand.Reader
	}
	o.Fingerprint = strings.ToLower(o.Fingerprint)
	o.Passwords = o.Passwords.withDefaults()
	if o.VerifySignature == nil {
		o.VerifySignature = defaultVerifySignature
	}
	if o.ProofMessage == nil {
		o.ProofMessage = defaultProofMessage
	}
	now := o.Now
	return &Service{
		o:            o,
		pairLim:      newDualLimiter(PairAttempts, RateWindow, GlobalRateFactor, maxLimiterKeys, now),
		registerLim:  newDualLimiter(RegisterAttempts, RateWindow, GlobalRateFactor, maxLimiterKeys, now),
		requestLim:   newDualLimiter(PairRequestAttempts, RateWindow, GlobalRateFactor, maxLimiterKeys, now),
		revealLim:    newDualLimiter(PairRequestAttempts*GlobalRateFactor, RateWindow, GlobalRateFactor, maxLimiterKeys, now),
		loginLim:     newLimiter(LoginAttempts, RateWindow, maxLimiterKeys, now),
		pollLim:      newLimiter(PairPollAttempts, RateWindow, maxLimiterKeys, now),
		challengeLim: newLimiter(ChallengeAttempts, RateWindow, maxLimiterKeys, now),
		accountFails: newLimiter(AccountFailures, AccountFailureWindow, maxLimiterKeys, now),
		nonces:       make(map[string]time.Time),
	}, nil
}

// unixNow is the injected clock in Unix seconds, the unit the store keeps.
func (s *Service) unixNow() int64 { return s.o.Now().Unix() }

// audit records an event; a nil Auditor drops it.
func (s *Service) audit(action, actor, target, detail string) {
	if s.o.Audit != nil {
		s.o.Audit.Try(audit.Event{Action: action, Actor: actor, Target: target, Detail: detail})
	}
}

// randomBytes reads n bytes from the injected random source.
func (s *Service) randomBytes(n int) ([]byte, error) {
	b := make([]byte, n)
	if _, err := io.ReadFull(s.o.Rand, b); err != nil {
		return nil, fmt.Errorf("pairing: random source: %w", err)
	}
	return b, nil
}

// randomHex returns n random bytes as lowercase hex.
func (s *Service) randomHex(n int) (string, error) {
	b, err := s.randomBytes(n)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// newDeviceID returns a random version-4 UUID string.
func (s *Service) newDeviceID() (string, error) {
	b, err := s.randomBytes(uuidBytes)
	if err != nil {
		return "", err
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	h := hex.EncodeToString(b)
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:], nil
}

// newCredentials mints a device id and a bearer token. The token is returned
// once; only its hash goes to the store.
func (s *Service) newCredentials() (id, token string, err error) {
	if id, err = s.newDeviceID(); err != nil {
		return "", "", err
	}
	token, err = s.randomHex(tokenBytes)
	return id, token, err
}

// hashCode is the stored form of a pairing code. The database looks a code up
// by this hash, so the comparison never runs over the secret itself.
func hashCode(code string) string {
	return store.HashToken(code)
}

// sanitizeLabel bounds and cleans a device name that an unauthenticated peer
// chose: control characters are dropped and it is cut to maxLabelRunes.
func sanitizeLabel(s string) string {
	out := make([]rune, 0, maxLabelRunes)
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			continue
		}
		out = append(out, r)
		if len(out) == maxLabelRunes {
			break
		}
	}
	if len(out) == 0 {
		return unnamedDeviceLabel
	}
	return string(out)
}

// PairResult is what a successful pairing, registration or login hands back.
// DeviceToken is the only copy of the token; the caller decides how it travels
// (JSON body, or an HttpOnly cookie for the browser companion).
type PairResult struct {
	DeviceToken      string
	DeviceID         string
	AgentName        string
	CertFingerprint  string
	Address          string
	TailscaleAddress string
}

// result builds a PairResult for a device and its token.
func (s *Service) result(deviceID, token string) PairResult {
	return PairResult{
		DeviceToken: token, DeviceID: deviceID, AgentName: s.o.AgentName,
		CertFingerprint: s.o.Fingerprint, Address: s.o.Address, TailscaleAddress: s.o.TailscaleAddress,
	}
}
