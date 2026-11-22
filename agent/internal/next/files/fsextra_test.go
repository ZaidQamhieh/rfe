package files

import (
	"bytes"
	"encoding/json"
	"errors"
	"image"
	"image/jpeg"
	"image/png"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/ZaidQamhieh/rfe/agent/internal/next/api"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/authz"
)

// Sizes and modes the chmod and media-info tests use.
const (
	fxPNGWidth, fxPNGHeight   = 7, 5
	fxJPEGWidth, fxJPEGHeight = 9, 4
	fxStartMode               = fs.FileMode(0o644)
	fxNewMode                 = fs.FileMode(0o600)
	fxSetuidBit               = fs.FileMode(0o4000)
)

// fxSkipOnWindows skips a test that relies on POSIX permission bits.
func fxSkipOnWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits")
	}
}

// fxPerm returns the permission bits of the file at p, followed through links.
func fxPerm(t *testing.T, p string) fs.FileMode {
	t.Helper()
	info, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode().Perm()
}

// fxSymlink makes link point at target, skipping the test where links are not allowed.
func fxSymlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
}

// fxEncode writes an image of the given size in the format enc produces.
func fxEncode(t *testing.T, p string, w, h int, enc func(*bytes.Buffer, image.Image) error) {
	t.Helper()
	var buf bytes.Buffer
	if err := enc(&buf, image.NewRGBA(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	write(t, p, buf.String())
}

func TestChmodChangesFileAndFolderAndDescribesThem(t *testing.T) {
	fxSkipOnWindows(t)
	e := newEnv(t, false)
	file := write(t, filepath.Join(e.root, "a.txt"), "x")
	dir := filepath.Join(e.root, "d")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(file, fxStartMode); err != nil {
		t.Fatal(err)
	}

	got, err := e.svc.Chmod(file, fxNewMode)
	if err != nil {
		t.Fatal(err)
	}
	if fxPerm(t, file) != fxNewMode || got.Path != file || got.Mode != fxNewMode.String() {
		t.Fatalf("file: perm %v entry %+v", fxPerm(t, file), got)
	}

	const folderMode = fs.FileMode(0o750)
	got, err = e.svc.Chmod(dir, folderMode)
	if err != nil {
		t.Fatal(err)
	}
	if fxPerm(t, dir) != folderMode || !got.IsDir {
		t.Fatalf("folder: perm %v entry %+v", fxPerm(t, dir), got)
	}
}

func TestChmodFollowsALinkInsideTheRootsOnly(t *testing.T) {
	fxSkipOnWindows(t)
	e := newEnv(t, false)
	target := write(t, filepath.Join(e.root, "real.txt"), "x")
	if err := os.Chmod(target, fxStartMode); err != nil {
		t.Fatal(err)
	}
	// os.Root refuses an absolute link, so the link inside the root is relative.
	fxSymlink(t, filepath.Base(target), filepath.Join(e.root, "link"))
	if _, err := e.svc.Chmod(filepath.Join(e.root, "link"), fxNewMode); err != nil {
		t.Fatal(err)
	}
	if fxPerm(t, target) != fxNewMode {
		t.Fatalf("target perm = %v", fxPerm(t, target))
	}
}

func TestChmodRefusesEverythingOutsideTheRoots(t *testing.T) {
	fxSkipOnWindows(t)
	e := newEnv(t, true)
	outside := write(t, filepath.Join(t.TempDir(), "o.txt"), "x")
	if err := os.Chmod(outside, fxStartMode); err != nil {
		t.Fatal(err)
	}
	fxSymlink(t, outside, filepath.Join(e.root, "escape"))
	secret := filepath.Join(e.root, dataDirName, "secret.key")
	if err := os.Chmod(secret, fxStartMode); err != nil {
		t.Fatal(err)
	}

	for name, path := range map[string]string{
		"outside path":     outside,
		"symlink escape":   filepath.Join(e.root, "escape"),
		"dotdot escape":    filepath.Join(e.root, "..", filepath.Base(filepath.Dir(outside)), "o.txt"),
		"protected folder": secret,
	} {
		if _, err := e.svc.Chmod(path, fxNewMode); !errors.Is(err, api.ErrForbidden) {
			t.Errorf("%s: err = %v, want api.ErrForbidden", name, err)
		}
	}
	if fxPerm(t, outside) != fxStartMode || fxPerm(t, secret) != fxStartMode {
		t.Fatal("a file outside the allowed folders was changed")
	}
}

func TestChmodReadOnlyChangesNothing(t *testing.T) {
	fxSkipOnWindows(t)
	e := newEnv(t, false)
	file := write(t, filepath.Join(e.root, "a.txt"), "x")
	if err := os.Chmod(file, fxStartMode); err != nil {
		t.Fatal(err)
	}
	if _, err := e.readOnly().Chmod(file, fxNewMode); !errors.Is(err, authz.ErrReadOnly) {
		t.Fatalf("err = %v, want authz.ErrReadOnly", err)
	}
	if fxPerm(t, file) != fxStartMode {
		t.Fatalf("perm = %v", fxPerm(t, file))
	}
}

func TestChmodErrors(t *testing.T) {
	e := newEnv(t, false)
	file := write(t, filepath.Join(e.root, "a.txt"), "x")
	cases := []struct {
		name string
		path string
		mode fs.FileMode
		want error
	}{
		{"missing path", filepath.Join(e.root, "nope"), fxNewMode, api.ErrPathNotFound},
		{"parent is a file", filepath.Join(file, "x"), fxNewMode, api.ErrPathNotFound},
		{"relative path", "a.txt", fxNewMode, api.ErrBadRequest},
		{"empty path", "", fxNewMode, api.ErrBadRequest},
		{"setuid bit", file, fxSetuidBit | fxNewMode, api.ErrBadRequest},
		{"type bit", file, fs.ModeDir | fxNewMode, api.ErrBadRequest},
	}
	for _, c := range cases {
		if _, err := e.svc.Chmod(c.path, c.mode); !errors.Is(err, c.want) {
			t.Errorf("%s: err = %v, want %v", c.name, err, c.want)
		}
	}
	if fxPerm(t, file) == fxNewMode|fxSetuidBit {
		t.Fatal("setuid reached the file")
	}
}

func TestMediaInfoReadsImageHeaders(t *testing.T) {
	e := newEnv(t, false)
	pngPath := filepath.Join(e.root, "a.png")
	jpgPath := filepath.Join(e.root, "b.jpg")
	fxEncode(t, pngPath, fxPNGWidth, fxPNGHeight, func(b *bytes.Buffer, im image.Image) error { return png.Encode(b, im) })
	fxEncode(t, jpgPath, fxJPEGWidth, fxJPEGHeight, func(b *bytes.Buffer, im image.Image) error { return jpeg.Encode(b, im, nil) })

	for path, want := range map[string]MediaInfo{
		pngPath: {Width: fxPNGWidth, Height: fxPNGHeight},
		jpgPath: {Width: fxJPEGWidth, Height: fxJPEGHeight},
	} {
		got, err := e.svc.MediaInfo(path)
		if err != nil || *got != want {
			t.Errorf("%s = %+v, %v; want %+v", filepath.Base(path), got, err, want)
		}
	}
}

func TestMediaInfoOfAFolderOrUnsupportedFileIsEmpty(t *testing.T) {
	e := newEnv(t, false)
	text := write(t, filepath.Join(e.root, "a.txt"), "just text")
	for _, p := range []string{text, e.root} {
		got, err := e.svc.MediaInfo(p)
		if err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		raw, _ := json.Marshal(got)
		if string(raw) != "{}" {
			t.Fatalf("%s = %s, want {}", p, raw)
		}
	}
}

func TestMediaInfoRefusesWhatLiesOutsideTheRoots(t *testing.T) {
	e := newEnv(t, true)
	outside := filepath.Join(t.TempDir(), "o.png")
	fxEncode(t, outside, fxPNGWidth, fxPNGHeight, func(b *bytes.Buffer, im image.Image) error { return png.Encode(b, im) })
	fxSymlink(t, outside, filepath.Join(e.root, "escape.png"))
	secret := filepath.Join(e.root, dataDirName, "secret.key")

	for name, path := range map[string]string{
		"outside path":   outside,
		"symlink escape": filepath.Join(e.root, "escape.png"),
		"protected":      secret,
	} {
		got, err := e.svc.MediaInfo(path)
		if !errors.Is(err, api.ErrForbidden) || got != nil {
			t.Errorf("%s = %+v, %v; want api.ErrForbidden", name, got, err)
		}
	}
}

func TestMediaInfoErrors(t *testing.T) {
	e := newEnv(t, false)
	for name, c := range map[string]struct {
		path string
		want error
	}{
		"missing":  {filepath.Join(e.root, "nope.png"), api.ErrPathNotFound},
		"relative": {"a.png", api.ErrBadRequest},
		"empty":    {"", api.ErrBadRequest},
	} {
		if _, err := e.svc.MediaInfo(c.path); !errors.Is(err, c.want) {
			t.Errorf("%s: err = %v, want %v", name, err, c.want)
		}
	}
}

func TestUnreadableFileIsForbiddenNotInternal(t *testing.T) {
	fxSkipOnWindows(t)
	if os.Geteuid() == 0 {
		t.Skip("root can open any file")
	}
	e := newEnv(t, false)
	file := write(t, filepath.Join(e.root, "locked.png"), "x")
	if err := os.Chmod(file, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(file, fxStartMode) })
	if _, err := e.svc.MediaInfo(file); !errors.Is(err, api.ErrForbidden) {
		t.Errorf("MediaInfo: err = %v, want api.ErrForbidden", err)
	}
	if _, err := e.svc.Chmod(file, fxNewMode); !errors.Is(err, api.ErrForbidden) {
		t.Errorf("Chmod: err = %v, want api.ErrForbidden", err)
	}
}
