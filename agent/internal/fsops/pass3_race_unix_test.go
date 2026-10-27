//go:build linux || darwin

package fsops

// Pass-3 review fixes: the link-then-unlink fallback never deletes an entry it
// did not create, and the overwrite put-back never replaces or deletes what is
// at the destination. Every interleaving is forced deterministically through the
// seams (linkatFn, unlinkatFn, beforeAsidePutBack); none relies on timing.

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// linkThen makes linkat succeed and then run after, as if another process acted
// between the link and the unlink.
func linkThen(after func()) func(int, string, int, string, int) error {
	return func(a int, b string, c int, d string, f int) error {
		if err := unix.Linkat(a, b, c, d, f); err != nil {
			return err
		}
		after()
		return nil
	}
}

func TestPass3FallbackSourceRemovedBetweenLinkAndUnlink(t *testing.T) {
	forceNoPrimitive(t)
	setSeams(t)
	dir := t.TempDir()
	pass3Write(t, filepath.Join(dir, "src"), "payload")
	linkatFn = linkThen(func() {
		if err := os.Remove(filepath.Join(dir, "src")); err != nil {
			t.Error(err)
		}
	})
	s, d := pass3Pair(dir, "src", "dst")
	if err := s.renameNoReplaceTo(d); err != nil {
		t.Fatalf("err = %v, want success: the content lives at dst", err)
	}
	if got := pass3Read(t, filepath.Join(dir, "dst")); got != "payload" {
		t.Errorf("dst = %q, want the content kept", got)
	}
}

func TestPass3FallbackSourceSwappedBetweenLinkAndUnlink(t *testing.T) {
	forceNoPrimitive(t)
	setSeams(t)
	dir := t.TempDir()
	pass3Write(t, filepath.Join(dir, "src"), "payload")
	linkatFn = linkThen(func() {
		if err := os.Remove(filepath.Join(dir, "src")); err != nil {
			t.Error(err)
		}
		pass3Write(t, filepath.Join(dir, "src"), "foreign")
	})
	s, d := pass3Pair(dir, "src", "dst")
	err := s.renameNoReplaceTo(d)
	if !errors.Is(err, errSourceChanged) || errors.Is(err, fs.ErrExist) || errors.Is(err, ErrNoAtomicRename) {
		t.Fatalf("err = %v, want errSourceChanged only", err)
	}
	if got := pass3Read(t, filepath.Join(dir, "src")); got != "foreign" {
		t.Errorf("the entry that took src's name was deleted or changed: %q", got)
	}
	if got := pass3Read(t, filepath.Join(dir, "dst")); got != "payload" {
		t.Errorf("dst = %q, want the content kept", got)
	}
}

func TestPass3FallbackUnlinkFailureDoesNotRemoveForeignDestination(t *testing.T) {
	forceNoPrimitive(t)
	setSeams(t)
	dir := t.TempDir()
	pass3Write(t, filepath.Join(dir, "src"), "payload")
	unlinkatFn = func(fd int, name string, flags int) error {
		if name != "src" {
			return unix.Unlinkat(fd, name, flags)
		}
		// Between the link and the cleanup, dst is replaced by a foreign entry.
		if err := os.Remove(filepath.Join(dir, "dst")); err != nil {
			t.Error(err)
		}
		pass3Write(t, filepath.Join(dir, "dst"), "foreign")
		return unix.EBUSY
	}
	s, d := pass3Pair(dir, "src", "dst")
	if err := s.renameNoReplaceTo(d); !errors.Is(err, unix.EBUSY) {
		t.Fatalf("err = %v, want EBUSY", err)
	}
	if got := pass3Read(t, filepath.Join(dir, "dst")); got != "foreign" {
		t.Errorf("a foreign dst was removed or changed: %q", got)
	}
	if got := pass3Read(t, filepath.Join(dir, "src")); got != "payload" {
		t.Errorf("src = %q", got)
	}
}

// Unlink fails and the source is gone too: the new link is the last name of the
// content and must stay.
func TestPass3FallbackUnlinkFailureNeverRemovesLastName(t *testing.T) {
	forceNoPrimitive(t)
	setSeams(t)
	dir := t.TempDir()
	pass3Write(t, filepath.Join(dir, "src"), "payload")
	unlinkatFn = func(fd int, name string, flags int) error {
		if name != "src" {
			return unix.Unlinkat(fd, name, flags)
		}
		_ = os.Remove(filepath.Join(dir, "src"))
		return unix.EBUSY
	}
	s, d := pass3Pair(dir, "src", "dst")
	_ = s.renameNoReplaceTo(d)
	if got := pass3Read(t, filepath.Join(dir, "dst")); got != "payload" {
		t.Errorf("the last name of the content was removed: %q", got)
	}
}

