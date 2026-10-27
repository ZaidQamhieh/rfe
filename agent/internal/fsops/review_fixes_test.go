package fsops

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestIsUnder_FilesystemRoot(t *testing.T) {
	sep := string(filepath.Separator)
	if !isUnder(sep+"a"+sep+"b", sep) {
		t.Fatalf("path below the filesystem root must be under it")
	}
	if isUnder(sep+"ab", sep+"a") {
		t.Fatalf("sibling with a shared prefix must not be under the root")
	}
}

func TestCopy_FolderIntoItselfRejected(t *testing.T) {
	ops, root := setupJail(t)
	src := filepath.Join(root, "a")
	sub := filepath.Join(src, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, dup := range []bool{false, true} {
		for _, dest := range []string{sub, src} {
			res := ops.Copy([]string{src}, dest, dup, false)
			if len(res) != 1 || res[0].OK {
				t.Fatalf("copy into itself (dest=%s dup=%v) must fail: %+v", dest, dup, res)
			}
		}
	}
	if _, err := os.Stat(filepath.Join(sub, "a")); !os.IsNotExist(err) {
		t.Fatalf("no nested copy may be created, stat err: %v", err)
	}
}

func TestCopy_DuplicateLongNameTerminates(t *testing.T) {
	ops, root := setupJail(t)
	name := strings.Repeat("x", 254)
	src := filepath.Join(root, name)
	if err := os.WriteFile(src, []byte("d"), 0o644); err != nil {
		t.Skipf("filesystem rejects long names: %v", err)
	}
	done := make(chan struct{})
	go func() {
		ops.Copy([]string{src}, root, true, false)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("duplicate copy of a 254-byte name did not terminate")
	}
}

func TestUniqueTrashName_LongNameTerminates(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(trashFilesDir(dir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(trashInfoDir(dir), 0o755); err != nil {
		t.Fatal(err)
	}
	name := strings.Repeat("x", 254)
	if err := os.WriteFile(filepath.Join(trashFilesDir(dir), name), nil, 0o644); err != nil {
		t.Skipf("filesystem rejects long names: %v", err)
	}
	done := make(chan struct{})
	go func() {
		uniqueTrashName(dir, name)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("uniqueTrashName did not terminate")
	}
}

func TestResolve_OutsideRootsRejectedWithoutTouchingDisk(t *testing.T) {
	ops, _ := setupJail(t)
	outside := t.TempDir()
	file := filepath.Join(outside, "candidate")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	// Below a regular file the OS would answer ENOTDIR (a distinct error);
	// the roots check must answer first, identically for existing and
	// missing paths.
	for _, p := range []string{filepath.Join(file, "x"), filepath.Join(outside, "missing", "x")} {
		_, err := ops.Resolve(p)
		if !errors.Is(err, ErrForbidden) {
			t.Fatalf("%s: want ErrForbidden, got %v", p, err)
		}
	}
}

func TestResolve_ProtectedDirDeniedUnderBroadRoot(t *testing.T) {
	home := t.TempDir()
	data := filepath.Join(home, ".rfe")
	if err := os.MkdirAll(data, 0o700); err != nil {
		t.Fatal(err)
	}
	key := filepath.Join(data, "agent-key.pem")
	if err := os.WriteFile(key, []byte("k"), 0o600); err != nil {
		t.Fatal(err)
	}
	ops := New([]string{home}, false)
	if _, err := ops.Resolve(key); err != nil {
		t.Fatalf("before protecting, the broad root reaches it: %v", err)
	}
	ProtectDir(data)
	t.Cleanup(func() { protectedMu.Lock(); protectedDirs = nil; protectedMu.Unlock() })
	for _, p := range []string{key, data, filepath.Join(data, "new")} {
		if _, err := ops.Resolve(p); !errors.Is(err, ErrForbidden) {
			t.Fatalf("%s must be forbidden, got %v", p, err)
		}
	}
	if _, err := ops.Resolve(filepath.Join(home, "ok.txt")); err != nil {
		t.Fatalf("sibling paths stay reachable: %v", err)
	}
	unrestricted := New(nil, false)
	if _, err := unrestricted.Resolve(key); !errors.Is(err, ErrForbidden) {
		t.Fatalf("also protected with no roots, got %v", err)
	}
}
