//go:build !windows

package fsops

// Spec tests (QA2) that need symlinks, FIFOs, sockets or permission bits:
// FR-18, FR-30, FR-34 and the DELETE_FAILED alternate flow of UC-DELETE.

import (
	"archive/tar"
	"errors"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// qa2specOpenNoBlock calls ops.Open on path and fails the test if it does not
// return within 5 seconds. On timeout it opens the path for writing so that a
// blocked reader is released before the test ends.
func qa2specOpenNoBlock(t *testing.T, ops *Ops, path string) (*os.File, error) {
	t.Helper()
	type result struct {
		f   *os.File
		err error
	}
	ch := make(chan result, 1)
	go func() {
		f, err := ops.Open(path)
		ch <- result{f, err}
	}()
	select {
	case r := <-ch:
		return r.f, r.err
	case <-time.After(5 * time.Second):
		go func() {
			if w, err := os.OpenFile(path, os.O_WRONLY, 0); err == nil {
				_ = w.Close()
			}
		}()
		t.Fatalf("Open(%s) did not return within 5s; the agent must not block on a FIFO or socket", path)
		return nil, nil
	}
}

// TestQA2SpecFR18_TarLinkAndFifoEntriesNotCreated checks that non-regular archive entries are not created.
func TestQA2SpecFR18_TarLinkAndFifoEntriesNotCreated(t *testing.T) {
	// FR-18: "Given a tar.gz containing a symbolic link entry, when POST /fs/extract, then no symbolic link is created under destDir."
	// A FIFO entry is checked the same way (FR-18: "or other non-regular entries").
	// The response is not asserted (open question 13).
	// catches: a symlink or FIFO is created under destDir.
	ops, root := setupJail(t)
	archive := filepath.Join(root, "links.tgz")
	qa2specTarGzArchive(t, archive, []qa2specEntry{
		{name: "ok.txt", body: "fine"},
		{name: "link", typeflag: tar.TypeSymlink, linkname: "../../outside-target"},
		{name: "pipe", typeflag: tar.TypeFifo},
	})
	dest := filepath.Join(root, "out")

	if _, err := ops.Extract(archive, dest); err != nil {
		t.Logf("Extract returned an error (response is open question 13): %v", err)
	}
	if _, err := os.Lstat(dest); os.IsNotExist(err) {
		return
	}
	err := filepath.WalkDir(dest, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == dest {
			return nil
		}
		switch {
		case d.Type()&fs.ModeSymlink != 0:
			t.Errorf("symbolic link created under destDir: %s", p)
		case d.IsDir(), d.Type().IsRegular():
		default:
			t.Errorf("non-regular entry created under destDir: %s (type %v)", p, d.Type())
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", dest, err)
	}
}

// TestQA2SpecFR30_FIFONotOpened checks that a FIFO is refused without being opened.
func TestQA2SpecFR30_FIFONotOpened(t *testing.T) {
	// FR-30: "Given a FIFO at path P, when GET /content?path=P is sent, then the response is 422 with code NOT_A_FILE and the request does not block on P."
	// The package returns ErrNotRegular, which the contract maps to NOT_A_FILE; the 422 mapping is server-side.
	// catches: Open blocks on the FIFO, or returns a handle to it.
	ops, root := setupJail(t)
	fifo := filepath.Join(root, "pipe")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatalf("mkfifo: %v", err)
	}

	f, err := qa2specOpenNoBlock(t, ops, fifo)
	if f != nil {
		_ = f.Close()
	}
	if !errors.Is(err, ErrNotRegular) {
		t.Errorf("Open(FIFO): err = %v, want ErrNotRegular (NOT_A_FILE)", err)
	}
}

// TestQA2SpecFR30_SocketNotOpened checks that a socket is refused like a FIFO.
func TestQA2SpecFR30_SocketNotOpened(t *testing.T) {
	// FR-30: "GET /content on a FIFO, socket or device must return 422 NOT_A_FILE and must not open the path."
	// The socket is in a short temp dir so the unix socket path stays under the OS limit.
	// The device case needs a device node outside temp dirs and is not tested.
	// catches: a socket is opened as a file, or the request fails with another error.
	dir, err := os.MkdirTemp("", "qa2spec")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	ln, err := net.Listen("unix", filepath.Join(dir, "s.sock"))
	if err != nil {
		t.Fatalf("listen unix socket: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	ops := New([]string{dir}, false)
	f, err := qa2specOpenNoBlock(t, ops, filepath.Join(dir, "s.sock"))
	if f != nil {
		_ = f.Close()
	}
	if !errors.Is(err, ErrNotRegular) {
		t.Errorf("Open(socket): err = %v, want ErrNotRegular (NOT_A_FILE)", err)
	}
}

// TestQA2SpecFR34_ChildPathUnderEscapingSymlinkForbidden checks a child path reached through a link that escapes the jail.
func TestQA2SpecFR34_ChildPathUnderEscapingSymlinkForbidden(t *testing.T) {
	// FR-34: "Given a symbolic link inside the jail whose target is outside it, when DELETE /fs or PATCH /fs/rename acts on a child path under that link, then the result is FORBIDDEN and nothing outside the jail changes."
	// The link itself as the named path is open question 2 and is not tested. The destination under the link is covered by the same rule.
	// catches: a file outside the jail is deleted, moved or created through the escaping link.
	ops, root := setupJail(t)
	outside := t.TempDir()
	victim := filepath.Join(outside, "victim.txt")
	qa2specWrite(t, victim, "keep")
	link := filepath.Join(root, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	child := filepath.Join(link, "victim.txt")

	res := ops.Delete([]string{child})
	if len(res) != 1 || res[0].OK || qa2specCode(res[0]) != "FORBIDDEN" {
		t.Errorf("Delete under escaping link: %+v, want code FORBIDDEN", res)
	}
	trashDir := filepath.Join(t.TempDir(), "Trash")
	res = ops.MoveToTrash([]string{child}, trashDir)
	if len(res) != 1 || res[0].OK || qa2specCode(res[0]) != "FORBIDDEN" {
		t.Errorf("MoveToTrash under escaping link: %+v, want code FORBIDDEN", res)
	}
	if got := qa2specRead(t, victim); got != "keep" {
		t.Errorf("victim changed after refused delete: %q", got)
	}

	if _, err := ops.Rename(child, filepath.Join(link, "moved.txt")); !errors.Is(err, ErrForbidden) {
		t.Errorf("Rename under escaping link: err = %v, want ErrForbidden", err)
	}
	if qa2specExists(filepath.Join(outside, "moved.txt")) {
		t.Errorf("rename created a file outside the jail")
	}
	if got := qa2specRead(t, victim); got != "keep" {
		t.Errorf("victim changed after refused rename: %q", got)
	}

	inside := filepath.Join(root, "inside.txt")
	qa2specWrite(t, inside, "in")
	if _, err := ops.Rename(inside, filepath.Join(link, "new.txt")); !errors.Is(err, ErrForbidden) {
		t.Errorf("Rename into escaping link: err = %v, want ErrForbidden", err)
	}
	if qa2specExists(filepath.Join(outside, "new.txt")) {
		t.Errorf("rename into the escaping link created a file outside the jail")
	}
	if got := qa2specRead(t, inside); got != "in" {
		t.Errorf("inside file changed after refused rename: %q", got)
	}
}

// TestQA2SpecUCDelete_RemovalFailureReportsDeleteFailed checks the UC-DELETE alternate flow for a failed removal.
func TestQA2SpecUCDelete_RemovalFailureReportsDeleteFailed(t *testing.T) {
	// UC-DELETE alt: "removal fails -> BatchResult error DELETE_FAILED".
	// The removal fails because the parent directory is read-only. Root bypasses that, so the test is skipped as root.
	// catches: a failed removal reports ok=true or a different code, or the file is removed anyway.
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permissions; a removal failure cannot be forced")
	}
	ops, root := setupJail(t)
	dir := filepath.Join(root, "locked")
	file := filepath.Join(dir, "keep.txt")
	qa2specWrite(t, file, "keep")
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	res := ops.Delete([]string{file})
	if len(res) != 1 || res[0].OK || qa2specCode(res[0]) != "DELETE_FAILED" {
		t.Errorf("Delete with failing removal: %+v, want ok=false with code DELETE_FAILED", res)
	}
	if !qa2specExists(file) {
		t.Errorf("file removed despite the failed delete")
	}
}
