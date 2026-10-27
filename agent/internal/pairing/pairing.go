// Package pairing manages the device pairing flow.
//
// Pairing codes are stored in the SQLite DB (not daemon memory) so the
// `rfe pair` CLI can mint a code that the already-running daemon will
// accept, with no restart. A code is valid for a chosen TTL and is single-use.
package pairing

import (
	"crypto/rand"
	"encoding/json"
	"math/big"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/store"
)

const (
	codeLen = 8 // characters in the one-time code
	// Alphabet chosen to be easy to type: no 0/O, 1/I/l confusion.
	alphabet = "23456789ABCDEFGHJKLMNPQRSTUVWXYZ"
)

// DefaultTTL is how long a newly minted pairing code stays valid.
const DefaultTTL = 60 * time.Minute

// Manager mints and validates pairing codes, backed by the store.
type Manager struct {
	db          *store.DB
	lan         string
	tailscale   string
	fingerprint string
}

// QRPayload is the JSON embedded in the QR code the phone scans.
type QRPayload struct {
	Address          string `json:"address"`
	TailscaleAddress string `json:"tailscaleAddress,omitempty"`
	CertFingerprint  string `json:"certFingerprint"`
	PairingCode      string `json:"pairingCode"`
}

// New creates a Manager. It does not mint a code — codes are minted on demand
// (by `rfe pair`), so the daemon no longer rotates a code on every
// restart. lanAddress/tailscaleAddress are the agent's reachable host:port
// pairs; fingerprint is the TLS cert's SHA-256 hex.
func New(db *store.DB, lanAddress, tailscaleAddress, fingerprint string) *Manager {
	return &Manager{
		db:          db,
		lan:         lanAddress,
		tailscale:   tailscaleAddress,
		fingerprint: fingerprint,
	}
}

// Mint generates a new single-use pairing code valid for ttl, persists it, and
// returns the code together with the QR payload the phone should scan.
func (m *Manager) Mint(ttl time.Duration) (string, QRPayload, error) {
	return m.mint(ttl, "", false, false)
}

// MintOwner generates a single-use code that can also create the owner
// account (POST /v1/register). Ordinary codes only pair a device, so a
// code handed to a guest cannot be turned into an owner account.
func (m *Manager) MintOwner(ttl time.Duration) (string, QRPayload, error) {
	return m.mint(ttl, "", false, true)
}

// MintGuest generates a single-use pairing code that pre-configures the
// device created when it's redeemed as read-only and jailed to jailRoot —
// the Guest role: the resulting device can never be full-access, and (like
// every device) can't change its own jail/read-only state, only an admin can.
func (m *Manager) MintGuest(ttl time.Duration, jailRoot string) (string, QRPayload, error) {
	return m.mint(ttl, jailRoot, true, false)
}

func (m *Manager) mint(ttl time.Duration, jailRoot string, readOnly, canRegister bool) (string, QRPayload, error) {
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	code, err := randomCode(codeLen)
	if err != nil {
		return "", QRPayload{}, err
	}
	if err := m.db.CreatePairingCodeFor(code, time.Now().Add(ttl), jailRoot, readOnly, canRegister); err != nil {
		return "", QRPayload{}, err
	}
	return code, m.Payload(code), nil
}

// Payload builds the QR payload for a given code.
func (m *Manager) Payload(code string) QRPayload {
	return QRPayload{
		Address:          m.lan,
		TailscaleAddress: m.tailscale,
		CertFingerprint:  m.fingerprint,
		PairingCode:      code,
	}
}

// PayloadJSON returns the QR payload as the JSON string embedded in the QR code.
func (p QRPayload) JSON() string {
	b, _ := json.Marshal(p)
	return string(b)
}

// Consume validates code and clears it (single-use), returning its guest-mode
// defaults (if any) to apply to the device created on success.
// Peek reports what code would allow without consuming it.
func (m *Manager) Peek(code string) store.PairingCodeInfo {
	return m.db.PeekPairingCode(code)
}

func (m *Manager) Consume(code string) store.PairingCodeInfo {
	return m.db.ConsumePairingCode(code)
}

func randomCode(n int) (string, error) {
	b := make([]byte, n)
	for i := range b {
		idx, err := rand.Int(rand.Reader, big.NewInt(int64(len(alphabet))))
		if err != nil {
			return "", err
		}
		b[i] = alphabet[idx.Int64()]
	}
	return string(b), nil
}
