package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZaidQamhieh/rfe/agent/internal/settings"
	"github.com/ZaidQamhieh/rfe/agent/internal/store"
)

func TestNormalizeSetupRootDefaultsToDedicatedFolderAndRequiresDirectory(t *testing.T) {
	home := t.TempDir()
	defaultRoot := defaultSetupRoot(home)
	if err := os.Mkdir(defaultRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	got, err := normalizeSetupRoot("", home)
	if err != nil {
		t.Fatalf("default setup root: %v", err)
	}
	if got != defaultRoot {
		t.Fatalf("default setup root = %q, want %q", got, defaultRoot)
	}

	file := filepath.Join(home, "not-a-folder")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := normalizeSetupRoot(file, home); err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("file accepted as setup root: %v", err)
	}
	if _, err := normalizeSetupRoot(filepath.Join(home, "missing"), home); err == nil {
		t.Fatal("missing directory accepted as setup root")
	}
}

func TestPromptSetupRootUsesDedicatedFolderOnBlankInput(t *testing.T) {
	var output strings.Builder
	defaultRoot := filepath.Join("/home/example", "RFE Files")
	got, err := promptSetupRoot(strings.NewReader("\n"), &output, defaultRoot)
	if err != nil || got != defaultRoot {
		t.Fatalf("blank prompt result = (%q, %v)", got, err)
	}
	if !strings.Contains(output.String(), defaultRoot) {
		t.Fatalf("prompt did not show default root: %q", output.String())
	}
	got, err = promptSetupRoot(strings.NewReader("/mnt/shared\n"), io.Discard, defaultRoot)
	if err != nil || got != "/mnt/shared" {
		t.Fatalf("explicit prompt result = (%q, %v)", got, err)
	}
}

func TestEnsureDefaultShareRootCreatesOnlyTheDedicatedFolder(t *testing.T) {
	home := t.TempDir()
	root := defaultSetupRoot(home)
	if err := ensureDefaultShareRoot(root); err != nil {
		t.Fatalf("create default share root: %v", err)
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		t.Fatalf("default share root stat = (%v, %v)", info, err)
	}
	if _, err := os.Stat(filepath.Join(home, ".rfe")); !os.IsNotExist(err) {
		t.Fatalf("creating share root touched private agent data path: %v", err)
	}
}

func TestSetupPersistsRestrictedRootAndRefusesExistingData(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("RFE_DATA_DIR", "")
	root := filepath.Join(home, "Shared")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}

	output, err := captureSetupOutput(t, func() error {
		return cmdSetup([]string{"-root", root, "-no-install"})
	})
	if err != nil {
		t.Fatalf("setup: %v\n%s", err, output)
	}
	if !strings.Contains(output, "Initial folder access:") || !strings.Contains(output, "Setup saved.") {
		t.Fatalf("setup output = %q", output)
	}

	db, err := store.Open(defaultDataDir())
	if err != nil {
		t.Fatal(err)
	}
	configured, err := settings.Load(db, false, nil, hostName())
	if err != nil {
		db.Close()
		t.Fatal(err)
	}
	if got := configured.Roots(); len(got) != 1 || got[0] != root {
		db.Close()
		t.Fatalf("persisted roots = %#v, want only %q", got, root)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	otherRoot := filepath.Join(home, "Elsewhere")
	if err := os.Mkdir(otherRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := captureSetupOutput(t, func() error {
		return cmdSetup([]string{"-root", otherRoot, "-no-install"})
	}); err == nil || !strings.Contains(err.Error(), "did not change it") {
		t.Fatalf("setup changed an existing installation: %v", err)
	}
	if _, err := os.Stat(defaultSetupRoot(home)); !os.IsNotExist(err) {
		t.Fatalf("setup created its default folder before refusing existing data: %v", err)
	}

	db, err = store.Open(defaultDataDir())
	if err != nil {
		t.Fatal(err)
	}
	configured, err = settings.Load(db, false, nil, hostName())
	if err != nil {
		db.Close()
		t.Fatal(err)
	}
	defer db.Close()
	if got := configured.Roots(); len(got) != 1 || got[0] != root {
		t.Fatalf("existing roots after rejected setup = %#v, want only %q", got, root)
	}
	t.Setenv("RFE_DATA_DIR", filepath.Join(home, "custom-agent-data"))
	if _, err := captureSetupOutput(t, func() error {
		return cmdSetup([]string{"-root", root, "-no-install"})
	}); err == nil || !strings.Contains(err.Error(), "standard data folder") {
		t.Fatalf("setup accepted a data path the installed service would not reuse: %v", err)
	}
}

func captureSetupOutput(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = write
	callErr := fn()
	os.Stdout = old
	if err := write.Close(); err != nil {
		t.Fatal(err)
	}
	output, err := io.ReadAll(read)
	if err != nil {
		t.Fatal(err)
	}
	if err := read.Close(); err != nil {
		t.Fatal(err)
	}
	return string(output), callErr
}
