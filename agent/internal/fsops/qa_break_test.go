//go:build !windows

package fsops

// Break tests for the filesystem operations, written by qa-break and not run by it.
//
// Run from /home/zaid/Projects/rfe-rn/agent:
//   go test ./internal/fsops/ -run TestQABreak -count=1 -v
//
// Each test's doc comment names the requirement (requirements.md item number)
// or the code comment it checks. Where requirements.md leaves the behaviour
// open (Open questions, Assumed items), the test asserts only safety
// properties: nothing outside the jail is touched, no existing file is lost,
// no panic, no hang. The two archive-limit tests are skipped under -short.

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// qaBreakZipEntry is one member of a zip written by qaBreakWriteZip. A zero
// mode means a regular file with mode 0644; ModeSymlink makes a symlink entry.
type qaBreakZipEntry struct {
	name string
	data []byte
	mode os.FileMode
}

func qaBreakWriteZip(t *testing.T, path string, entries []qaBreakZipEntry) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for _, e := range entries {
		mode := e.mode
		if mode == 0 {
			mode = 0o644
		}
		hdr := &zip.FileHeader{Name: e.name, Method: zip.Deflate}
		hdr.SetMode(mode)
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(e.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

// qaBreakTarEntry is one member of a .tar.gz written by qaBreakWriteTarGz.
type qaBreakTarEntry struct {
	name     string
	typeflag byte
	linkname string
	data     []byte
}

func qaBreakWriteTarGz(t *testing.T, path string, entries []qaBreakTarEntry) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		hdr := &tar.Header{Name: e.name, Typeflag: e.typeflag, Linkname: e.linkname, Mode: 0o644}
		if e.typeflag == tar.TypeReg {
			hdr.Size = int64(len(e.data))
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if e.typeflag == tar.TypeReg {
			if _, err := tw.Write(e.data); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

// qaBreakZeros is an endless stream of zero bytes, used to build an archive
// whose expanded size is past the extraction budget without holding it in memory.
type qaBreakZeros struct{}

func (qaBreakZeros) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 0
	}
	return len(p), nil
}

func qaBreakWrite(t *testing.T, p, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func qaBreakRead(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read %s: %v", p, err)
	}
	return string(b)
}

// qaBreakRegularFiles lists every regular file under dir (links are not followed).
func qaBreakRegularFiles(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type().IsRegular() {
			out = append(out, p)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// qaBreakWithin runs fn and fails the test if fn has not returned within d.
func qaBreakWithin(t *testing.T, d time.Duration, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	select {
	case <-done:
	case <-time.After(d):
		t.Fatalf("operation did not return within %v", d)
	}
}

// qaBreakNoCompressTemp fails if a Compress temp file for base is left in dir.
func qaBreakNoCompressTemp(t *testing.T, dir, base string) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, "."+base+".rfe-tmp-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("compress temp file left behind: %v", matches)
	}
}

// Requirement 1 (jail root): the configured root itself is an Assumed item, not
// a contract, so this is safety-only: whatever the root is spelled as, the root
// and a sentinel file under it survive every entry operation. Label: code failure.
func TestQABreak_JailRootSpellingsSurvive(t *testing.T) {
	ops, root := setupJail(t)
	sentinel := filepath.Join(root, "sentinel.txt")
	qaBreakWrite(t, sentinel, "keep")
	trashDir := filepath.Join(t.TempDir(), "Trash")
	sub := filepath.Join(root, "sub")
	for _, p := range []string{root, root + "/", root + "/.", root + "/sub/.."} {
		_ = ops.Delete([]string{p})
		_ = ops.MoveToTrash([]string{p}, trashDir)
		_ = ops.Move([]string{p}, sub, false, true)
		_ = ops.Copy([]string{p}, sub, true, true)
	}
	if _, err := ops.Rename(root, root+"-moved"); err == nil {
		t.Errorf("Rename(root, root-moved) succeeded: the jail root was moved")
	}
	if _, err := os.Stat(root); err != nil {
		t.Fatalf("jail root is gone: %v", err)
	}
	if got := qaBreakRead(t, sentinel); got != "keep" {
		t.Fatalf("sentinel under the jail root changed: %q", got)
	}
}

// Requirement 7: a relative path given to an fs route is refused with
// ErrForbidden (403) and nothing is created relative to the working directory.
// Source: jail.go:126-128 and 479-481. Label: code failure.
func TestQABreak_RelativePathsRefusedAndNothingCreated(t *testing.T) {
	ops, root := setupJail(t)
	name := "qa-break-relative-probe"
	t.Cleanup(func() {
		_ = os.RemoveAll(name)
		_ = os.Remove(name + ".zip")
	})
	a := filepath.Join(root, "a.txt")
	qaBreakWrite(t, a, "A")
	if _, err := ops.CreateFolder(name + "/x"); !errors.Is(err, ErrForbidden) {
		t.Errorf("CreateFolder(relative) = %v, want ErrForbidden", err)
	}
	if _, err := ops.CreateFolder(name); !errors.Is(err, ErrForbidden) {
		t.Errorf("CreateFolder(relative leaf) = %v, want ErrForbidden", err)
	}
	if res := ops.Delete([]string{"a.txt"}); len(res) != 1 || res[0].OK {
		t.Errorf("Delete(relative) = %+v, want refusal", res)
	}
	if _, err := ops.Compress([]string{a}, name+".zip"); !errors.Is(err, ErrForbidden) {
		t.Errorf("Compress(relative dest) = %v, want ErrForbidden", err)
	}
	if _, err := os.Lstat(name); !os.IsNotExist(err) {
		t.Errorf("a folder was created relative to the working directory (err=%v)", err)
	}
	if _, err := os.Lstat(name + ".zip"); !os.IsNotExist(err) {
		t.Errorf("an archive was created relative to the working directory (err=%v)", err)
	}
	if got := qaBreakRead(t, a); got != "A" {
		t.Fatalf("file under the jail changed: %q", got)
	}
}

// Requirement 8: a path outside the configured roots is refused with
// ErrForbidden, and nothing at that location is read, created, modified,
// renamed or deleted. Source: protocol/openapi.yaml:1275-1277, 971-972;
// docs/architecture.md:60-61. Label: code failure.
func TestQABreak_OutsidePathsRefusedAndUntouched(t *testing.T) {
	ops, root := setupJail(t)
	outside := t.TempDir()
	secret := filepath.Join(outside, "x")
	qaBreakWrite(t, secret, "SECRET")
	a := filepath.Join(root, "a.txt")
	qaBreakWrite(t, a, "A")

	t.Run("meta", func(t *testing.T) {
		if _, err := ops.Meta(secret); !errors.Is(err, ErrForbidden) {
			t.Fatalf("Meta(outside) = %v, want ErrForbidden", err)
		}
	})
	t.Run("listdir", func(t *testing.T) {
		if _, err := ops.ListDir(outside, "", 10); !errors.Is(err, ErrForbidden) {
			t.Fatalf("ListDir(outside) = %v, want ErrForbidden", err)
		}
	})
	t.Run("open", func(t *testing.T) {
		f, err := ops.Open(secret)
		if f != nil {
			f.Close()
		}
		if !errors.Is(err, ErrForbidden) {
			t.Fatalf("Open(outside) = %v, want ErrForbidden", err)
		}
	})
	t.Run("write", func(t *testing.T) {
		if _, err := ops.WriteContent(secret, []byte("X"), nil); !errors.Is(err, ErrForbidden) {
			t.Fatalf("WriteContent(outside) = %v, want ErrForbidden", err)
		}
		if got := qaBreakRead(t, secret); got != "SECRET" {
			t.Fatalf("outside file changed: %q", got)
		}
	})
	t.Run("delete", func(t *testing.T) {
		if res := ops.Delete([]string{secret}); len(res) != 1 || res[0].OK {
			t.Fatalf("Delete(outside) = %+v, want refusal", res)
		}
		if got := qaBreakRead(t, secret); got != "SECRET" {
			t.Fatalf("outside file changed: %q", got)
		}
	})
	t.Run("create", func(t *testing.T) {
		if _, err := ops.CreateFolder(filepath.Join(outside, "new")); !errors.Is(err, ErrForbidden) {
			t.Fatalf("CreateFolder(outside) = %v, want ErrForbidden", err)
		}
		if _, err := os.Lstat(filepath.Join(outside, "new")); !os.IsNotExist(err) {
			t.Fatalf("folder created outside the jail (err=%v)", err)
		}
	})
	t.Run("compress-dest", func(t *testing.T) {
		if _, err := ops.Compress([]string{a}, filepath.Join(outside, "a.zip")); !errors.Is(err, ErrForbidden) {
			t.Fatalf("Compress(dest outside) = %v, want ErrForbidden", err)
		}
		if _, err := os.Lstat(filepath.Join(outside, "a.zip")); !os.IsNotExist(err) {
			t.Fatalf("archive written outside the jail (err=%v)", err)
		}
	})
	t.Run("rename-out", func(t *testing.T) {
		if _, err := ops.Rename(a, filepath.Join(outside, "moved.txt")); err == nil {
			t.Fatal("Rename moved a file out of the jail")
		}
		if _, err := os.Lstat(a); err != nil {
			t.Fatalf("source lost after refused rename: %v", err)
		}
	})
	t.Run("rename-in", func(t *testing.T) {
		if _, err := ops.Rename(secret, filepath.Join(root, "stolen.txt")); err == nil {
			t.Fatal("Rename moved an outside file into the jail")
		}
		if _, err := os.Lstat(secret); err != nil {
			t.Fatalf("outside file moved (err=%v)", err)
		}
	})
	t.Run("move-in", func(t *testing.T) {
		if res := ops.Move([]string{secret}, root, false, false); len(res) != 1 || res[0].OK {
			t.Fatalf("Move(outside into jail) = %+v, want refusal", res)
		}
		if _, err := os.Lstat(secret); err != nil {
			t.Fatalf("outside file moved (err=%v)", err)
		}
	})
}

// Requirement 9: dot-dot is removed before the jail decision. A cleaned path
// that leaves the root is refused and the outside file is untouched; a cleaned
// path that stays inside is accepted. Source: docs/architecture.md:60;
// jail.go:129-132. Label: code failure.
func TestQABreak_DotDotEscapeRefusedInsideAccepted(t *testing.T) {
	ops, root := setupJail(t)
	outside := t.TempDir()
	secret := filepath.Join(outside, "x")
	qaBreakWrite(t, secret, "SECRET")
	escape := filepath.Join(root, "sub", "..", "..", filepath.Base(outside), "x")

	t.Run("delete", func(t *testing.T) {
		if res := ops.Delete([]string{escape}); len(res) != 1 || res[0].OK {
			t.Fatalf("Delete(dot-dot escape) = %+v, want refusal", res)
		}
		if got := qaBreakRead(t, secret); got != "SECRET" {
			t.Fatalf("outside file changed: %q", got)
		}
	})
	t.Run("open", func(t *testing.T) {
		f, err := ops.Open(escape)
		if f != nil {
			f.Close()
		}
		if !errors.Is(err, ErrForbidden) {
			t.Fatalf("Open(dot-dot escape) = %v, want ErrForbidden", err)
		}
	})
	t.Run("write", func(t *testing.T) {
		if _, err := ops.WriteContent(escape, []byte("X"), nil); !errors.Is(err, ErrForbidden) {
			t.Fatalf("WriteContent(dot-dot escape) = %v, want ErrForbidden", err)
		}
		if got := qaBreakRead(t, secret); got != "SECRET" {
			t.Fatalf("outside file changed: %q", got)
		}
	})
	t.Run("inside-accepted", func(t *testing.T) {
		plain := filepath.Join(root, "plain.txt")
		qaBreakWrite(t, plain, "P")
		if res := ops.Delete([]string{filepath.Join(root, "sub", "..", "plain.txt")}); len(res) != 1 || !res[0].OK {
			t.Fatalf("Delete(inside dot-dot) = %+v, want OK", res)
		}
		if _, err := os.Lstat(plain); !os.IsNotExist(err) {
			t.Fatalf("inside file not deleted (err=%v)", err)
		}
	})
}

// Requirement 10: a symlink in an intermediate component whose target is
// outside the root must not be followed for create or read. Source:
// docs/architecture.md:60-61; jail.go:219-227. Label: code failure.
func TestQABreak_IntermediateLinkOutsideRefused(t *testing.T) {
	ops, root := setupJail(t)
	outside := t.TempDir()
	qaBreakWrite(t, filepath.Join(outside, "secret.txt"), "S")
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	if _, err := ops.CreateFolder(filepath.Join(root, "link", "new")); !errors.Is(err, ErrForbidden) {
		t.Errorf("CreateFolder through an outside link = %v, want ErrForbidden", err)
	}
	if _, err := os.Lstat(filepath.Join(outside, "new")); !os.IsNotExist(err) {
		t.Fatalf("folder created through the link (err=%v)", err)
	}
	f, err := ops.Open(filepath.Join(root, "link", "secret.txt"))
	if f != nil {
		f.Close()
	}
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("Open through an outside link = %v, want ErrForbidden", err)
	}
	if res := ops.Delete([]string{filepath.Join(root, "link", "secret.txt")}); len(res) != 1 || res[0].OK {
		t.Errorf("Delete through an outside link = %+v, want refusal", res)
	}
	if got := qaBreakRead(t, filepath.Join(outside, "secret.txt")); got != "S" {
		t.Fatalf("outside file changed through the link: %q", got)
	}
}

// Requirement 2 and the trailing-slash spelling: Delete and MoveToTrash of
// "link/" must act on the symlink itself, never on its target. Source:
// openapi.yaml:830-832; fsops.go:906-913 (Clean before resolveEntry). Label: code failure.
func TestQABreak_TrailingSlashLinkNeverTouchesTarget(t *testing.T) {
	ops, _, target, link := symlinkFixture(t)
	trashDir := filepath.Join(t.TempDir(), "Trash")
	t.Run("delete", func(t *testing.T) {
		_ = ops.Delete([]string{link + "/"})
		assertTargetIntact(t, target)
	})
	t.Run("trash", func(t *testing.T) {
		_ = ops.MoveToTrash([]string{link + "/"}, trashDir)
		assertTargetIntact(t, target)
	})
}

// Requirement 2 (trash variant): a link whose target lies outside the roots is
// trashed as the link; the outside target keeps its bytes. Source:
// openapi.yaml:830-832 ("a link whose target is outside the allowed roots can
// still be removed"). Label: code failure.
func TestQABreak_EscapingLinkTrashedNotTarget(t *testing.T) {
	ops, root, trashDir := setupTrash(t)
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.txt")
	qaBreakWrite(t, secret, "S")
	link := filepath.Join(root, "out-link")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	res := ops.MoveToTrash([]string{link}, trashDir)
	if len(res) != 1 || !res[0].OK {
		t.Fatalf("MoveToTrash(escaping link) = %+v, want OK", res)
	}
	assertGone(t, link)
	if got := qaBreakRead(t, secret); got != "S" {
		t.Fatalf("outside target changed: %q", got)
	}
	// The package-level listing: ops.ListTrash would hide this item, because its
	// target lies outside the jail.
	entries, err := ListTrash(trashDir)
	if err != nil || len(entries) != 1 || entries[0].OriginalPath != link {
		t.Fatalf("trash listing = %+v, %v; want the link's own path", entries, err)
	}
}

// Requirement 3: a rename of a symlink returns a description of the link:
// isSymlink true and symlinkTarget equal to the original target string.
// Source: openapi.yaml:888-889; fsops.go:650-654 and 1013-1040. Label: code failure.
func TestQABreak_RenamedLinkReportsItsTarget(t *testing.T) {
	ops, root, target, link := symlinkFixture(t)
	alias := filepath.Join(root, "alias")
	e, err := ops.Rename(link, alias)
	if err != nil {
		t.Fatalf("Rename(link): %v", err)
	}
	if !e.IsSymlink || e.SymlinkTarget != "real" {
		t.Fatalf("rename response = %+v, want isSymlink=true symlinkTarget=real", e)
	}
	if dest, err := os.Readlink(alias); err != nil || dest != "real" {
		t.Fatalf("renamed link target = %q, %v", dest, err)
	}
	assertTargetIntact(t, target)
}

// Requirement 5(b): copying a folder that contains a symlink recreates the
// symlink with the same target string and writes no bytes of the target under
// the copy. Source: openapi.yaml:913-915; fsops.go:1093-1096 (doc: "recreates
// symlinks without dereferencing them"). Label: code failure.
func TestQABreak_CopiedFolderKeepsInnerLinksAsLinks(t *testing.T) {
	ops, root := setupJail(t)
	qaBreakWrite(t, filepath.Join(root, "target.txt"), "TTTT")
	qaBreakWrite(t, filepath.Join(root, "F", "inner.txt"), "IN")
	if err := os.Symlink("../target.txt", filepath.Join(root, "F", "X")); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(root, "out")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	res := ops.Copy([]string{filepath.Join(root, "F")}, dest, false, false)
	if len(res) != 1 || !res[0].OK {
		t.Fatalf("Copy(folder with a link) = %+v", res)
	}
	if target, err := os.Readlink(filepath.Join(dest, "F", "X")); err != nil || target != "../target.txt" {
		t.Fatalf("copied link = %q, %v; want a link with the same target string", target, err)
	}
	for _, p := range qaBreakRegularFiles(t, dest) {
		if qaBreakRead(t, p) == "TTTT" {
			t.Fatalf("target bytes were written under the copy at %s", p)
		}
	}
}

// Requirement 5(a): copying a symlink source writes a regular file named after
// the link, with the target's bytes. Source: openapi.yaml:913-915. Label: code failure.
func TestQABreak_CopiedLinkBecomesRegularFileWithTargetBytes(t *testing.T) {
	ops, root := setupJail(t)
	qaBreakWrite(t, filepath.Join(root, "report.pdf"), "R")
	if err := os.Symlink("report.pdf", filepath.Join(root, "Shortcut")); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(root, "b")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	res := ops.Copy([]string{filepath.Join(root, "Shortcut")}, dest, false, false)
	if len(res) != 1 || !res[0].OK {
		t.Fatalf("Copy(link) = %+v", res)
	}
	fi, err := os.Lstat(filepath.Join(dest, "Shortcut"))
	if err != nil || !fi.Mode().IsRegular() {
		t.Fatalf("copy of a link is not a regular file: %v, %v", fi, err)
	}
	if got := qaBreakRead(t, filepath.Join(dest, "Shortcut")); got != "R" {
		t.Fatalf("copy bytes = %q, want the target's bytes", got)
	}
}

// Requirement 6: a link whose target is outside the roots yields no target
// bytes through Copy, Compress or Open. Source: docs/architecture.md:60-61;
// jail.go:122-158. Label: code failure.
func TestQABreak_LinkToOutsideYieldsNoBytes(t *testing.T) {
	ops, root := setupJail(t)
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.txt")
	qaBreakWrite(t, secret, "TOPSECRET")
	leak := filepath.Join(root, "leak.txt")
	if err := os.Symlink(secret, leak); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(root, "out")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	res := ops.Copy([]string{leak}, dest, false, false)
	if len(res) != 1 || res[0].OK {
		t.Fatalf("Copy(link to outside) = %+v, want a per-item error", res)
	}
	for _, p := range qaBreakRegularFiles(t, dest) {
		if qaBreakRead(t, p) == "TOPSECRET" {
			t.Fatalf("secret bytes copied into %s", p)
		}
	}
	f, err := ops.Open(leak)
	if f != nil {
		f.Close()
	}
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("Open(link to outside) = %v, want ErrForbidden", err)
	}
	zipPath := filepath.Join(root, "leak.zip")
	if _, err := ops.Compress([]string{leak}, zipPath); err == nil {
		zr, zerr := zip.OpenReader(zipPath)
		if zerr != nil {
			t.Fatal(zerr)
		}
		defer zr.Close()
		for _, zf := range zr.File {
			rc, rerr := zf.Open()
			if rerr != nil {
				t.Fatal(rerr)
			}
			b, _ := io.ReadAll(rc)
			rc.Close()
			if strings.Contains(string(b), "TOPSECRET") {
				t.Fatalf("archive entry %s carries the outside target's bytes", zf.Name)
			}
		}
	}
}

