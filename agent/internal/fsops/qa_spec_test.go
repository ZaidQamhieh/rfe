//go:build !windows

// qa_spec_test.go checks the fsops package against the numbered requirements in
// /tmp/rfe-e2e/requirements.md: Requirements 2-6, 9-11, 13-19 and 21-25.
//
// Each expected result is the requirement's own wording. The code under test is
// reached through the Ops methods only. Requirements 1, 7, 8, 12, 20 and 26 are
// not in the brief's cover list; 27-41 are HTTP, transfer and capability
// requirements; the open questions and Assumed items are not tested.
//
// Partially untestable here: Requirement 19 names a "volume prefix" id. On Linux
// filepath.VolumeName is always empty, so the volume-prefix case has no Linux
// reading and is not tested.
//
// Requirement 22 (unreadable source) is skipped when the test runs as root,
// because root reads mode-000 files.

package fsops

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// qaSpecEnv is one fresh jail for a test. Every path is built from base, which
// is symlink-resolved, so the paths compare equal to what Ops reports.
type qaSpecEnv struct {
	ops     *Ops
	base    string // symlink-resolved temp dir
	root    string // the single configured root
	outside string // a sibling of root, never under a root
	trash   string // trash store, outside every root
}

func qaSpecNewEnv(t *testing.T) qaSpecEnv {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve temp dir: %v", err)
	}
	env := qaSpecEnv{
		base:    base,
		root:    filepath.Join(base, "root"),
		outside: filepath.Join(base, "outside"),
		trash:   filepath.Join(base, "trash"),
	}
	qaSpecMkdir(t, env.root)
	qaSpecMkdir(t, env.outside)
	env.ops = New([]string{env.root}, false)
	return env
}

func qaSpecMkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
}

// qaSpecWriteMode writes body and then chmods to the exact mode, so the umask
// cannot change the mode a test compares.
func qaSpecWriteMode(t *testing.T, path, body string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), mode); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatalf("chmod %s: %v", path, err)
	}
}

func qaSpecWrite(t *testing.T, path, body string) {
	t.Helper()
	qaSpecWriteMode(t, path, body, 0o644)
}

func qaSpecRead(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

// qaSpecOpenBody reads a file through Ops.Open, the same call GET /content makes.
func qaSpecOpenBody(t *testing.T, ops *Ops, path string) string {
	t.Helper()
	f, err := ops.Open(path)
	if err != nil {
		t.Fatalf("Open(%s) = %v, want the file", path, err)
	}
	defer f.Close()
	b, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("read opened %s: %v", path, err)
	}
	return string(b)
}

func qaSpecLink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("symlink %s -> %s: %v", link, target, err)
	}
}

// qaSpecLinkIs checks that link is a symbolic link (not followed) pointing at want.
func qaSpecLinkIs(t *testing.T, link, want string) {
	t.Helper()
	fi, err := os.Lstat(link)
	if err != nil {
		t.Errorf("lstat %s: %v (want a symbolic link)", link, err)
		return
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Errorf("%s is not a symbolic link (mode %v)", link, fi.Mode())
		return
	}
	got, err := os.Readlink(link)
	if err != nil || got != want {
		t.Errorf("%s points to %q (err %v), want %q", link, got, err, want)
	}
}

// qaSpecAbsent fails when p exists. Symbolic links are not followed.
func qaSpecAbsent(t *testing.T, p string) {
	t.Helper()
	if _, err := os.Lstat(p); !os.IsNotExist(err) {
		t.Errorf("%s should not exist (lstat err = %v)", p, err)
	}
}

func qaSpecPresent(t *testing.T, p string) {
	t.Helper()
	if _, err := os.Lstat(p); err != nil {
		t.Errorf("%s should exist: %v", p, err)
	}
}

