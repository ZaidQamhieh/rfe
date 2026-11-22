package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZaidQamhieh/rfe/agent/internal/hostupdate"
)

func TestSignedManifestVerifiesWithTheGeneratedKey(t *testing.T) {
	dir := t.TempDir()
	key := filepath.Join(dir, "k.key")
	if err := generate(key); err != nil {
		t.Fatal(err)
	}
	priv, err := loadKey(key)
	if err != nil {
		t.Fatal(err)
	}
	if err := generate(key); err == nil {
		t.Fatal("generate overwrote an existing key")
	}
	bundle := filepath.Join(dir, "rfe-linux-amd64.tar.gz")
	os.WriteFile(bundle, []byte("archive bytes"), 0o644)
	out := filepath.Join(dir, "out")
	if err := run([]string{"-key", key, "-version", "1.2.0", "-base-url", "https://x.test/r/", "-out", out, "linux/amd64=" + bundle}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(out, "manifest.json"))
	sig, _ := os.ReadFile(filepath.Join(out, "manifest.json.sig"))
	keys := hostupdate.Keys{Primary: priv.Public().(ed25519.PublicKey)}
	if err := keys.Verify(data, sig); err != nil {
		t.Fatal(err)
	}
	rel, err := hostupdate.ParseRelease(data)
	if err != nil || rel.Bundles[0].URL != "https://x.test/r/rfe-linux-amd64.tar.gz" {
		t.Fatalf("release %+v, err %v", rel, err)
	}
	if err := keys.Verify(append(data, ' '), sig); err == nil {
		t.Fatal("a changed manifest verified")
	}
	if _, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sig))); err != nil {
		t.Fatal(err)
	}
}

func TestRealPrimaryKeyFileFormatLoads(t *testing.T) {
	p := filepath.Join(os.Getenv("HOME"), ".rfe-keystore", "update-signing", "primary.key")
	if _, err := os.Stat(p); err != nil {
		t.Skip("owner key not on this machine")
	}
	if _, err := loadKey(p); err != nil {
		t.Fatal(err)
	}
}