// Requirement 11: after ProtectDir, every Ops method refuses the protected
// folder and anything under it, and a sibling path still works. Source:
// jail.go:165-168 and 495-497. Label: code failure.
func TestQABreak_ProtectedFolderUnreachableThroughEveryOp(t *testing.T) {
	ops, root := setupJail(t)
	prot := filepath.Join(root, "vault")
	inside := filepath.Join(prot, "file.txt")
	qaBreakWrite(t, inside, "V")
	beside := filepath.Join(root, "beside.txt")
	qaBreakWrite(t, beside, "B")
	ProtectDir(prot)
	out := filepath.Join(root, "out")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}

	if _, err := ops.ListDir(prot, "", 10); !errors.Is(err, ErrForbidden) {
		t.Errorf("ListDir(protected) = %v, want ErrForbidden", err)
	}
	if _, err := ops.Meta(inside); !errors.Is(err, ErrForbidden) {
		t.Errorf("Meta(protected file) = %v, want ErrForbidden", err)
	}
	if f, err := ops.Open(inside); err == nil {
		f.Close()
		t.Errorf("Open(protected file) succeeded")
	}
	if res := ops.Delete([]string{inside}); len(res) != 1 || res[0].OK {
		t.Errorf("Delete(protected file) = %+v, want refusal", res)
	}
	if res := ops.Copy([]string{beside}, prot, false, false); len(res) != 1 || res[0].OK {
		t.Errorf("Copy into protected folder = %+v, want refusal", res)
	}
	if res := ops.Copy([]string{inside}, out, false, false); len(res) != 1 || res[0].OK {
		t.Errorf("Copy out of protected folder = %+v, want refusal", res)
	}
	if _, err := os.Lstat(filepath.Join(prot, "beside.txt")); !os.IsNotExist(err) {
		t.Errorf("a file was copied into the protected folder (err=%v)", err)
	}
	if _, err := os.Lstat(filepath.Join(out, "file.txt")); !os.IsNotExist(err) {
		t.Errorf("a protected file was copied out (err=%v)", err)
	}
	if got := qaBreakRead(t, inside); got != "V" {
		t.Fatalf("protected file changed: %q", got)
	}
	if _, err := ops.Meta(beside); err != nil {
		t.Fatalf("path beside the protected folder refused: %v", err)
	}
}

