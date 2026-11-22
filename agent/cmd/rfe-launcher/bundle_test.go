package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ZaidQamhieh/rfe/agent/internal/bundle"
	"github.com/ZaidQamhieh/rfe/agent/internal/launcher"
)

func writeSrc(t *testing.T, files ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("content of "+f), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestAssembleFreshRootAndUpgrade(t *testing.T) {
	ext := ""
	if runtime.GOOS == "windows" {
		ext = ".exe"
	}
	out := t.TempDir()
	src := writeSrc(t, "rfe"+ext, "rfe-indexd"+ext, "rfe-sidecars.txt")
	if err := assemble(src, out, "1.0.0", runtime.GOOS, runtime.GOARCH, false); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(out, "versions", "1.0.0")
	m, err := bundle.Verify(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Files) != 3 { // rfe, rfe-indexd (rfe-thumbd absent), rfe-sidecars.txt
		t.Fatalf("files = %+v", m.Files)
	}
	st, err := launcher.NewStore(out).Read()
	if err != nil || st.Current != "1.0.0" || st.Pending != "" {
		t.Fatalf("state = %+v, %v", st, err)
	}
	if left, _ := os.ReadDir(filepath.Join(out, "staging")); len(left) != 0 {
		t.Fatalf("staging not emptied: %v", left)
	}

	// A second bundle on an existing root changes no state unless asked.
	if err := assemble(src, out, "1.1.0", runtime.GOOS, runtime.GOARCH, false); err != nil {
		t.Fatal(err)
	}
	if st, _ := launcher.NewStore(out).Read(); st.Pending != "" {
		t.Fatalf("pending set without -pending: %+v", st)
	}
	if err := assemble(src, out, "1.2.0", runtime.GOOS, runtime.GOARCH, true); err != nil {
		t.Fatal(err)
	}
	if st, _ := launcher.NewStore(out).Read(); st.Pending != "1.2.0" || st.Current != "1.0.0" {
		t.Fatalf("state = %+v", st)
	}
	// The same version twice is refused rather than overwritten.
	if err := assemble(src, out, "1.2.0", runtime.GOOS, runtime.GOARCH, true); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("err = %v", err)
	}
}

func TestAssembleNeedsAgentAndSafeVersion(t *testing.T) {
	out := t.TempDir()
	if err := assemble(writeSrc(t, "rfe-indexd"), out, "1.0.0", "linux", "amd64", false); err == nil {
		t.Fatal("assembled without an agent binary")
	}
	if _, err := os.Stat(filepath.Join(out, "versions", "1.0.0")); err == nil {
		t.Fatal("half-built version left in versions/")
	}
	if err := assemble(writeSrc(t, "rfe"), out, "../evil", "linux", "amd64", false); err == nil {
		t.Fatal("accepted an unsafe version")
	}
}
