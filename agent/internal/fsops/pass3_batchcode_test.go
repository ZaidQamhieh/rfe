//go:build linux || darwin

package fsops

// A move or restore whose no-replace rename hits a file system with no atomic
// primitive and no hard links fails with ErrNoAtomicRename. The per-item result
// must say NOT_IMPLEMENTED with fixed text: no absolute path, nothing changed.

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

func TestPass3BatchCode(t *testing.T) {
	wrapped := fmt.Errorf("rename /abs/a /abs/b: %w: %w", ErrNoAtomicRename, unix.EPERM)
	for _, c := range []struct {
		name string
		err  error
		want string
	}{
		{"sentinel", ErrNoAtomicRename, "NOT_IMPLEMENTED"},
		{"wrapped", wrapped, "NOT_IMPLEMENTED"},
		{"conflict", ErrConflict, "CONFLICT"},
		{"exist", fs.ErrExist, "CONFLICT"},
		{"raw ENOTSUP is not the sentinel", unix.ENOTSUP, "DEF"},
		{"bare errors.ErrUnsupported is not the sentinel", errors.ErrUnsupported, "DEF"},
		{"other", errors.New("boom"), "DEF"},
	} {
		if got := batchCode(c.err, "DEF"); got != c.want {
			t.Errorf("%s: batchCode = %q, want %q", c.name, got, c.want)
		}
	}
	if got := batchMessage(wrapped, wrapped.Error()); got != NoAtomicRenameMessage {
		t.Errorf("batchMessage(sentinel) = %q", got)
	}
	const note = "; the replaced entry was kept at /x/.rfe-old-1"
	if got := batchMessage(wrapped, wrapped.Error()+note); got != strings.TrimSuffix(NoAtomicRenameMessage, "; nothing was changed")+note {
		t.Errorf("batchMessage(sentinel+note) = %q", got)
	}
	if got := batchMessage(errors.New("x"), "keep"); got != "keep" {
		t.Errorf("batchMessage(other) = %q, want keep", got)
	}
}

// noHardLinks makes the native primitive report "unsupported" and linkat fail
// with EPERM, as on a file system that cannot hard-link.
func noHardLinks(t *testing.T) {
	t.Helper()
	forceNoPrimitive(t)
	setSeams(t)
	linkatFn = func(int, string, int, string, int) error { return syscall.Errno(unix.EPERM) }
}

func checkNotImplemented(t *testing.T, what string, res []BatchResult, root string) {
	t.Helper()
	if len(res) != 1 || res[0].OK || res[0].Error == nil {
		t.Fatalf("%s: result = %+v, want one failure", what, res)
	}
	e := res[0].Error
	if e.Code != "NOT_IMPLEMENTED" {
		t.Errorf("%s: code = %q, want NOT_IMPLEMENTED (%q)", what, e.Code, e.Message)
	}
	// root "" means the item also carries a where-it-was-kept note (an overwrite
	// whose put-back failed too); otherwise the text is exactly the fixed one.
	if root != "" && e.Message != NoAtomicRenameMessage {
		t.Errorf("%s: message = %q, want exactly the fixed text", what, e.Message)
	}
	if root != "" && (strings.Contains(e.Message, root) || strings.Contains(e.Message, "/")) {
		t.Errorf("%s: message leaks a path: %q", what, e.Message)
	}
	if root == "" && !strings.HasPrefix(e.Message, strings.TrimSuffix(NoAtomicRenameMessage, "; nothing was changed")) {
		t.Errorf("%s: message = %q, want the fixed text first", what, e.Message)
	}
}

func TestPass3BatchMoveNoAtomicRename(t *testing.T) {
	ops, root := setupJail(t)
	pass3Write(t, filepath.Join(root, "src.txt"), "payload")
	if err := os.Mkdir(filepath.Join(root, "dest"), 0o755); err != nil {
		t.Fatal(err)
	}
	noHardLinks(t)
	res := ops.Move([]string{filepath.Join(root, "src.txt")}, filepath.Join(root, "dest"), false, false)
	checkNotImplemented(t, "move", res, root)
	if got := pass3Read(t, filepath.Join(root, "src.txt")); got != "payload" {
		t.Errorf("source = %q, want intact", got)
	}
	if exists(filepath.Join(root, "dest", "src.txt")) {
		t.Error("destination was created")
	}
}

func TestPass3BatchMoveNoAtomicRenameLeavesOccupantAlone(t *testing.T) {
	ops, root := setupJail(t)
	pass3Write(t, filepath.Join(root, "src.txt"), "payload")
	pass3Write(t, filepath.Join(root, "dest", "src.txt"), "occupant")
	noHardLinks(t)
	res := ops.Move([]string{filepath.Join(root, "src.txt")}, filepath.Join(root, "dest"), false, true)
	checkNotImplemented(t, "overwrite move", res, "")
	if got := pass3Read(t, filepath.Join(root, "src.txt")); got != "payload" {
		t.Errorf("source = %q, want intact", got)
	}
	// The put-back goes through the same no-replace rename, so on this file
	// system the replaced entry stays at its hidden name and the message says so.
	if !strings.HasPrefix(res[0].Error.Message, strings.TrimSuffix(NoAtomicRenameMessage, "; nothing was changed")) {
		t.Errorf("message = %q, want the fixed text first", res[0].Error.Message)
	}
	matches, _ := filepath.Glob(filepath.Join(root, "dest", ".rfe-old-*"))
	if exists(filepath.Join(root, "dest", "src.txt")) {
		if got := pass3Read(t, filepath.Join(root, "dest", "src.txt")); got != "occupant" {
			t.Errorf("destination = %q, want the occupant", got)
		}
	} else if len(matches) != 1 || pass3Read(t, matches[0]) != "occupant" {
		t.Errorf("occupant lost: aside entries %v", matches)
	} else if !strings.Contains(res[0].Error.Message, matches[0]) {
		t.Errorf("message %q does not say where the occupant was kept (%s)", res[0].Error.Message, matches[0])
	}
}

func TestPass3BatchRestoreNoAtomicRename(t *testing.T) {
	ops, root, trashDir := setupTrash(t)
	src := filepath.Join(root, "doc.txt")
	pass3Write(t, src, "keep me")
	if res := ops.MoveToTrash([]string{src}, trashDir); len(res) != 1 || !res[0].OK {
		t.Fatalf("MoveToTrash: %+v", res)
	}
	items, err := ListTrash(trashDir)
	if err != nil || len(items) != 1 {
		t.Fatalf("ListTrash: %v %+v", err, items)
	}
	noHardLinks(t)
	res := ops.RestoreFromTrash([]string{items[0].ID}, trashDir)
	checkNotImplemented(t, "restore", res, root)
	if exists(src) {
		t.Error("destination was created")
	}
	payload := filepath.Join(trashFilesDir(trashDir), items[0].ID)
	if got := pass3Read(t, payload); got != "keep me" {
		t.Errorf("trash payload = %q, want intact", got)
	}
	if after, _ := ListTrash(trashDir); len(after) != 1 {
		t.Errorf("trash entry gone after a failed restore: %+v", after)
	}
}