// Requirement 12: a FIFO is refused by Open with an error (ErrNotRegular, the
// NOT_A_FILE source) and Open does not block on it. Source: openapi.yaml:2497-2498;
// jail.go:327-329, 348-350. Label: code failure. The 422 mapping is server-side
// and is not checked here.
func TestQABreak_FIFOOpenRefusedWithoutBlocking(t *testing.T) {
	ops, root := setupJail(t)
	fifo := filepath.Join(root, "p")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	var openErr error
	qaBreakWithin(t, 10*time.Second, func() {
		f, err := ops.Open(fifo)
		if f != nil {
			f.Close()
		}
		openErr = err
	})
	if openErr == nil {
		t.Fatal("Open(FIFO) returned no error")
	}
	if !errors.Is(openErr, ErrNotRegular) {
		t.Fatalf("Open(FIFO) = %v, want ErrNotRegular", openErr)
	}
}

// Requirement 16(a): a copy with overwrite=true that fails part-way leaves the
// original destination in place. The failure is forced by a FIFO inside the
// source folder, which the copy refuses. Source: fsops.go:725-727 (doc: the
// replaced destination is "kept until the copy has succeeded"); fsops.go:774
// (restoreAside). Label: code failure.
func TestQABreak_FailedOverwriteCopyKeepsOriginal(t *testing.T) {
	ops, root := setupJail(t)
	src := filepath.Join(root, "S")
	qaBreakWrite(t, filepath.Join(src, "a.txt"), "A")
	if err := syscall.Mkfifo(filepath.Join(src, "p"), 0o600); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(root, "out")
	orig := filepath.Join(dest, "S", "keep.txt")
	qaBreakWrite(t, orig, "orig")
	var res []BatchResult
	qaBreakWithin(t, 15*time.Second, func() {
		res = ops.Copy([]string{src}, dest, false, true)
	})
	if len(res) != 1 || res[0].OK {
		t.Fatalf("Copy with an unreadable entry = %+v, want a failure", res)
	}
	if got := qaBreakRead(t, orig); got != "orig" {
		t.Fatalf("original destination lost after a failed overwrite: %q", got)
	}
	if aside, _ := filepath.Glob(filepath.Join(dest, ".rfe-old-*")); len(aside) != 0 {
		t.Fatalf("replaced folder left behind: %v", aside)
	}
}

