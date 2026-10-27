package pairing

import (
	"strings"
	"testing"
)

const (
	fpA   = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	fpB   = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	nonce = "00112233445566778899aabbccddeeff"
)

func TestSASIsDeterministicAndKeyedByFingerprint(t *testing.T) {
	a1, err := SAS(fpA, nonce, "req1")
	if err != nil {
		t.Fatal(err)
	}
	a2, _ := SAS(fpA, nonce, "req1")
	if a1 != a2 {
		t.Fatalf("not deterministic: %q vs %q", a1, a2)
	}
	if len(a1) != 9 || a1[4] != ' ' || strings.Trim(a1, "0123456789 ") != "" {
		t.Fatalf("bad format %q", a1)
	}
	if b, _ := SAS(fpB, nonce, "req1"); b == a1 {
		t.Fatal("a different certificate must give a different code")
	}
	if c, _ := SAS(fpA, nonce, "req2"); c == a1 {
		t.Fatal("a different request must give a different code")
	}
}

// Pinned vector: the phone app computes the same value in TypeScript
// (mobile/src/features/pairing/sas.test.ts).
func TestSASVector(t *testing.T) {
	got, _ := SAS(fpA, nonce, "req1")
	if got != "7298 4153" {
		t.Fatalf("got %q want 7298 4153", got)
	}
}

func TestSASRejectsBadInput(t *testing.T) {
	if _, err := SAS("zz", nonce, "r"); err == nil {
		t.Fatal("bad fingerprint accepted")
	}
	if _, err := SAS(fpA, "00", "r"); err == nil {
		t.Fatal("bad nonce accepted")
	}
}
