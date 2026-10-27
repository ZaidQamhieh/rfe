package fsops

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRootedPathRejectsSymlinkSwapAfterAuthorization(t *testing.T) {
	base := t.TempDir()
	jail := filepath.Join(base, "jail")
	parent := filepath.Join(jail, "parent")
	outside := filepath.Join(base, "outside")
	if err := os.MkdirAll(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	ops := New([]string{jail}, false)

	// access performs the authorization and pins the jail directory. Replace
	// the checked parent with a symlink before using the returned path handle.
	path, err := ops.access(filepath.Join(parent, "created.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer path.close()
	if err := os.Rename(parent, filepath.Join(jail, "parent.saved")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, parent); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	f, err := path.openFile(os.O_CREATE|os.O_WRONLY, 0o600)
	if err == nil {
		f.Close()
		t.Fatal("rooted open followed a swapped symlink outside the jail")
	}
	if _, err := os.Lstat(filepath.Join(outside, "created.txt")); !os.IsNotExist(err) {
		t.Fatalf("outside target was created: %v", err)
	}
}

func TestRootedPathStaysOnOpenedJailWhenRootNameIsReplaced(t *testing.T) {
	base := t.TempDir()
	jail := filepath.Join(base, "jail")
	oldFile := filepath.Join(jail, "inside.txt")
	if err := os.MkdirAll(jail, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(oldFile, []byte("opened jail"), 0o600); err != nil {
		t.Fatal(err)
	}
	ops := New([]string{jail}, false)
	path, err := ops.access(oldFile)
	if err != nil {
		t.Fatal(err)
	}
	defer path.close()

	openedJail := filepath.Join(base, "opened-jail")
	if err := os.Rename(jail, openedJail); err != nil {
		if windowsHandleBlocksRename(err) {
			// Windows refuses to rename the jail while its opened root handle is
			// live, so the name cannot be swapped; the path still reads the jail.
			f, oerr := path.open()
			if oerr != nil {
				t.Fatal(oerr)
			}
			defer f.Close()
			buf := make([]byte, len("opened jail"))
			if _, rerr := f.ReadAt(buf, 0); rerr != nil || string(buf) != "opened jail" {
				t.Fatalf("read through opened path = (%q, %v)", buf, rerr)
			}
			return
		}
		t.Fatal(err)
	}
	if err := os.Mkdir(jail, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(jail, "inside.txt"), []byte("replacement"), 0o600); err != nil {
		t.Fatal(err)
	}

	f, err := path.open()
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	buf := make([]byte, len("opened jail"))
	if _, err := f.ReadAt(buf, 0); err != nil {
		t.Fatal(err)
	}
	if string(buf) != "opened jail" {
		t.Fatalf("opened path followed replacement jail name: got %q", buf)
	}
}

func TestArchiveWriteRejectsSymlinkSwapAfterJoinCheck(t *testing.T) {
	base := t.TempDir()
	jail := filepath.Join(base, "jail")
	dest := filepath.Join(jail, "dest")
	parent := filepath.Join(dest, "parent")
	outside := filepath.Join(base, "outside")
	if err := os.MkdirAll(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	ops := New([]string{jail}, false)
	destPath, err := ops.access(dest)
	if err != nil {
		t.Fatal(err)
	}
	defer destPath.close()
	target, err := safeJoinSecure(destPath, filepath.Join("parent", "payload.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(parent, filepath.Join(dest, "parent.saved")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, parent); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if f, err := createArchiveFile(target, 0o600); err == nil {
		f.Close()
		t.Fatal("archive write followed a swapped symlink outside the jail")
	}
	if _, err := os.Lstat(filepath.Join(outside, "payload.txt")); !os.IsNotExist(err) {
		t.Fatalf("outside target was created: %v", err)
	}
}