// Requirement 16(b) and "folder into itself": copying or moving a folder into
// itself or its own subfolder is refused and creates nothing, with overwrite
// or duplicate set. Source: fsops.go:712-721 (Copy), 820-830 (Move); fsops.go:676-678
// and 809-811 (ancestor-guard docs). Label: code failure.
func TestQABreak_FolderIntoItselfRefused(t *testing.T) {
	ops, root := setupJail(t)
	d := filepath.Join(root, "D")
	keep := filepath.Join(d, "keep.txt")
	qaBreakWrite(t, keep, "keep")
	sub := filepath.Join(d, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	check := func(name string, res []BatchResult) {
		t.Helper()
		if len(res) != 1 || res[0].OK {
			t.Errorf("%s = %+v, want refusal", name, res)
		}
		if got := qaBreakRead(t, keep); got != "keep" {
			t.Fatalf("%s changed the folder: %q", name, got)
		}
		if _, err := os.Lstat(filepath.Join(sub, "D")); !os.IsNotExist(err) {
			t.Fatalf("%s created %s/D (err=%v)", name, sub, err)
		}
		if _, err := os.Lstat(filepath.Join(d, "D")); !os.IsNotExist(err) {
			t.Fatalf("%s created D/D (err=%v)", name, err)
		}
	}
	check("move into own subfolder, overwrite", ops.Move([]string{d}, sub, false, true))
	check("copy into own subfolder, overwrite", ops.Copy([]string{d}, sub, false, true))
	check("copy into itself, duplicate", ops.Copy([]string{d}, d, true, true))
	check("move into itself, duplicate", ops.Move([]string{d}, d, true, true))
}

// Requirement 16 (ancestor guard): an overwrite whose destination contains the
// source must not remove the destination. Here the source root/n/sub/n is
// copied or moved to root with overwrite=true, which would replace root/n.
// Source: fsops.go:743-747 and 838-847. Label: code failure.
func TestQABreak_OverwriteNeverRemovesAncestorOfSource(t *testing.T) {
	ops, root := setupJail(t)
	src := filepath.Join(root, "n", "sub", "n")
	keep := filepath.Join(src, "keep.txt")
	qaBreakWrite(t, keep, "keep")
	if res := ops.Copy([]string{src}, root, false, true); len(res) != 1 || res[0].OK {
		t.Errorf("Copy over an ancestor = %+v, want refusal", res)
	}
	if got := qaBreakRead(t, keep); got != "keep" {
		t.Fatalf("source destroyed by copy overwrite: %q", got)
	}
	if res := ops.Move([]string{src}, root, false, true); len(res) != 1 || res[0].OK {
		t.Errorf("Move over an ancestor = %+v, want refusal", res)
	}
	if got := qaBreakRead(t, keep); got != "keep" {
		t.Fatalf("source destroyed by move overwrite: %q", got)
	}
}

// Copy, move and rename of a folder onto itself. Undefined by requirements
// (the same-path case is not specified): safety-only, the folder and its
// contents must survive every spelling. Label: code failure.
func TestQABreak_SelfTargetOperationsKeepContents(t *testing.T) {
	ops, root := setupJail(t)
	d := filepath.Join(root, "D")
	keep := filepath.Join(d, "keep.txt")
	qaBreakWrite(t, keep, "keep")
	_ = ops.Copy([]string{d}, root, false, false)
	_ = ops.Copy([]string{d}, root, false, true)
	_ = ops.Copy([]string{d}, root, true, false)
	_ = ops.Move([]string{d}, root, false, true)
	_, _ = ops.Rename(d, d)
	if got := qaBreakRead(t, keep); got != "keep" {
		t.Fatalf("folder contents changed by a same-path operation: %q", got)
	}
	if _, err := os.Stat(d); err != nil {
		t.Fatalf("folder removed by a same-path operation: %v", err)
	}
}

// Requirement 14: with duplicate=false, overwrite=false an existing destination
// yields CONFLICT and is unchanged; overwrite=true replaces it; duplicate=true
// with overwrite=true creates "name (1)" and never replaces the existing
// destination. Source: openapi.yaml:2371, 2375-2380. Label: code failure.
func TestQABreak_CopyAndMoveOverwriteFlags(t *testing.T) {
	type batch func(ops *Ops, sources []string, destDir string, duplicate, overwrite bool) []BatchResult
	batches := []struct {
		name string
		run  batch
	}{
		{"copy", (*Ops).Copy},
		{"move", (*Ops).Move},
	}
	setup := func(t *testing.T) (ops *Ops, src, dstDir string) {
		t.Helper()
		var root string
		ops, root = setupJail(t)
		src = filepath.Join(root, "src.txt")
		qaBreakWrite(t, src, "NEW")
		dstDir = filepath.Join(root, "dst")
		qaBreakWrite(t, filepath.Join(dstDir, "src.txt"), "OLD")
		return ops, src, dstDir
	}
	for _, b := range batches {
		t.Run(b.name+"/no-overwrite", func(t *testing.T) {
			ops, src, dstDir := setup(t)
			res := b.run(ops, []string{src}, dstDir, false, false)
			if len(res) != 1 || res[0].OK || res[0].Error == nil || res[0].Error.Code != "CONFLICT" {
				t.Fatalf("result = %+v, want CONFLICT", res)
			}
			if got := qaBreakRead(t, filepath.Join(dstDir, "src.txt")); got != "OLD" {
				t.Fatalf("destination changed on CONFLICT: %q", got)
			}
			if got := qaBreakRead(t, src); got != "NEW" {
				t.Fatalf("source changed on CONFLICT: %q", got)
			}
		})
		t.Run(b.name+"/overwrite", func(t *testing.T) {
			ops, src, dstDir := setup(t)
			res := b.run(ops, []string{src}, dstDir, false, true)
			if len(res) != 1 || !res[0].OK {
				t.Fatalf("result = %+v, want OK", res)
			}
			if got := qaBreakRead(t, filepath.Join(dstDir, "src.txt")); got != "NEW" {
				t.Fatalf("destination after overwrite = %q, want NEW", got)
			}
		})
		t.Run(b.name+"/duplicate-beats-overwrite", func(t *testing.T) {
			ops, src, dstDir := setup(t)
			res := b.run(ops, []string{src}, dstDir, true, true)
			if len(res) != 1 || !res[0].OK {
				t.Fatalf("result = %+v, want OK", res)
			}
			if got := qaBreakRead(t, filepath.Join(dstDir, "src.txt")); got != "OLD" {
				t.Fatalf("existing destination replaced under duplicate: %q", got)
			}
			if got := qaBreakRead(t, filepath.Join(dstDir, "src (1).txt")); got != "NEW" {
				t.Fatalf("keep-both copy = %q, want NEW", got)
			}
		})
	}
}

// Requirement 17: Rename never replaces an existing file; it fails with
// ErrConflict (409) and both entries are unchanged. Source: fsops.go:622-629;
// openapi.yaml:889-890. Label: code failure.
func TestQABreak_RenameNeverReplacesFile(t *testing.T) {
	ops, root := setupJail(t)
	a := filepath.Join(root, "a")
	b := filepath.Join(root, "b")
	qaBreakWrite(t, a, "A")
	qaBreakWrite(t, b, "B")
	if _, err := ops.Rename(a, b); !errors.Is(err, ErrConflict) {
		t.Fatalf("Rename onto existing file = %v, want ErrConflict", err)
	}
	if got := qaBreakRead(t, a); got != "A" {
		t.Fatalf("source changed: %q", got)
	}
	if got := qaBreakRead(t, b); got != "B" {
		t.Fatalf("destination replaced: %q", got)
	}
}

// Requirement 18: a restore into an occupied original path uses a keep-both
// name, the BatchResult path is that name, and the occupant is intact. Source:
// openapi.yaml:1150-1152; trash.go:263-264, 287. Label: code failure.
func TestQABreak_RestoreKeepsBothWithReportedPath(t *testing.T) {
	ops, root, trashDir := setupTrash(t)
	x := filepath.Join(root, "x.txt")
	qaBreakWrite(t, x, "old")
	if res := ops.MoveToTrash([]string{x}, trashDir); len(res) != 1 || !res[0].OK {
		t.Fatalf("MoveToTrash = %+v", res)
	}
	entries, err := ops.ListTrash(trashDir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("ListTrash = %+v, %v", entries, err)
	}
	qaBreakWrite(t, x, "new")
	res := ops.RestoreFromTrash([]string{entries[0].ID}, trashDir)
	if len(res) != 1 || !res[0].OK {
		t.Fatalf("RestoreFromTrash = %+v", res)
	}
	want := filepath.Join(root, "x (1).txt")
	if res[0].Path != want {
		t.Fatalf("restore path = %q, want %q", res[0].Path, want)
	}
	if got := qaBreakRead(t, x); got != "new" {
		t.Fatalf("occupant of the original path replaced: %q", got)
	}
	if got := qaBreakRead(t, want); got != "old" {
		t.Fatalf("restored content = %q, want old", got)
	}
}

// Trash round trip with collisions: two items with the same basename both get
// distinct trash ids, both restore, and nothing is lost. Source: requirements.md
// item 18 and trash.go:441-455 (unique trash names). Label: code failure.
func TestQABreak_TrashCollisionsRoundTrip(t *testing.T) {
	ops, root, trashDir := setupTrash(t)
	x := filepath.Join(root, "x.txt")
	qaBreakWrite(t, x, "one")
	if res := ops.MoveToTrash([]string{x}, trashDir); len(res) != 1 || !res[0].OK {
		t.Fatalf("MoveToTrash(one) = %+v", res)
	}
	qaBreakWrite(t, x, "two")
	if res := ops.MoveToTrash([]string{x}, trashDir); len(res) != 1 || !res[0].OK {
		t.Fatalf("MoveToTrash(two) = %+v", res)
	}
	entries, err := ops.ListTrash(trashDir)
	if err != nil || len(entries) != 2 {
		t.Fatalf("ListTrash = %+v, %v; want two items", entries, err)
	}
	if entries[0].ID == entries[1].ID {
		t.Fatalf("two trash items share id %q", entries[0].ID)
	}
	res := ops.RestoreFromTrash([]string{entries[0].ID, entries[1].ID}, trashDir)
	if len(res) != 2 || !res[0].OK || !res[1].OK {
		t.Fatalf("RestoreFromTrash = %+v", res)
	}
	if res[0].Path == res[1].Path {
		t.Fatalf("both restores landed on %q", res[0].Path)
	}
	c0 := qaBreakRead(t, res[0].Path)
	c1 := qaBreakRead(t, res[1].Path)
	if !((c0 == "one" && c1 == "two") || (c0 == "two" && c1 == "one")) {
		t.Fatalf("restored contents = %q, %q; want one and two", c0, c1)
	}
}

// Requirement 13 (trash listing): a name with a newline, non-ASCII characters
// and a literal "%41" round-trips through trash with its original path intact.
// Source: openapi.yaml:826-829; trash.go:466-500 (percent-encoding). Label: code failure.
func TestQABreak_AwkwardNamesRoundTripThroughTrash(t *testing.T) {
	ops, root, trashDir := setupTrash(t)
	src := filepath.Join(root, "a\nb é 你 %41 .txt")
	qaBreakWrite(t, src, "u")
	if res := ops.MoveToTrash([]string{src}, trashDir); len(res) != 1 || !res[0].OK {
		t.Fatalf("MoveToTrash = %+v", res)
	}
	entries, err := ops.ListTrash(trashDir)
	if err != nil || len(entries) != 1 || entries[0].OriginalPath != src {
		t.Fatalf("ListTrash = %+v, %v; want original path %q", entries, err, src)
	}
	res := ops.RestoreFromTrash([]string{entries[0].ID}, trashDir)
	if len(res) != 1 || !res[0].OK || res[0].Path != src {
		t.Fatalf("RestoreFromTrash = %+v, want path %q", res, src)
	}
	if got := qaBreakRead(t, src); got != "u" {
		t.Fatalf("restored content = %q", got)
	}
}

// Requirement 19: trash ids that are not single opaque names are refused with
// BAD_REQUEST, and nothing outside the store is read or changed. A hostile
// payload and sidecar are planted beside the store to catch a traversal.
// EmptyTrash with a bad id in the batch deletes nothing. Source: trash.go:46-49,
// 69-80, 228-230, 329-331; openapi.yaml:1131. Label: code failure.
func TestQABreak_TrashIdTraversalRefused(t *testing.T) {
	ops, root, trashDir := setupTrash(t)
	good := filepath.Join(root, "good.txt")
	qaBreakWrite(t, good, "G")
	if res := ops.MoveToTrash([]string{good}, trashDir); len(res) != 1 || !res[0].OK {
		t.Fatalf("MoveToTrash = %+v", res)
	}
	entries, err := ops.ListTrash(trashDir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("ListTrash = %+v, %v", entries, err)
	}
	goodID := entries[0].ID
	stolen := filepath.Join(root, "stolen.txt")
	if err := os.WriteFile(filepath.Join(trashDir, "x"), []byte("PAYLOAD"), 0o600); err != nil {
		t.Fatal(err)
	}
	sidecar := "[Trash Info]\nPath=" + stolen + "\nDeletionDate=2020-01-01T00:00:00\n"
	if err := os.WriteFile(filepath.Join(trashDir, "x.trashinfo"), []byte(sidecar), 0o600); err != nil {
		t.Fatal(err)
	}
	bad := []string{"../x", ".", "..", "a/b", "/etc/passwd"}
	res := ops.RestoreFromTrash(bad, trashDir)
	if len(res) != len(bad) {
		t.Fatalf("RestoreFromTrash returned %d results for %d ids", len(res), len(bad))
	}
	for i, r := range res {
		if r.OK || r.Error == nil || r.Error.Code != "BAD_REQUEST" {
			t.Errorf("RestoreFromTrash(%q) = %+v, want BAD_REQUEST", bad[i], r)
		}
	}
	if _, err := os.Lstat(stolen); !os.IsNotExist(err) {
		t.Fatalf("a traversal id restored a file to %s (err=%v)", stolen, err)
	}
	if got := qaBreakRead(t, filepath.Join(trashDir, "x")); got != "PAYLOAD" {
		t.Fatalf("payload outside the store changed: %q", got)
	}
	if err := ops.EmptyTrash(trashDir, []string{"../x"}); err == nil {
		t.Errorf("EmptyTrash(../x) returned no error")
	}
	if _, err := os.Stat(filepath.Join(trashDir, "x")); err != nil {
		t.Fatalf("payload outside the store deleted: %v", err)
	}
	if err := ops.EmptyTrash(trashDir, []string{goodID, "../x"}); err == nil {
		t.Errorf("EmptyTrash with a bad id returned no error")
	}
	// The planted sidecar is listed too, so look only for the valid item.
	after, err := ListTrash(trashDir)
	if err != nil {
		t.Fatalf("ListTrash: %v", err)
	}
	found := false
	for _, e := range after {
		if e.ID == goodID {
			found = true
		}
	}
	if !found {
		t.Fatalf("a batch with a bad id deleted a valid item: %+v", after)
	}
}

// Requirement 20 (jail-scoped trash): a jailed view lists only items whose
// original path is inside its jail, emptying without ids removes only those,
// and an out-of-jail id is skipped rather than deleted. Source: trash.go:191-196,
// 291-297; openapi.yaml:1139. Label: code failure.
func TestQABreak_JailedTrashScopedToJail(t *testing.T) {
	base := t.TempDir()
	jail := filepath.Join(base, "J")
	if err := os.MkdirAll(jail, 0o755); err != nil {
		t.Fatal(err)
	}
	ops := New([]string{base}, false)
	trashDir := filepath.Join(t.TempDir(), "Trash")
	in := filepath.Join(jail, "in.txt")
	out := filepath.Join(base, "out.txt")
	qaBreakWrite(t, in, "I")
	qaBreakWrite(t, out, "O")
	if res := ops.MoveToTrash([]string{in, out}, trashDir); len(res) != 2 || !res[0].OK || !res[1].OK {
		t.Fatalf("MoveToTrash = %+v", res)
	}
	jailed := ops.Jailed(jail)
	visible, err := jailed.ListTrash(trashDir)
	if err != nil || len(visible) != 1 || visible[0].OriginalPath != in {
		t.Fatalf("jailed ListTrash = %+v, %v; want only %q", visible, err, in)
	}
	all, err := ListTrash(trashDir)
	if err != nil || len(all) != 2 {
		t.Fatalf("store holds %+v, %v; want two items", all, err)
	}
	var outID string
	for _, e := range all {
		if e.OriginalPath == out {
			outID = e.ID
		}
	}
	if outID == "" {
		t.Fatalf("outside item missing from the store: %+v", all)
	}
	if err := jailed.EmptyTrash(trashDir, []string{outID}); err != nil {
		t.Fatalf("jailed EmptyTrash(outside id) = %v", err)
	}
	if after, _ := ListTrash(trashDir); len(after) != 2 {
		t.Fatalf("jailed view deleted an out-of-jail item: %+v", after)
	}
	if err := jailed.EmptyTrash(trashDir, nil); err != nil {
		t.Fatalf("jailed EmptyTrash(all) = %v", err)
	}
	left, err := ListTrash(trashDir)
	if err != nil || len(left) != 1 || left[0].OriginalPath != out {
		t.Fatalf("after jailed empty = %+v, %v; want only the outside item", left, err)
	}
}

// Requirement 21: Compress never overwrites an existing dest; the archive gets
// a "(n)" sibling and the existing file is unchanged. Source: openapi.yaml:944-947;
// archive.go:61-65. Label: code failure.
func TestQABreak_CompressKeepsExistingArchive(t *testing.T) {
	ops, root := setupJail(t)
	a := filepath.Join(root, "a.txt")
	qaBreakWrite(t, a, "A")
	dest := filepath.Join(root, "dest.zip")
	qaBreakWrite(t, dest, "OLD")
	e, err := ops.Compress([]string{a}, dest)
	if err != nil || e == nil {
		t.Fatalf("Compress = %v, %v", e, err)
	}
	if got := qaBreakRead(t, dest); got != "OLD" {
		t.Fatalf("existing archive replaced: %q", got)
	}
	if filepath.Base(e.Path) != "dest (1).zip" {
		t.Fatalf("Entry.Path = %q, want the dest (1).zip sibling", e.Path)
	}
	zr, err := zip.OpenReader(filepath.Join(root, "dest (1).zip"))
	if err != nil {
		t.Fatalf("sibling archive unreadable: %v", err)
	}
	zr.Close()
}

// Requirement 22: a source outside the roots is refused before any byte is
// written: ErrForbidden, no archive, no temp file. Source: openapi.yaml:944-945;
// archive.go:44-56. Label: code failure.
func TestQABreak_CompressRefusesOutsideSourceBeforeWriting(t *testing.T) {
	ops, root := setupJail(t)
	outside := t.TempDir()
	a := filepath.Join(root, "a.txt")
	qaBreakWrite(t, a, "A")
	qaBreakWrite(t, filepath.Join(outside, "secret.txt"), "S")
	dest := filepath.Join(root, "out.zip")
	if _, err := ops.Compress([]string{a, filepath.Join(outside, "secret.txt")}, dest); !errors.Is(err, ErrForbidden) {
		t.Fatalf("Compress with an outside source = %v, want ErrForbidden", err)
	}
	if _, err := os.Lstat(dest); !os.IsNotExist(err) {
		t.Fatalf("archive created despite refusal (err=%v)", err)
	}
	qaBreakNoCompressTemp(t, root, "out.zip")
}

// Requirement 22: an unreadable source makes Compress fail, and neither the
// archive nor its temp file remains. Source: openapi.yaml:944-945; archive.go:70-104.
// Skipped as root, which ignores permission bits. Label: code failure.
func TestQABreak_CompressFailureLeavesNoArchiveOrTemp(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores permission bits")
	}
	ops, root := setupJail(t)
	dir := filepath.Join(root, "d")
	qaBreakWrite(t, filepath.Join(dir, "a.txt"), "A")
	locked := filepath.Join(dir, "b.txt")
	qaBreakWrite(t, locked, "B")
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o644) })
	dest := filepath.Join(root, "out.zip")
	if _, err := ops.Compress([]string{dir}, dest); err == nil {
		t.Fatal("Compress over an unreadable file returned no error")
	}
	if _, err := os.Lstat(dest); !os.IsNotExist(err) {
		t.Fatalf("archive left behind after failure (err=%v)", err)
	}
	qaBreakNoCompressTemp(t, root, "out.zip")
}

