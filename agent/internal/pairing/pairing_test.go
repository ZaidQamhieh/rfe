package pairing

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ZaidQamhieh/rfe/agent/internal/store"
)

func newTestManager(t *testing.T) *Manager {
	t.Helper()
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return New(db, "192.168.1.5:8080", "100.64.0.1:8080", "abc123fingerprint")
}

// Mint must return a single-use code of the expected length, drawn only from
// the unambiguous alphabet, plus a QR payload carrying the manager's identity.
func TestMintProducesValidCodeAndPayload(t *testing.T) {
	m := newTestManager(t)

	code, payload, err := m.Mint(DefaultTTL)
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	if len(code) != codeLen {
		t.Errorf("code length = %d, want %d", len(code), codeLen)
	}
	for _, c := range code {
		if !strings.ContainsRune(alphabet, c) {
			t.Errorf("code contains out-of-alphabet rune %q", c)
		}
	}
	if payload.PairingCode != code {
		t.Errorf("payload code %q != minted code %q", payload.PairingCode, code)
	}
	if payload.Address != "192.168.1.5:8080" || payload.TailscaleAddress != "100.64.0.1:8080" {
		t.Errorf("payload addresses not propagated: %+v", payload)
	}
	if payload.CertFingerprint != "abc123fingerprint" {
		t.Errorf("payload fingerprint = %q", payload.CertFingerprint)
	}
}

// A ttl <= 0 should fall back to DefaultTTL rather than minting an
// already-expired (or zero-lived) code.
func TestMintNonPositiveTTLFallsBackToDefault(t *testing.T) {
	m := newTestManager(t)
	code, _, err := m.Mint(0)
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	if !m.Consume(code).Valid {
		t.Error("code minted with ttl=0 was not valid; expected DefaultTTL fallback")
	}
}

// Consume enforces single use: the first redemption succeeds, the second fails,
// and an unknown code is rejected.
func TestConsumeIsSingleUse(t *testing.T) {
	m := newTestManager(t)
	code, _, err := m.Mint(DefaultTTL)
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	if !m.Consume(code).Valid {
		t.Fatal("first Consume should succeed")
	}
	if m.Consume(code).Valid {
		t.Error("second Consume should fail (single-use)")
	}
	if m.Consume("NEVERMINTED").Valid {
		t.Error("unknown code should not be consumable")
	}
}

// A code minted via Mint (non-guest) carries no jail/read-only defaults.
// A code minted via MintGuest carries the given jailRoot and forces read-only.
func TestMintGuestCarriesDefaults(t *testing.T) {
	m := newTestManager(t)

	normalCode, _, err := m.Mint(DefaultTTL)
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	info := m.Consume(normalCode)
	if !info.Valid || info.JailRoot != "" || info.ReadOnly {
		t.Errorf("expected non-guest code with no defaults, got %+v", info)
	}

	guestCode, _, err := m.MintGuest(DefaultTTL, "/home/pc/Shared")
	if err != nil {
		t.Fatalf("MintGuest: %v", err)
	}
	info = m.Consume(guestCode)
	if !info.Valid || info.JailRoot != "/home/pc/Shared" || !info.ReadOnly {
		t.Errorf("expected guest code to carry jailRoot+readOnly, got %+v", info)
	}
}

// Successive mints must not collide — the code space is large and crypto/rand
// backed, so a handful of mints should always be distinct.
func TestMintCodesAreUnique(t *testing.T) {
	m := newTestManager(t)
	seen := make(map[string]bool)
	for i := 0; i < 50; i++ {
		code, _, err := m.Mint(DefaultTTL)
		if err != nil {
			t.Fatalf("Mint #%d: %v", i, err)
		}
		if seen[code] {
			t.Fatalf("duplicate code minted: %q", code)
		}
		seen[code] = true
	}
}

// JSON must round-trip the payload fields the phone reads out of the QR code.
func TestPayloadJSONRoundTrips(t *testing.T) {
	p := QRPayload{
		Address:          "192.168.1.5:8080",
		TailscaleAddress: "100.64.0.1:8080",
		CertFingerprint:  "fp",
		PairingCode:      "CODE2345",
	}
	var got QRPayload
	if err := json.Unmarshal([]byte(p.JSON()), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got != p {
		t.Errorf("round-trip mismatch:\n got  %+v\n want %+v", got, p)
	}
}

// TailscaleAddress is omitempty — when absent it should not appear in the JSON,
// so the phone can tell "no Tailscale address" from an empty string.
func TestPayloadOmitsEmptyTailscale(t *testing.T) {
	p := QRPayload{Address: "192.168.1.5:8080", CertFingerprint: "fp", PairingCode: "CODE2345"}
	if strings.Contains(p.JSON(), "tailscaleAddress") {
		t.Errorf("empty TailscaleAddress should be omitted, got %s", p.JSON())
	}
}
