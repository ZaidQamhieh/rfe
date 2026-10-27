package security

import (
	"os"
	"path/filepath"
	"testing"
)

// F-92: a key left behind by a crash before the certificate was written is replaced; a complete pair is kept.
func TestLoadOrCreateCert_RecoversFromKeyOnly(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "agent-key.pem"), []byte("truncated"), 0o600); err != nil {
		t.Fatal(err)
	}
	c1, err := LoadOrCreateCert(dir)
	if err != nil {
		t.Fatalf("key-only dir: %v", err)
	}
	c2, err := LoadOrCreateCert(dir)
	if err != nil {
		t.Fatal(err)
	}
	if Fingerprint(c1) != Fingerprint(c2) {
		t.Fatal("a complete pair must be reused, not regenerated")
	}
	ents, _ := os.ReadDir(dir)
	if len(ents) != 2 {
		t.Fatalf("temp files left behind: %v", ents)
	}
}