// Requirement 23: an entry name that climbs out of destDir is refused, the
// response is an error, and no file is written outside destDir. Source:
// openapi.yaml:969-971; archive.go:267-276. Label: code failure.
func TestQABreak_ExtractZipSlipRefused(t *testing.T) {
	ops, root := setupJail(t)
	out := filepath.Join(root, "out")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ name, outside string }{
		{"../evil.txt", "evil.txt"},
		{"sub/../../evil2.txt", "evil2.txt"},
	} {
		archive := filepath.Join(root, fmt.Sprintf("slip-%d.zip", len(c.name)))
		qaBreakWriteZip(t, archive, []qaBreakZipEntry{{name: c.name, data: []byte("E")}})
		if _, err := ops.Extract(archive, out); err == nil {
			t.Errorf("Extract(%q) returned no error", c.name)
		}
		if _, err := os.Lstat(filepath.Join(root, c.outside)); !os.IsNotExist(err) {
			t.Errorf("entry %q wrote %s outside destDir (err=%v)", c.name, c.outside, err)
		}
	}
}

// Requirement 23: an absolute entry name is refused with an error response, and
// no file lands at its literal absolute path or under destDir. Source:
// requirement 23 as decided at the 2026-10-26 closure; archive.go
// checkEntryNameNotAbsolute.
func TestQABreak_ExtractAbsoluteEntryRefused(t *testing.T) {
	ops, root := setupJail(t)
	out := filepath.Join(root, "out")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	abs := filepath.Join(outside, "abs-evil.txt")
	archive := filepath.Join(root, "abs.zip")
	qaBreakWriteZip(t, archive, []qaBreakZipEntry{{name: abs, data: []byte("E")}})
	_, err := ops.Extract(archive, out)
	if _, statErr := os.Lstat(abs); !os.IsNotExist(statErr) {
		t.Fatalf("absolute entry written at its literal path (err=%v)", statErr)
	}
	// Requirement 23 (2026-10-26 closure): the name is refused, not joined under destDir.
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("Extract with an absolute entry: err = %v, want ErrForbidden", err)
	}
	if _, statErr := os.Lstat(filepath.Join(out, abs)); !os.IsNotExist(statErr) {
		t.Errorf("absolute entry re-rooted under destDir (err=%v)", statErr)
	}
}