// An overwrite move on a file system with neither RENAME_NOREPLACE nor hard
// links: the occupant is moved aside, the move is refused, and the occupant must
// come back to its name.
func TestPass3OverwriteMoveNoAtomicRenamePutsOccupantBack(t *testing.T) {
	ops, root := setupJail(t)
	pass3Write(t, filepath.Join(root, "src.txt"), "payload")
	pass3Write(t, filepath.Join(root, "dest", "src.txt"), "occupant")
	noHardLinks(t)
	res := ops.Move([]string{filepath.Join(root, "src.txt")}, filepath.Join(root, "dest"), false, true)
	if len(res) != 1 || res[0].OK || res[0].Error == nil || res[0].Error.Code != "NOT_IMPLEMENTED" {
		t.Fatalf("result = %+v, want NOT_IMPLEMENTED", res)
	}
	if got := pass3Read(t, filepath.Join(root, "dest", "src.txt")); got != "occupant" {
		t.Errorf("occupant not back at its name: %q", got)
	}
	if got := pass3Read(t, filepath.Join(root, "src.txt")); got != "payload" {
		t.Errorf("source = %q", got)
	}
	if m, _ := filepath.Glob(filepath.Join(root, "dest", ".rfe-old-*")); len(m) != 0 {
		t.Errorf("hidden copy left behind: %v", m)
	}
	if msg := res[0].Error.Message; strings.Contains(msg, "/") || msg != NoAtomicRenameMessage {
		t.Errorf("message = %q, want exactly the fixed text", msg)
	}
}

// If the put-back cannot happen (dst is taken), the entry is kept and the note
// gives its hidden name only, never "nothing was changed" and never a path.
func TestPass3RestoreAsideKeepNoAtomicRenameDestinationTaken(t *testing.T) {
	noHardLinks(t)
	dir := t.TempDir()
	pass3Write(t, filepath.Join(dir, ".rfe-old-aaaa"), "replaced")
	pass3Write(t, filepath.Join(dir, "dst"), "newcomer")
	s, d := pass3Pair(dir, ".rfe-old-aaaa", "dst")
	note := restoreAsideKeep(d, s)
	if got := pass3Read(t, filepath.Join(dir, "dst")); got != "newcomer" {
		t.Errorf("dst = %q, want untouched", got)
	}
	if got := pass3Read(t, filepath.Join(dir, ".rfe-old-aaaa")); got != "replaced" {
		t.Errorf("aside = %q, want kept", got)
	}
	if !strings.Contains(note, ".rfe-old-aaaa") || strings.Contains(note, "/") || strings.Contains(note, "nothing was changed") {
		t.Errorf("note = %q, want the hidden name only", note)
	}
	if msg := batchMessage(ErrNoAtomicRename, ErrNoAtomicRename.Error()+note); strings.Contains(msg, "nothing was changed") || strings.Contains(msg, "/") {
		t.Errorf("message = %q", msg)
	}
}

// restoreAside removes the partial result and puts the replaced entry back; an
// entry created at dst in between is never deleted or replaced.
func TestPass3RestoreAsideKeepsEntryCreatedAfterRemoval(t *testing.T) {
	for _, mode := range []string{"native", "no primitive"} {
		t.Run(mode, func(t *testing.T) {
			if mode != "native" {
				noHardLinks(t)
			}
			old := beforeAsidePutBack
			t.Cleanup(func() { beforeAsidePutBack = old })
			dir := t.TempDir()
			pass3Write(t, filepath.Join(dir, ".rfe-old-bbbb"), "replaced")
			pass3Write(t, filepath.Join(dir, "dst"), "partial copy")
			beforeAsidePutBack = func() { pass3Write(t, filepath.Join(dir, "dst"), "concurrent") }
			s, d := pass3Pair(dir, ".rfe-old-bbbb", "dst")
			note := restoreAside(d, s)
			if got := pass3Read(t, filepath.Join(dir, "dst")); got != "concurrent" {
				t.Errorf("dst = %q, the concurrent entry was removed or replaced", got)
			}
			if got := pass3Read(t, filepath.Join(dir, ".rfe-old-bbbb")); got != "replaced" {
				t.Errorf("aside = %q, want kept", got)
			}
			if !strings.Contains(note, ".rfe-old-bbbb") {
				t.Errorf("note = %q, want the kept name", note)
			}
		})
	}
}

// With nothing in the way, restoreAside still puts the replaced entry back.
func TestPass3RestoreAsideRestoresWhenFree(t *testing.T) {
	dir := t.TempDir()
	pass3Write(t, filepath.Join(dir, ".rfe-old-cccc"), "replaced")
	pass3Write(t, filepath.Join(dir, "dst"), "partial copy")
	s, d := pass3Pair(dir, ".rfe-old-cccc", "dst")
	if note := restoreAside(d, s); note != "" {
		t.Errorf("note = %q", note)
	}
	if got := pass3Read(t, filepath.Join(dir, "dst")); got != "replaced" {
		t.Errorf("dst = %q, want the replaced entry back", got)
	}
}
