//go:build !windows

package fsops

// Pass-2 review fix: a failed Copy must be able to remove its partial
// destination even when the source holds read-only folders.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A read-only subfolder copied before a failing sibling must not stop the
// cleanup: the destination is absent afterwards.
func TestPass2Review_FailedCopyWithReadOnlySubfolderLeavesNoDestination(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory modes and reads a 0000 file")
	}
	ops, root := setupJail(t)
	src := filepath.Join(root, "src", "d")
	pass2Write(t, filepath.Join(src, "a", "f.txt"), "inside")
	if err := os.Chmod(filepath.Join(src, "a"), 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(src, "a"), 0o755) })
	pass2Write(t, filepath.Join(src, "z.txt"), "secret")
	if err := os.Chmod(filepath.Join(src, "z.txt"), 0); err != nil {
		t.Fatal(err)
	}
	dstDir := filepath.Join(root, "dst")
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		t.Fatal(err)
	}

	res := ops.Copy([]string{src}, dstDir, false, false)
	if len(res) != 1 || res[0].OK || res[0].Error == nil || res[0].Error.Code != "COPY_FAILED" {
		t.Fatalf("Copy with an unreadable sibling: %+v", res)
	}
	if _, err := os.Lstat(filepath.Join(dstDir, "d")); !os.IsNotExist(err) {
		t.Fatalf("a partial destination remains: lstat err = %v", err)
	}
	if msg := res[0].Error.Message; strings.Contains(msg, "a partial copy remains") {
		t.Errorf("message claims a partial copy remains although it was removed: %q", msg)
	}
}

// Audit F-144 stays true: a read-only source subfolder is copied whole and
// ends with the source mode once the copy has succeeded.
func TestPass2Review_SuccessfulCopyKeepsReadOnlySubfolderMode(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory modes")
	}
	ops, root := setupJail(t)
	src := filepath.Join(root, "src", "d")
	pass2Write(t, filepath.Join(src, "a", "deep", "f.txt"), "inside")
	pass2Write(t, filepath.Join(src, "b.txt"), "b")
	for _, p := range []string{filepath.Join(src, "a", "deep"), filepath.Join(src, "a"), src} {
		if err := os.Chmod(p, 0o555); err != nil {
			t.Fatal(err)
		}
	}
	dstDir := filepath.Join(root, "dst")
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dstDir, "d")
	t.Cleanup(func() {
		for _, p := range []string{src, filepath.Join(src, "a"), filepath.Join(src, "a", "deep"),
			dst, filepath.Join(dst, "a"), filepath.Join(dst, "a", "deep")} {
			_ = os.Chmod(p, 0o755)
		}
	})

	res := ops.Copy([]string{src}, dstDir, false, false)
	if len(res) != 1 || !res[0].OK {
		t.Fatalf("Copy of a folder with read-only subfolders: %+v", res)
	}
	if got := pass2Read(t, filepath.Join(dst, "a", "deep", "f.txt")); got != "inside" {
		t.Errorf("copied file now %q", got)
	}
	for _, p := range []string{dst, filepath.Join(dst, "a"), filepath.Join(dst, "a", "deep")} {
		info, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != 0o555 {
			t.Errorf("%s mode = %o, want 555", p, got)
		}
	}
}