// Requirement 24: an entry whose parent is a symlink already in destDir is
// refused, and nothing is written through the link. Source: openapi.yaml:970-971;
// archive.go:282-303 (checkNoSymlinkParent). Label: code failure.
func TestQABreak_ExtractThroughExistingLinkRefused(t *testing.T) {
	ops, root := setupJail(t)
	out := filepath.Join(root, "out")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(out, "l")); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(root, "through.zip")
	qaBreakWriteZip(t, archive, []qaBreakZipEntry{{name: "l/x", data: []byte("X")}})
	if _, err := ops.Extract(archive, out); err == nil {
		t.Errorf("Extract(l/x through a link) returned no error")
	}
	if _, err := os.Lstat(filepath.Join(outside, "x")); !os.IsNotExist(err) {
		t.Fatalf("entry written through the existing link (err=%v)", err)
	}
}

// Requirement 24: archive symlink and hard-link entries never create a link.
// Whether they are skipped or refused is open (openapi.yaml:970-971 says
// "rejected/skipped"), so only the no-link property is asserted. Source:
// archive.go:447-449 (zip), 510-511 (tar default case). Label: code failure.
func TestQABreak_ArchiveLinkEntriesNeverCreateLinks(t *testing.T) {
	ops, root := setupJail(t)
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.txt")
	qaBreakWrite(t, secret, "S")
	secretInfo, err := os.Lstat(secret)
	if err != nil {
		t.Fatal(err)
	}
	zipArc := filepath.Join(root, "links.zip")
	qaBreakWriteZip(t, zipArc, []qaBreakZipEntry{
		{name: "lnk", data: []byte(secret), mode: os.ModeSymlink | 0o777},
		{name: "ok.txt", data: []byte("OK")},
	})
	tgzArc := filepath.Join(root, "links.tar.gz")
	qaBreakWriteTarGz(t, tgzArc, []qaBreakTarEntry{
		{name: "tlnk", typeflag: tar.TypeSymlink, linkname: secret},
		{name: "hlnk", typeflag: tar.TypeLink, linkname: secret},
	})
	cases := []struct {
		label, archive string
		names          []string
	}{
		{"zip", zipArc, []string{"lnk"}},
		{"tar.gz", tgzArc, []string{"tlnk", "hlnk"}},
	}
	for _, c := range cases {
		dest := filepath.Join(root, "out-"+c.label)
		_, _ = ops.Extract(c.archive, dest)
		for _, name := range c.names {
			info, lerr := os.Lstat(filepath.Join(dest, name))
			if lerr != nil {
				continue
			}
			if info.Mode()&os.ModeSymlink != 0 {
				t.Errorf("%s: entry %q created a symlink", c.label, name)
			}
			if os.SameFile(info, secretInfo) {
				t.Errorf("%s: entry %q is a hard link to the outside file", c.label, name)
			}
		}
	}
}