// qaSpecTree maps each entry under dir (relative path, "." for dir itself) to
// its Lstat mode.
func qaSpecTree(t *testing.T, dir string) map[string]os.FileMode {
	t.Helper()
	out := make(map[string]os.FileMode)
	err := filepath.WalkDir(dir, func(p string, _ os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := os.Lstat(p)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		out[rel] = info.Mode()
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", dir, err)
	}
	return out
}

// qaSpecNoSpecialFiles fails for any entry under dir that is neither a folder
// nor a regular file (symbolic links, hard links, devices, FIFOs and so on).
func qaSpecNoSpecialFiles(t *testing.T, dir string) {
	t.Helper()
	for rel, mode := range qaSpecTree(t, dir) {
		if rel == "." || mode.IsDir() || mode.IsRegular() {
			continue
		}
		t.Errorf("%s was created with mode %v; only regular files and folders may be extracted", rel, mode)
	}
}

// qaSpecNoBytes fails when any regular file under dir contains marker.
func qaSpecNoBytes(t *testing.T, dir, marker string) {
	t.Helper()
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !d.Type().IsRegular() {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if bytes.Contains(b, []byte(marker)) {
			t.Errorf("%s holds the protected bytes %q", p, marker)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", dir, err)
	}
}

// qaSpecNoTempFiles fails when dir holds an entry whose name starts with prefix.
func qaSpecNoTempFiles(t *testing.T, dir, prefix string) {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir %s: %v", dir, err)
	}
	for _, e := range ents {
		if strings.HasPrefix(e.Name(), prefix) {
			t.Errorf("temp file %s left in %s", e.Name(), dir)
		}
	}
}

// qaSpecEntry is one zip member. A zero mode means a regular file.
type qaSpecEntry struct {
	name string
	body string
	mode os.FileMode
}

func qaSpecZip(t *testing.T, archive string, entries []qaSpecEntry) {
	t.Helper()
	f, err := os.Create(archive)
	if err != nil {
		t.Fatalf("create %s: %v", archive, err)
	}
	zw := zip.NewWriter(f)
	for _, e := range entries {
		mode := e.mode
		if mode == 0 {
			mode = 0o644
		}
		fh := &zip.FileHeader{Name: e.name, Method: zip.Store}
		fh.SetMode(mode)
		w, err := zw.CreateHeader(fh)
		if err != nil {
			t.Fatalf("zip header %s: %v", e.name, err)
		}
		if _, err := w.Write([]byte(e.body)); err != nil {
			t.Fatalf("zip write %s: %v", e.name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("file close: %v", err)
	}
}

// qaSpecTarEntry is one tar member. Only TypeReg entries take a body.
type qaSpecTarEntry struct {
	hdr  tar.Header
	body string
}

func qaSpecTarGz(t *testing.T, archive string, entries []qaSpecTarEntry) {
	t.Helper()
	f, err := os.Create(archive)
	if err != nil {
		t.Fatalf("create %s: %v", archive, err)
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		h := e.hdr
		if h.Typeflag == tar.TypeReg {
			h.Size = int64(len(e.body))
		}
		if err := tw.WriteHeader(&h); err != nil {
			t.Fatalf("tar header %s: %v", h.Name, err)
		}
		if h.Typeflag == tar.TypeReg {
			if _, err := tw.Write([]byte(e.body)); err != nil {
				t.Fatalf("tar write %s: %v", h.Name, err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tar close: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("file close: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Requirement 2: DELETE acts on a symbolic link itself, permanently or into
// trash, leaving its target unchanged, including when the target lies outside
// the configured roots.
// ---------------------------------------------------------------------------

func TestReq02_DeleteRemovesLinkItself(t *testing.T) {
	for _, place := range []string{"inside", "outside"} {
		for _, how := range []string{"permanent", "trash"} {
			t.Run(place+"_"+how, func(t *testing.T) {
				env := qaSpecNewEnv(t)
				dir := env.root
				if place == "outside" {
					dir = env.outside
				}
				target := filepath.Join(dir, "target.txt")
				qaSpecWriteMode(t, target, "TARGET-02", 0o640)
				link := filepath.Join(env.root, "link")
				qaSpecLink(t, target, link)

				var res []BatchResult
				if how == "permanent" {
					res = env.ops.Delete([]string{link})
				} else {
					res = env.ops.MoveToTrash([]string{link}, env.trash)
				}
				if len(res) != 1 || !res[0].OK {
					t.Fatalf("%s delete of the link = %+v, want one ok result", how, res)
				}
				qaSpecAbsent(t, link)
				if got := qaSpecRead(t, target); got != "TARGET-02" {
					t.Errorf("target bytes = %q, want %q", got, "TARGET-02")
				}
				fi, err := os.Stat(target)
				if err != nil {
					t.Fatalf("stat target: %v", err)
				}
				if fi.Mode().Perm() != 0o640 {
					t.Errorf("target mode = %v, want 0640", fi.Mode().Perm())
				}
			})
		}
	}
}

// ---------------------------------------------------------------------------
// Requirement 3: rename of a symbolic link renames the link, not its target,
// and the response describes the link (isSymlink true, symlinkTarget set to
// the original target string).
// ---------------------------------------------------------------------------

func TestReq03_RenameRenamesLinkItself(t *testing.T) {
	env := qaSpecNewEnv(t)
	target := filepath.Join(env.root, "target.txt")
	qaSpecWrite(t, target, "TARGET-03")
	link := filepath.Join(env.root, "link")
	qaSpecLink(t, target, link)
	link2 := filepath.Join(env.root, "link2")

	e, err := env.ops.Rename(link, link2)
	if err != nil {
		t.Fatalf("Rename(link, link2) = %v, want success", err)
	}
	if e == nil || !e.IsSymlink || e.SymlinkTarget != target {
		t.Errorf("Rename response = %+v, want isSymlink true and symlinkTarget %q", e, target)
	}
	qaSpecAbsent(t, link)
	qaSpecLinkIs(t, link2, target)
	if got := qaSpecRead(t, target); got != "TARGET-03" {
		t.Errorf("target bytes = %q, want %q", got, "TARGET-03")
	}
}

// ---------------------------------------------------------------------------
// Requirement 4: move of a symbolic link among the sources moves the link
// itself and leaves its target where it is; a destDir that is a link to a
// folder inside the roots is accepted.
// ---------------------------------------------------------------------------

func TestReq04_MoveMovesLinkItself(t *testing.T) {
	env := qaSpecNewEnv(t)
	target := filepath.Join(env.root, "target.txt")
	qaSpecWrite(t, target, "TARGET-04")
	link := filepath.Join(env.root, "link")
	qaSpecLink(t, target, link)
	dirA := filepath.Join(env.root, "dirA")
	qaSpecMkdir(t, dirA)

	res := env.ops.Move([]string{link}, dirA, false, false)
	if len(res) != 1 || !res[0].OK {
		t.Fatalf("Move(link, dirA) = %+v, want one ok result", res)
	}
	qaSpecAbsent(t, link)
	qaSpecLinkIs(t, filepath.Join(dirA, "link"), target)
	if got := qaSpecRead(t, target); got != "TARGET-04" {
		t.Errorf("target bytes = %q, want %q", got, "TARGET-04")
	}

	// destDir is a symbolic link to a folder inside the roots.
	dirB := filepath.Join(env.root, "dirB")
	qaSpecMkdir(t, dirB)
	dirLink := filepath.Join(env.root, "dirLink")
	qaSpecLink(t, dirB, dirLink)
	moved := filepath.Join(dirA, "link")
	res = env.ops.Move([]string{moved}, dirLink, false, false)
	if len(res) != 1 || !res[0].OK {
		t.Fatalf("Move(link, dirLink) = %+v, want one ok result", res)
	}
	qaSpecAbsent(t, moved)
	qaSpecLinkIs(t, filepath.Join(dirB, "link"), target)
	if got := qaSpecRead(t, target); got != "TARGET-04" {
		t.Errorf("target bytes = %q, want %q", got, "TARGET-04")
	}
}

// ---------------------------------------------------------------------------
// Requirement 5: copy of a symbolic-link source copies the target's content
// under the link's own name; links inside a copied folder are recreated as
// links with the same target string, not dereferenced.
// ---------------------------------------------------------------------------

// Requirement 5(a): link L -> file with bytes B; copy to D gives D/L as a
// regular file with bytes B.
func TestReq05_CopyLinkCopiesTargetContent(t *testing.T) {
	env := qaSpecNewEnv(t)
	file := filepath.Join(env.root, "file.txt")
	qaSpecWrite(t, file, "BYTES-05")
	link := filepath.Join(env.root, "lnk")
	qaSpecLink(t, file, link)
	dest := filepath.Join(env.root, "D")
	qaSpecMkdir(t, dest)

	res := env.ops.Copy([]string{link}, dest, false, false)
	if len(res) != 1 || !res[0].OK {
		t.Fatalf("Copy(link, D) = %+v, want one ok result", res)
	}
	copied := filepath.Join(dest, "lnk")
	fi, err := os.Lstat(copied)
	if err != nil {
		t.Fatalf("lstat copied link name: %v", err)
	}
	if !fi.Mode().IsRegular() {
		t.Errorf("D/lnk mode = %v, want a regular file under the link's name", fi.Mode())
	}
	if got := qaSpecRead(t, copied); got != "BYTES-05" {
		t.Errorf("D/lnk bytes = %q, want %q", got, "BYTES-05")
	}
}

// Requirement 5(b): folder F containing X -> T; copy F to D gives a symlink at
// D/F/X with Readlink == T, and no bytes of T written under D/F.
func TestReq05_CopyFolderKeepsInnerLinks(t *testing.T) {
	env := qaSpecNewEnv(t)
	target := filepath.Join(env.outside, "t.txt")
	qaSpecWrite(t, target, "TARGET-05")
	folder := filepath.Join(env.root, "F")
	qaSpecMkdir(t, folder)
	inner := filepath.Join(folder, "X")
	qaSpecLink(t, target, inner)
	dest := filepath.Join(env.root, "D")
	qaSpecMkdir(t, dest)

	res := env.ops.Copy([]string{folder}, dest, false, false)
	if len(res) != 1 || !res[0].OK {
		t.Fatalf("Copy(F, D) = %+v, want one ok result", res)
	}
	qaSpecLinkIs(t, filepath.Join(dest, "F", "X"), target)
	for rel, mode := range qaSpecTree(t, filepath.Join(dest, "F")) {
		if mode.IsRegular() {
			t.Errorf("D/F/%s is a regular file; the link target was dereferenced", rel)
		}
	}
	if got := qaSpecRead(t, target); got != "TARGET-05" {
		t.Errorf("target bytes = %q, want %q", got, "TARGET-05")
	}
}

// ---------------------------------------------------------------------------
// Requirement 6: a link whose target lies outside the roots yields none of the
// target's bytes through copy or through GET /content.
// ---------------------------------------------------------------------------

func TestReq06_OutsideTargetLinkYieldsNoBytes(t *testing.T) {
	env := qaSpecNewEnv(t)
	secret := filepath.Join(env.outside, "secret")
	qaSpecWrite(t, secret, "TOP-SECRET-06")
	link := filepath.Join(env.root, "L")
	qaSpecLink(t, secret, link)
	dest := filepath.Join(env.root, "D")
	qaSpecMkdir(t, dest)

	res := env.ops.Copy([]string{link}, dest, false, false)
	if len(res) != 1 || res[0].OK || res[0].Error == nil {
		t.Errorf("Copy of an outside-target link = %+v, want a per-item error", res)
	}
	qaSpecNoBytes(t, dest, "TOP-SECRET-06")

	// GET /content maps to Open on the link path.
	f, err := env.ops.Open(link)
	if f != nil {
		f.Close()
		t.Errorf("Open(link) returned a file; the target's bytes must not be served")
	}
	if !errors.Is(err, ErrForbidden) {
		t.Errorf("Open(link) err = %v, want ErrForbidden", err)
	}
}

// ---------------------------------------------------------------------------
// Requirement 9: dot-dot components are removed before the jail decision.
// A cleaned path inside a root is accepted; one that leaves the root is refused
// and the file it names is untouched.
// ---------------------------------------------------------------------------

func TestReq09_DotDotCleanedBeforeJailCheck(t *testing.T) {
	env := qaSpecNewEnv(t)
	qaSpecMkdir(t, filepath.Join(env.root, "sub"))
	qaSpecWrite(t, filepath.Join(env.root, "plain.txt"), "PLAIN-09")
	outsideX := filepath.Join(env.outside, "x")
	qaSpecWrite(t, outsideX, "OUTSIDE-09")

	// Built by concatenation: filepath.Join would clean the dot-dot parts first.
	inside := env.root + "/sub/../plain.txt"
	e, err := env.ops.Meta(inside)
	if err != nil {
		t.Errorf("Meta(%s) = %v, want accepted", inside, err)
	} else if e.Name != "plain.txt" {
		t.Errorf("Meta(%s).Name = %q, want %q", inside, e.Name, "plain.txt")
	}
	if got := qaSpecOpenBody(t, env.ops, inside); got != "PLAIN-09" {
		t.Errorf("Open(%s) bytes = %q, want %q", inside, got, "PLAIN-09")
	}

	escape := env.root + "/sub/../../outside/x"
	if _, err := env.ops.Meta(escape); !errors.Is(err, ErrForbidden) {
		t.Errorf("Meta(%s) err = %v, want ErrForbidden", escape, err)
	}
	if f, err := env.ops.Open(escape); err == nil {
		f.Close()
		t.Errorf("Open(%s) succeeded, want ErrForbidden", escape)
	} else if !errors.Is(err, ErrForbidden) {
		t.Errorf("Open(%s) err = %v, want ErrForbidden", escape, err)
	}
	res := env.ops.Delete([]string{escape})
	if len(res) != 1 || res[0].OK || res[0].Error == nil || res[0].Error.Code != "FORBIDDEN" {
		t.Errorf("Delete(%s) = %+v, want a FORBIDDEN per-item result", escape, res)
	}
	if got := qaSpecRead(t, outsideX); got != "OUTSIDE-09" {
		t.Errorf("outside file bytes = %q, want %q", got, "OUTSIDE-09")
	}
}

// ---------------------------------------------------------------------------
// Requirement 10: a symbolic link in an intermediate component is not followed
// when its target lies outside the root. A request naming R/link/... is refused
// and creates or reads nothing under the link target.
// ---------------------------------------------------------------------------

func TestReq10_IntermediateLinkOutsideRefused(t *testing.T) {
	env := qaSpecNewEnv(t)
	keep := filepath.Join(env.outside, "keep.txt")
	qaSpecWrite(t, keep, "KEEP-10")
	link := filepath.Join(env.root, "link")
	qaSpecLink(t, env.outside, link)

	if _, err := env.ops.CreateFolder(link + "/new"); !errors.Is(err, ErrForbidden) {
		t.Errorf("CreateFolder(link/new) err = %v, want ErrForbidden", err)
	}
	qaSpecAbsent(t, filepath.Join(env.outside, "new"))
	if _, err := env.ops.ListDir(link, "", 100); !errors.Is(err, ErrForbidden) {
		t.Errorf("ListDir(link) err = %v, want ErrForbidden", err)
	}
	if _, err := env.ops.ListDir(link+"/keep.txt", "", 100); !errors.Is(err, ErrForbidden) {
		t.Errorf("ListDir(link/keep.txt) err = %v, want ErrForbidden", err)
	}
	if got := qaSpecRead(t, keep); got != "KEEP-10" {
		t.Errorf("outside file bytes = %q, want %q", got, "KEEP-10")
	}
}

// ---------------------------------------------------------------------------
// Requirement 11: after ProtectDir(D), every Ops method refuses D and D/file,
// and Copy into or out of D; a path beside D still works. Checked with a root
// configured and with no roots configured.
// ---------------------------------------------------------------------------

func TestReq11_ProtectedDirRefusedThroughEveryMethod(t *testing.T) {
	for _, scope := range []string{"rooted", "unrestricted"} {
		t.Run(scope, func(t *testing.T) {
			env := qaSpecNewEnv(t)
			ops := env.ops
			if scope == "unrestricted" {
				ops = New(nil, false)
			}
			data := filepath.Join(env.root, "data")
			qaSpecMkdir(t, data)
			file := filepath.Join(data, "file.txt")
			qaSpecWrite(t, file, "PROTECTED-11")
			beside := filepath.Join(env.root, "beside.txt")
			qaSpecWrite(t, beside, "BESIDE-11")
			ProtectDir(data)

			if _, err := ops.ListDir(data, "", 100); !errors.Is(err, ErrForbidden) {
				t.Errorf("ListDir(D) err = %v, want ErrForbidden", err)
			}
			if _, err := ops.Meta(data); !errors.Is(err, ErrForbidden) {
				t.Errorf("Meta(D) err = %v, want ErrForbidden", err)
			}
			if _, err := ops.Meta(file); !errors.Is(err, ErrForbidden) {
				t.Errorf("Meta(D/file) err = %v, want ErrForbidden", err)
			}
			if f, err := ops.Open(file); err == nil {
				f.Close()
				t.Errorf("Open(D/file) succeeded, want ErrForbidden")
			} else if !errors.Is(err, ErrForbidden) {
				t.Errorf("Open(D/file) err = %v, want ErrForbidden", err)
			}
			for i, r := range ops.Delete([]string{file, data}) {
				if r.OK || r.Error == nil || r.Error.Code != "FORBIDDEN" {
					t.Errorf("Delete[%d] = %+v, want FORBIDDEN", i, r)
				}
			}
			for i, r := range ops.Copy([]string{beside}, data, false, false) {
				if r.OK || r.Error == nil || r.Error.Code != "FORBIDDEN" {
					t.Errorf("Copy into D [%d] = %+v, want FORBIDDEN", i, r)
				}
			}
			for i, r := range ops.Copy([]string{file}, env.root, false, false) {
				if r.OK || r.Error == nil || r.Error.Code != "FORBIDDEN" {
					t.Errorf("Copy out of D [%d] = %+v, want FORBIDDEN", i, r)
				}
			}
			if got := qaSpecRead(t, file); got != "PROTECTED-11" {
				t.Errorf("D/file bytes = %q, want %q", got, "PROTECTED-11")
			}
			if e, err := ops.Meta(beside); err != nil || e.Name != "beside.txt" {
				t.Errorf("Meta(beside D) = %+v, %v; want beside.txt accepted", e, err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Requirement 13: a default delete (MoveToTrash) lists the item in the trash with
// its original path; a permanent delete of a folder with children leaves
// neither the folder nor the children, and adds no trash entry.
// ---------------------------------------------------------------------------

func TestReq13_TrashDefaultAndPermanentRecursive(t *testing.T) {
	env := qaSpecNewEnv(t)
	file := filepath.Join(env.root, "doc.txt")
	qaSpecWrite(t, file, "DOC-13")

	res := env.ops.MoveToTrash([]string{file}, env.trash)
	if len(res) != 1 || !res[0].OK {
		t.Fatalf("MoveToTrash(file) = %+v, want one ok result", res)
	}
	qaSpecAbsent(t, file)
	entries, err := env.ops.ListTrash(env.trash)
	if err != nil {
		t.Fatalf("ListTrash: %v", err)
	}
	if len(entries) != 1 || entries[0].OriginalPath != file {
		t.Fatalf("ListTrash = %+v, want one entry with OriginalPath %q", entries, file)
	}

	tree := filepath.Join(env.root, "tree")
	qaSpecMkdir(t, filepath.Join(tree, "child", "deeper"))
	qaSpecWrite(t, filepath.Join(tree, "child", "deeper", "leaf.txt"), "LEAF-13")
	qaSpecWrite(t, filepath.Join(tree, "top.txt"), "TOP-13")

	res = env.ops.Delete([]string{tree})
	if len(res) != 1 || !res[0].OK {
		t.Fatalf("Delete(tree) = %+v, want one ok result", res)
	}
	qaSpecAbsent(t, tree)
	qaSpecAbsent(t, filepath.Join(tree, "child", "deeper", "leaf.txt"))
	entries, err = env.ops.ListTrash(env.trash)
	if err != nil {
		t.Fatalf("ListTrash after permanent delete: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("ListTrash after permanent delete = %+v, want still only the first entry", entries)
	}
}

// ---------------------------------------------------------------------------
// Requirement 14: copy or move with overwrite=true and duplicate=false replaces
// an existing destination; with overwrite=false it yields a per-item CONFLICT
// and leaves the destination unchanged.
// ---------------------------------------------------------------------------

func TestReq14_OverwriteFlagDecidesReplacement(t *testing.T) {
	for _, op := range []string{"copy", "move"} {
		t.Run(op, func(t *testing.T) {
			env := qaSpecNewEnv(t)
			srcDir := filepath.Join(env.root, "src")
			destDir := filepath.Join(env.root, "dest")
			qaSpecMkdir(t, srcDir)
			qaSpecMkdir(t, destDir)
			src := filepath.Join(srcDir, "F")
			dst := filepath.Join(destDir, "F")
			qaSpecWrite(t, src, "NEW-14")
			qaSpecWrite(t, dst, "OLD-14")
			run := func(overwrite bool) []BatchResult {
				if op == "copy" {
					return env.ops.Copy([]string{src}, destDir, false, overwrite)
				}
				return env.ops.Move([]string{src}, destDir, false, overwrite)
			}

			res := run(false)
			if len(res) != 1 || res[0].OK || res[0].Error == nil || res[0].Error.Code != "CONFLICT" {
				t.Errorf("%s without overwrite = %+v, want a per-item CONFLICT", op, res)
			}
			if got := qaSpecRead(t, dst); got != "OLD-14" {
				t.Errorf("destination bytes after refused %s = %q, want %q", op, got, "OLD-14")
			}

			res = run(true)
			if len(res) != 1 || !res[0].OK {
				t.Fatalf("%s with overwrite = %+v, want one ok result", op, res)
			}
			if got := qaSpecRead(t, dst); got != "NEW-14" {
				t.Errorf("destination bytes after overwrite %s = %q, want %q", op, got, "NEW-14")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Requirement 15: duplicate=true creates a non-colliding name on collision and
// never replaces an existing destination, even with overwrite=true.
// ---------------------------------------------------------------------------

func TestReq15_DuplicateNeverReplacesDestination(t *testing.T) {
	for _, op := range []string{"copy", "move"} {
		t.Run(op, func(t *testing.T) {
			env := qaSpecNewEnv(t)
			srcDir := filepath.Join(env.root, "src")
			destDir := filepath.Join(env.root, "dest")
			qaSpecMkdir(t, srcDir)
			qaSpecMkdir(t, destDir)
			src := filepath.Join(srcDir, "F")
			dst := filepath.Join(destDir, "F")
			qaSpecWrite(t, src, "NEW-15")
			qaSpecWrite(t, dst, "OLD-15")

			var res []BatchResult
			if op == "copy" {
				res = env.ops.Copy([]string{src}, destDir, true, true)
			} else {
				res = env.ops.Move([]string{src}, destDir, true, true)
			}
			if len(res) != 1 || !res[0].OK {
				t.Fatalf("%s with duplicate and overwrite = %+v, want one ok result", op, res)
			}
			if got := qaSpecRead(t, dst); got != "OLD-15" {
				t.Errorf("existing destination bytes = %q, want %q", got, "OLD-15")
			}
			if got := qaSpecRead(t, filepath.Join(destDir, "F (1)")); got != "NEW-15" {
				t.Errorf("duplicate \"F (1)\" bytes = %q, want %q", got, "NEW-15")
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Requirement 16(a): a failure in the copy during an overwrite leaves the
// original destination intact. The failure is injected with a socket in the
// source folder, which cannot be copied as file content.
// ---------------------------------------------------------------------------

func TestReq16_FailedOverwriteKeepsDestination(t *testing.T) {
	env := qaSpecNewEnv(t)
	srcDir := filepath.Join(env.root, "src", "S")
	qaSpecMkdir(t, srcDir)
	qaSpecWrite(t, filepath.Join(srcDir, "ok.txt"), "OK-16")
	ln, err := net.Listen("unix", filepath.Join(srcDir, "blocker.sock"))
	if err != nil {
		t.Fatalf("listen on unix socket fixture: %v", err)
	}
	defer ln.Close()

	destDir := filepath.Join(env.root, "dest")
	dest := filepath.Join(destDir, "S")
	qaSpecMkdir(t, dest)
	qaSpecWrite(t, filepath.Join(dest, "keep.txt"), "ORIGINAL-16")

	res := env.ops.Copy([]string{srcDir}, destDir, false, true)
	if len(res) != 1 || res[0].OK {
		t.Errorf("overwrite copy with an uncopyable child = %+v, want a per-item failure", res)
	}
	if got := qaSpecRead(t, filepath.Join(dest, "keep.txt")); got != "ORIGINAL-16" {
		t.Errorf("destination bytes after failed overwrite = %q, want %q", got, "ORIGINAL-16")
	}
}

// ---------------------------------------------------------------------------
// Requirement 16(b): an overwrite never deletes a destination that contains the
// source. Moving a folder into its own subfolder returns an error and leaves the
// folder intact; a destination that is an ancestor of the source is refused.
// ---------------------------------------------------------------------------

func TestReq16_OverwriteNeverRemovesContainingDestination(t *testing.T) {
	t.Run("move folder into its own subfolder", func(t *testing.T) {
		env := qaSpecNewEnv(t)
		folder := filepath.Join(env.root, "D")
		sub := filepath.Join(folder, "sub")
		qaSpecMkdir(t, sub)
		qaSpecWrite(t, filepath.Join(folder, "keep.txt"), "KEEP-16")

		res := env.ops.Move([]string{folder}, sub, false, true)
		if len(res) != 1 || res[0].OK || res[0].Error == nil {
			t.Errorf("Move(D, D/sub, overwrite) = %+v, want an error", res)
		}
		if got := qaSpecRead(t, filepath.Join(folder, "keep.txt")); got != "KEEP-16" {
			t.Errorf("D/keep.txt bytes = %q, want %q", got, "KEEP-16")
		}
		qaSpecPresent(t, sub)
	})

	t.Run("copy onto an ancestor of the source", func(t *testing.T) {
		env := qaSpecNewEnv(t)
		src := filepath.Join(env.root, "z", "q", "z")
		qaSpecMkdir(t, src)
		qaSpecWrite(t, filepath.Join(src, "keep.txt"), "SRC-16")

		res := env.ops.Copy([]string{src}, env.root, false, true)
		if len(res) != 1 || res[0].OK {
			t.Errorf("Copy(root/z/q/z, root, overwrite) = %+v, want a per-item failure", res)
		}
		if got := qaSpecRead(t, filepath.Join(src, "keep.txt")); got != "SRC-16" {
			t.Errorf("source bytes = %q, want %q", got, "SRC-16")
		}
	})

	t.Run("move onto an ancestor of the source", func(t *testing.T) {
		env := qaSpecNewEnv(t)
		src := filepath.Join(env.root, "z", "q", "z")
		qaSpecMkdir(t, src)
		qaSpecWrite(t, filepath.Join(src, "keep.txt"), "SRC-16")

		res := env.ops.Move([]string{src}, env.root, false, true)
		if len(res) != 1 || res[0].OK {
			t.Errorf("Move(root/z/q/z, root, overwrite) = %+v, want a per-item failure", res)
		}
		if got := qaSpecRead(t, filepath.Join(src, "keep.txt")); got != "SRC-16" {
			t.Errorf("source bytes = %q, want %q", got, "SRC-16")
		}
	})
}

// ---------------------------------------------------------------------------
// Requirement 17: rename never replaces an existing file or folder (ErrConflict,
// the 409 CONFLICT) and leaves both entries unchanged; renaming a folder into
// its own subtree is a conflict and creates nothing.
// ---------------------------------------------------------------------------

func TestReq17_RenameNeverReplacesOrNests(t *testing.T) {
	t.Run("onto an existing file", func(t *testing.T) {
		env := qaSpecNewEnv(t)
		a := filepath.Join(env.root, "a.txt")
		b := filepath.Join(env.root, "b.txt")
		qaSpecWrite(t, a, "A-17")
		qaSpecWrite(t, b, "B-17")

		if _, err := env.ops.Rename(a, b); !errors.Is(err, ErrConflict) {
			t.Errorf("Rename onto existing file err = %v, want ErrConflict", err)
		}
		if got := qaSpecRead(t, a); got != "A-17" {
			t.Errorf("a.txt bytes = %q, want %q", got, "A-17")
		}
		if got := qaSpecRead(t, b); got != "B-17" {
			t.Errorf("b.txt bytes = %q, want %q", got, "B-17")
		}
	})

	t.Run("folder into its own subtree", func(t *testing.T) {
		env := qaSpecNewEnv(t)
		d := filepath.Join(env.root, "D")
		qaSpecMkdir(t, d)
		qaSpecWrite(t, filepath.Join(d, "f.txt"), "F-17")

		if _, err := env.ops.Rename(d, filepath.Join(d, "sub", "x")); !errors.Is(err, ErrConflict) {
			t.Errorf("Rename(D, D/sub/x) err = %v, want ErrConflict", err)
		}
		qaSpecAbsent(t, filepath.Join(d, "sub"))
		if got := qaSpecRead(t, filepath.Join(d, "f.txt")); got != "F-17" {
			t.Errorf("D/f.txt bytes = %q, want %q", got, "F-17")
		}
	})
}

// ---------------------------------------------------------------------------
// Requirement 18: restore never replaces an occupied original path. It uses a
// keep-both name, and the BatchResult path is the name actually used.
// ---------------------------------------------------------------------------

func TestReq18_RestoreKeepsBothName(t *testing.T) {
	env := qaSpecNewEnv(t)
	orig := filepath.Join(env.root, "note")
	qaSpecWrite(t, orig, "OLD-18")
	if res := env.ops.MoveToTrash([]string{orig}, env.trash); len(res) != 1 || !res[0].OK {
		t.Fatalf("MoveToTrash(note) = %+v, want one ok result", res)
	}
	entries, err := env.ops.ListTrash(env.trash)
	if err != nil || len(entries) != 1 {
		t.Fatalf("ListTrash = %+v, %v; want one entry", entries, err)
	}
	qaSpecWrite(t, orig, "NEW-18")

	res := env.ops.RestoreFromTrash([]string{entries[0].ID}, env.trash)
	if len(res) != 1 || !res[0].OK {
		t.Fatalf("RestoreFromTrash = %+v, want one ok result", res)
	}
	want := filepath.Join(env.root, "note (1)")
	if filepath.Clean(res[0].Path) != want {
		t.Errorf("restore BatchResult.Path = %q, want %q", res[0].Path, want)
	}
	if got := qaSpecRead(t, want); got != "OLD-18" {
		t.Errorf("restored bytes = %q, want %q", got, "OLD-18")
	}
	if got := qaSpecRead(t, orig); got != "NEW-18" {
		t.Errorf("occupied original bytes = %q, want %q", got, "NEW-18")
	}
}

// ---------------------------------------------------------------------------
// Requirement 19: a trash id must be a single opaque name. An id with a
// separator, or equal to "." or "..", is refused with BAD_REQUEST (restore) or
// ErrBadTrashID (empty), and nothing outside the store is read or changed.
// The volume-prefix case is not tested on Linux (see the package header).
// ---------------------------------------------------------------------------

func TestReq19_TrashIDMustBeOpaqueName(t *testing.T) {
	cases := []struct {
		name string
		id   string
	}{
		{"parent_escape", "../x"},
		{"separator", "a/b"},
		{"absolute", "/abs"},
		{"dot", "."},
		{"dotdot", ".."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := qaSpecNewEnv(t)
			qaSpecMkdir(t, env.trash)
			// For id "../x" the store path would reach trash/x and trash/x.trashinfo.
			// Plant a decoy there whose restore target must never be created.
			restoreTarget := filepath.Join(env.base, "restored-19")
			decoy := filepath.Join(env.trash, "x")
			decoyInfo := decoy + ".trashinfo"
			qaSpecWrite(t, decoy, "DECOY-19")
			qaSpecWrite(t, decoyInfo, "[Trash Info]\nPath="+restoreTarget+"\nDeletionDate=2020-01-01T00:00:00\n")

			res := env.ops.RestoreFromTrash([]string{tc.id}, env.trash)
			if len(res) != 1 || res[0].OK || res[0].Error == nil || res[0].Error.Code != "BAD_REQUEST" {
				t.Errorf("RestoreFromTrash(%q) = %+v, want BAD_REQUEST", tc.id, res)
			}
			if err := env.ops.EmptyTrash(env.trash, []string{tc.id}); !errors.Is(err, ErrBadTrashID) {
				t.Errorf("EmptyTrash(%q) err = %v, want ErrBadTrashID", tc.id, err)
			}
			qaSpecAbsent(t, restoreTarget)
			if got := qaSpecRead(t, decoy); got != "DECOY-19" {
				t.Errorf("decoy payload = %q, want unchanged %q", got, "DECOY-19")
			}
			qaSpecPresent(t, decoyInfo)
		})
	}
}

// ---------------------------------------------------------------------------
// Requirement 21: compress never overwrites an existing file. An existing dest
// becomes a "(n)" sibling, dest keeps its bytes, and the returned Entry carries
// the actual path.
// ---------------------------------------------------------------------------

func TestReq21_CompressKeepsExistingArchive(t *testing.T) {
	env := qaSpecNewEnv(t)
	src := filepath.Join(env.root, "src.txt")
	qaSpecWrite(t, src, "SRC-21")
	dest := filepath.Join(env.root, "dest.zip")
	qaSpecWrite(t, dest, "OLD-ARCHIVE-21")

	e, err := env.ops.Compress([]string{src}, dest)
	if err != nil {
		t.Fatalf("Compress = %v, want success", err)
	}
	actual := filepath.Join(env.root, "dest (1).zip")
	if e == nil || filepath.Clean(e.Path) != actual {
		t.Errorf("Compress Entry = %+v, want Path %q", e, actual)
	}
	if got := qaSpecRead(t, dest); got != "OLD-ARCHIVE-21" {
		t.Errorf("existing dest.zip bytes = %q, want %q", got, "OLD-ARCHIVE-21")
	}
	if zr, err := zip.OpenReader(actual); err != nil {
		t.Errorf("created archive %s is not a readable zip: %v", actual, err)
	} else {
		zr.Close()
	}
}

// ---------------------------------------------------------------------------
// Requirement 22: compress checks each source and the destination against the
// jail before writing any byte, and leaves neither a partial archive nor a temp
// file after a failure.
// ---------------------------------------------------------------------------

// Requirement 22(a): one source outside the roots returns ErrForbidden, and no
// dest is created.
func TestReq22_CompressChecksJailBeforeWriting(t *testing.T) {
	env := qaSpecNewEnv(t)
	ok := filepath.Join(env.root, "ok.txt")
	qaSpecWrite(t, ok, "OK-22")
	secret := filepath.Join(env.outside, "secret.txt")
	qaSpecWrite(t, secret, "S-22")
	dest := filepath.Join(env.root, "out.zip")

	if _, err := env.ops.Compress([]string{ok, secret}, dest); !errors.Is(err, ErrForbidden) {
		t.Errorf("Compress with an outside source err = %v, want ErrForbidden", err)
	}
	qaSpecAbsent(t, dest)
	qaSpecNoTempFiles(t, env.root, ".out.zip.rfe-tmp-")
}

// Requirement 22(b): an unreadable source yields an error, and neither dest.zip
// nor a ".dest.zip.rfe-tmp-*" file remains.
func TestReq22_CompressFailureLeavesNoArchive(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads mode-000 files; run this test as a non-root user")
	}
	env := qaSpecNewEnv(t)
	ok := filepath.Join(env.root, "ok.txt")
	qaSpecWrite(t, ok, "OK-22")
	locked := filepath.Join(env.root, "locked.txt")
	qaSpecWriteMode(t, locked, "LOCKED-22", 0o000)
	t.Cleanup(func() { _ = os.Chmod(locked, 0o644) })
	dest := filepath.Join(env.root, "dest.zip")

	if _, err := env.ops.Compress([]string{ok, locked}, dest); err == nil {
		t.Errorf("Compress with an unreadable source returned no error, want an error")
	}
	qaSpecAbsent(t, dest)
	qaSpecNoTempFiles(t, env.root, ".dest.zip.rfe-tmp-")
}

// ---------------------------------------------------------------------------
// Requirement 23: extract never writes a byte outside destDir, and an archive
// entry whose name escapes destDir is refused (the response is an error).
// ---------------------------------------------------------------------------

func TestReq23_ExtractRefusesEscapingEntry(t *testing.T) {
	t.Run("parent traversal", func(t *testing.T) {
		env := qaSpecNewEnv(t)
		dest := filepath.Join(env.root, "dest")
		qaSpecMkdir(t, dest)
		archive := filepath.Join(env.root, "evil.zip")
		qaSpecZip(t, archive, []qaSpecEntry{{name: "../evil.txt", body: "EVIL-23"}})

		if _, err := env.ops.Extract(archive, dest); err == nil {
			t.Errorf("Extract with a ../ entry returned no error, want a refusal")
		}
		qaSpecAbsent(t, filepath.Join(env.root, "evil.txt"))
	})

	t.Run("absolute name", func(t *testing.T) {
		env := qaSpecNewEnv(t)
		dest := filepath.Join(env.root, "dest")
		qaSpecMkdir(t, dest)
		abs := filepath.Join(env.base, "abs-target", "evil.txt")
		archive := filepath.Join(env.root, "abs.zip")
		qaSpecZip(t, archive, []qaSpecEntry{{name: abs, body: "EVIL-23"}})

		// Requirement 23 (2026-10-26 closure): an absolute entry name is refused,
		// neither written at its literal path nor re-rooted under destDir.
		if _, err := env.ops.Extract(archive, dest); !errors.Is(err, ErrForbidden) {
			t.Fatalf("Extract with an absolute entry: err = %v, want ErrForbidden", err)
		}
		qaSpecAbsent(t, abs)
		qaSpecAbsent(t, filepath.Join(dest, abs))
	})
}

// ---------------------------------------------------------------------------
// Requirement 24: extract never writes through a link already in destDir or in
// an entry's parent path, and creates no symlink, hard-link or device entry.
// ---------------------------------------------------------------------------

// Requirement 24(a): destDir/l -> O; a zip entry "l/x" is refused and O/x is absent.
func TestReq24_ExtractNeverWritesThroughExistingLink(t *testing.T) {
	env := qaSpecNewEnv(t)
	dest := filepath.Join(env.root, "dest")
	qaSpecMkdir(t, dest)
	qaSpecLink(t, env.outside, filepath.Join(dest, "l"))
	archive := filepath.Join(env.root, "leak.zip")
	qaSpecZip(t, archive, []qaSpecEntry{{name: "l/x", body: "LEAK-24"}})

	// The refusal may be an error or a skip (Open question 8), so only the
	// outcome outside destDir is asserted.
	_, _ = env.ops.Extract(archive, dest)
	qaSpecAbsent(t, filepath.Join(env.outside, "x"))
}

// Requirement 24(b): a zip or tar with a symlink entry creates no link (and no
// hard-link or device entry either).
func TestReq24_ExtractCreatesNoSpecialEntries(t *testing.T) {
	t.Run("zip", func(t *testing.T) {
		env := qaSpecNewEnv(t)
		dest := filepath.Join(env.root, "dest")
		qaSpecMkdir(t, dest)
		archive := filepath.Join(env.root, "special.zip")
		qaSpecZip(t, archive, []qaSpecEntry{
			{name: "lnk", body: env.outside, mode: os.ModeSymlink | 0o777},
			{name: "dev", body: "", mode: os.ModeDevice | 0o666},
			{name: "plain.txt", body: "PLAIN-24"},
		})

		_, _ = env.ops.Extract(archive, dest)
		qaSpecNoSpecialFiles(t, dest)
	})

	t.Run("tar.gz", func(t *testing.T) {
		env := qaSpecNewEnv(t)
		dest := filepath.Join(env.root, "dest")
		qaSpecMkdir(t, dest)
		hardTarget := filepath.Join(env.outside, "h")
		qaSpecWrite(t, hardTarget, "H-24")
		archive := filepath.Join(env.root, "special.tar.gz")
		qaSpecTarGz(t, archive, []qaSpecTarEntry{
			{hdr: tar.Header{Name: "lnk", Typeflag: tar.TypeSymlink, Linkname: env.outside, Mode: 0o777}},
			{hdr: tar.Header{Name: "hard", Typeflag: tar.TypeLink, Linkname: hardTarget, Mode: 0o644}},
			{hdr: tar.Header{Name: "dev", Typeflag: tar.TypeChar, Devmajor: 1, Devminor: 3, Mode: 0o666}},
			{hdr: tar.Header{Name: "plain.txt", Typeflag: tar.TypeReg, Mode: 0o644}, body: "PLAIN-24"},
		})

		_, _ = env.ops.Extract(archive, dest)
		qaSpecNoSpecialFiles(t, dest)
	})
}

// ---------------------------------------------------------------------------
// Requirement 25: extract never replaces an existing file. The name already in
// use receives a "(n)" sibling, and the original bytes stay intact.
// ---------------------------------------------------------------------------

func TestReq25_ExtractNeverReplacesFile(t *testing.T) {
	env := qaSpecNewEnv(t)
	dest := filepath.Join(env.root, "dest")
	qaSpecMkdir(t, dest)
	qaSpecWrite(t, filepath.Join(dest, "f.txt"), "A-25")
	archive := filepath.Join(env.root, "keep.zip")
	qaSpecZip(t, archive, []qaSpecEntry{{name: "f.txt", body: "B-25"}})

	if _, err := env.ops.Extract(archive, dest); err != nil {
		t.Fatalf("Extract = %v, want success", err)
	}
	if got := qaSpecRead(t, filepath.Join(dest, "f (1).txt")); got != "B-25" {
		t.Errorf("sibling \"f (1).txt\" bytes = %q, want %q", got, "B-25")
	}
	if got := qaSpecRead(t, filepath.Join(dest, "f.txt")); got != "A-25" {
		t.Errorf("existing f.txt bytes = %q, want %q", got, "A-25")
	}
}
