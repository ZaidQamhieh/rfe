package main

import (
	"os"
	"path/filepath"
	"testing"
)

// A pre-1.0 ~/.rfe-agent is read in place until it is moved, then renamed to ~/.rfe with its contents.
func TestLegacyDataDirIsUsedThenMoved(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("RFE_DATA_DIR", "")
	oldDir, newDir := filepath.Join(home, ".rfe-agent"), filepath.Join(home, ".rfe")
	if got := defaultDataDir(); got != newDir {
		t.Fatalf("fresh install: data dir %q, want %q", got, newDir)
	}
	if err := os.MkdirAll(oldDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(oldDir, "agent.db"), []byte("db"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := defaultDataDir(); got != oldDir {
		t.Fatalf("before the move: data dir %q, want the old %q", got, oldDir)
	}
	if err := migrateLegacyDataDir(); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(filepath.Join(newDir, "agent.db")); err != nil || string(b) != "db" {
		t.Fatalf("moved database: %q, %v", b, err)
	}
	if _, err := os.Lstat(oldDir); !os.IsNotExist(err) {
		t.Fatalf("old folder still there: %v", err)
	}
	if got := defaultDataDir(); got != newDir {
		t.Fatalf("after the move: data dir %q, want %q", got, newDir)
	}
}

// An existing ~/.rfe, a symlinked ~/.rfe-agent or $RFE_DATA_DIR leave everything where it is.
func TestLegacyDataDirNotMoved(t *testing.T) {
	cases := map[string]func(t *testing.T, home string){
		"new folder exists": func(t *testing.T, home string) {
			mkdir(t, filepath.Join(home, ".rfe-agent"))
			mkdir(t, filepath.Join(home, ".rfe"))
		},
		"old folder is a symlink": func(t *testing.T, home string) {
			target := filepath.Join(home, "elsewhere")
			mkdir(t, target)
			if err := os.Symlink(target, filepath.Join(home, ".rfe-agent")); err != nil {
				t.Skip("symlinks unavailable:", err)
			}
		},
		"RFE_DATA_DIR set": func(t *testing.T, home string) {
			mkdir(t, filepath.Join(home, ".rfe-agent"))
			t.Setenv("RFE_DATA_DIR", filepath.Join(home, "custom"))
		},
	}
	for name, prepare := range cases {
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("RFE_DATA_DIR", "")
			prepare(t, home)
			oldBefore, _ := os.Lstat(filepath.Join(home, ".rfe-agent"))
			if err := migrateLegacyDataDir(); err != nil {
				t.Fatal(err)
			}
			moveLegacyInstall(true) // must return before it looks at any service
			oldAfter, err := os.Lstat(filepath.Join(home, ".rfe-agent"))
			if err != nil || oldAfter.Mode() != oldBefore.Mode() {
				t.Fatalf("old folder changed: %v", err)
			}
		})
	}
}

func mkdir(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(p, 0o700); err != nil {
		t.Fatal(err)
	}
}