// Requirement 25: extraction never replaces a file already present, and a name
// repeated inside the archive gets a "(n)" sibling. Source: archive.go:317-320,
// 329-342; openapi.yaml (extract). Label: code failure.
func TestQABreak_ExtractNeverReplacesExistingOrRepeatedNames(t *testing.T) {
	ops, root := setupJail(t)
	t.Run("existing", func(t *testing.T) {
		out := filepath.Join(root, "existing")
		qaBreakWrite(t, filepath.Join(out, "f.txt"), "A")
		archive := filepath.Join(root, "existing.zip")
		qaBreakWriteZip(t, archive, []qaBreakZipEntry{{name: "f.txt", data: []byte("B")}})
		if _, err := ops.Extract(archive, out); err != nil {
			t.Fatalf("Extract: %v", err)
		}
		if got := qaBreakRead(t, filepath.Join(out, "f.txt")); got != "A" {
			t.Fatalf("existing file replaced: %q", got)
		}
		if got := qaBreakRead(t, filepath.Join(out, "f (1).txt")); got != "B" {
			t.Fatalf("keep-both extraction = %q, want B", got)
		}
	})
	t.Run("repeated-in-archive", func(t *testing.T) {
		out := filepath.Join(root, "repeat")
		archive := filepath.Join(root, "repeat.zip")
		qaBreakWriteZip(t, archive, []qaBreakZipEntry{
			{name: "d.txt", data: []byte("A")},
			{name: "d.txt", data: []byte("B")},
		})
		if _, err := ops.Extract(archive, out); err != nil {
			t.Fatalf("Extract: %v", err)
		}
		if got := qaBreakRead(t, filepath.Join(out, "d.txt")); got != "A" {
			t.Fatalf("first entry = %q, want A", got)
		}
		if got := qaBreakRead(t, filepath.Join(out, "d (1).txt")); got != "B" {
			t.Fatalf("second entry = %q, want B", got)
		}
	})
}

// File and folder with the same name: an archive that puts "x/y" beside an
// existing file "x" must not replace or damage that file. Undefined by
// requirements beyond "never replace an existing file": safety-only. Label: code failure.
func TestQABreak_ExtractFileAndFolderSameNameKeepsFile(t *testing.T) {
	ops, root := setupJail(t)
	out := filepath.Join(root, "out")
	qaBreakWrite(t, filepath.Join(out, "x"), "A")
	archive := filepath.Join(root, "clash.zip")
	qaBreakWriteZip(t, archive, []qaBreakZipEntry{{name: "x/y", data: []byte("Y")}})
	_, _ = ops.Extract(archive, out)
	if got := qaBreakRead(t, filepath.Join(out, "x")); got != "A" {
		t.Fatalf("existing file x damaged by an entry under x/: %q", got)
	}
}

// Requirement 26: an archive with more than 100,000 entries is refused with
// ErrArchiveTooLarge, and the over-limit entry leaves no file. Source:
// archive.go:233-236, 434-436. Label: code failure. Skipped under -short.
func TestQABreak_ArchiveOver100kEntriesRefused(t *testing.T) {
	if testing.Short() {
		t.Skip("writes and removes 100,001 files; run without -short")
	}
	ops, root := setupJail(t)
	out := filepath.Join(root, "out")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	entries := make([]qaBreakZipEntry, 0, 100001)
	for i := 0; i <= 100000; i++ {
		entries = append(entries, qaBreakZipEntry{name: fmt.Sprintf("f%06d.txt", i), data: []byte("x")})
	}
	archive := filepath.Join(root, "many.zip")
	qaBreakWriteZip(t, archive, entries)
	if _, err := ops.Extract(archive, out); !errors.Is(err, ErrArchiveTooLarge) {
		t.Fatalf("Extract(100,001 entries) = %v, want ErrArchiveTooLarge", err)
	}
	if _, err := os.Lstat(filepath.Join(out, "f100000.txt")); !os.IsNotExist(err) {
		t.Fatalf("over-limit entry was written (err=%v)", err)
	}
}

// Requirement 26: an archive that expands past 2 GiB is refused with
// ErrArchiveTooLarge and the over-limit entry leaves no truncated file. Needs
// about 2 GiB of free disk and a few seconds; skipped under -short. Label: needs environment.
func TestQABreak_ArchiveOver2GiBRefused(t *testing.T) {
	if testing.Short() {
		t.Skip("needs about 2 GiB of free disk; run without -short")
	}
	ops, root := setupJail(t)
	out := filepath.Join(root, "out")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(root, "big.zip")
	f, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	w, err := zw.CreateHeader(&zip.FileHeader{Name: "big.bin", Method: zip.Deflate})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.CopyN(w, qaBreakZeros{}, 2<<30+1); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := ops.Extract(archive, out); !errors.Is(err, ErrArchiveTooLarge) {
		t.Fatalf("Extract(2 GiB + 1 byte) = %v, want ErrArchiveTooLarge", err)
	}
	if _, err := os.Lstat(filepath.Join(out, "big.bin")); !os.IsNotExist(err) {
		t.Fatalf("truncated over-limit file left behind (err=%v)", err)
	}
}

// Requirement 39: a read-only Ops refuses every write with READ_ONLY (batch) or
// ErrReadOnly (single item) and changes nothing on disk. Source: fsops.go:606-607,
// 691-697, 907-913; trash.go:87-88; openapi.yaml:2500-2502. Label: code failure.
func TestQABreak_ReadOnlyOpsChangeNothing(t *testing.T) {
	root := t.TempDir()
	ops := New([]string{root}, true)
	a := filepath.Join(root, "a.txt")
	qaBreakWrite(t, a, "A")
	trashDir := filepath.Join(t.TempDir(), "Trash")
	out := filepath.Join(root, "out")

	if res := ops.Delete([]string{a}); len(res) != 1 || res[0].OK || res[0].Error == nil || res[0].Error.Code != "READ_ONLY" {
		t.Errorf("Delete on read-only = %+v, want READ_ONLY", res)
	}
	if res := ops.Copy([]string{a}, out, false, false); len(res) != 1 || res[0].OK || res[0].Error == nil || res[0].Error.Code != "READ_ONLY" {
		t.Errorf("Copy on read-only = %+v, want READ_ONLY", res)
	}
	if res := ops.Move([]string{a}, out, false, false); len(res) != 1 || res[0].OK {
		t.Errorf("Move on read-only = %+v, want refusal", res)
	}
	if res := ops.MoveToTrash([]string{a}, trashDir); len(res) != 1 || res[0].OK {
		t.Errorf("MoveToTrash on read-only = %+v, want refusal", res)
	}
	if _, err := ops.Rename(a, filepath.Join(root, "b.txt")); !errors.Is(err, ErrReadOnly) {
		t.Errorf("Rename on read-only = %v, want ErrReadOnly", err)
	}
	if _, err := ops.Compress([]string{a}, filepath.Join(root, "z.zip")); !errors.Is(err, ErrReadOnly) {
		t.Errorf("Compress on read-only = %v, want ErrReadOnly", err)
	}
	if _, err := ops.Extract(a, out); !errors.Is(err, ErrReadOnly) {
		t.Errorf("Extract on read-only = %v, want ErrReadOnly", err)
	}
	if got := qaBreakRead(t, a); got != "A" {
		t.Fatalf("read-only Ops changed a file: %q", got)
	}
	for _, p := range []string{out, filepath.Join(root, "b.txt"), filepath.Join(root, "z.zip")} {
		if _, err := os.Lstat(p); !os.IsNotExist(err) {
			t.Errorf("read-only Ops created %s (err=%v)", p, err)
		}
	}
}

// Requirement 13: a permanent delete of a folder with children removes the
// folder and its children, and leaves no trash entry. Source: openapi.yaml:826-829, 839.
// Label: code failure.
func TestQABreak_PermanentDeleteRemovesFolderTree(t *testing.T) {
	ops, root, trashDir := setupTrash(t)
	d := filepath.Join(root, "D")
	qaBreakWrite(t, filepath.Join(d, "sub", "f.txt"), "F")
	if res := ops.Delete([]string{d}); len(res) != 1 || !res[0].OK {
		t.Fatalf("Delete(folder) = %+v", res)
	}
	assertGone(t, d)
	entries, err := ops.ListTrash(trashDir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("permanent delete left trash entries: %+v, %v", entries, err)
	}
}

// Chmod through a symlink to an outside file must not change that file's mode,
// and a relative path is refused. Whether an in-jail link's target may be
// chmodded is Open question 3, so only the outside file is asserted. Source:
// jail.go:679-689; jail.go:126-128. Label: code failure.
func TestQABreak_ChmodThroughOutsideLinkLeavesTargetAlone(t *testing.T) {
	ops, root := setupJail(t)
	outside := t.TempDir()
	secret := filepath.Join(outside, "s.txt")
	qaBreakWrite(t, secret, "S")
	if err := os.Chmod(secret, 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "l")
	if err := os.Symlink(secret, link); err != nil {
		t.Fatal(err)
	}
	_ = ops.Chmod(link, 0o600)
	fi, err := os.Stat(secret)
	if err != nil || fi.Mode().Perm() != 0o644 {
		t.Fatalf("outside file mode changed through a link: %v, %v", fi, err)
	}
	if err := ops.Chmod("relative-probe", 0o600); !errors.Is(err, ErrForbidden) {
		t.Errorf("Chmod(relative) = %v, want ErrForbidden", err)
	}
}
