package hostupdate

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
)

// Bundle files that must be executable on unix even when the archive lost the mode bits (a zip made on Windows).
var execNames = map[string]bool{
	"rfe":          true,
	"rfe-launcher": true,
	"rfe-indexd":   true,
	"rfe-thumbd":   true,
}

// extractor writes the entries of one archive under dest, enforcing the limits and refusing anything but plain
// files and directories. It tracks the running total so a decompression bomb stops at MaxTotalBytes.
type extractor struct {
	dest    string
	lim     Limits
	total   int64
	entries int
	files   map[string]bool
}

// withDefaults fills the zero fields of l.
func (l Limits) withDefaults() Limits {
	if l.MaxFileBytes <= 0 {
		l.MaxFileBytes = DefaultMaxFileBytes
	}
	if l.MaxTotalBytes <= 0 {
		l.MaxTotalBytes = DefaultMaxTotalBytes
	}
	if l.MaxEntries <= 0 {
		l.MaxEntries = DefaultMaxEntries
	}
	return l
}

// extractArchive unpacks the tar.gz or zip at archive (chosen by its first bytes, not its name) into the new
// directory dest and returns the slash-separated paths of the regular files it wrote. dest must not exist yet.
func extractArchive(archive, dest string, lim Limits) ([]string, error) {
	f, err := os.Open(archive)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	magic := make([]byte, 4)
	n, _ := io.ReadFull(f, magic)
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	if err := os.Mkdir(dest, 0o755); err != nil {
		return nil, err
	}
	x := &extractor{dest: dest, lim: lim.withDefaults(), files: map[string]bool{}}
	switch {
	case n >= 2 && magic[0] == 0x1f && magic[1] == 0x8b:
		err = x.tarGz(f)
	case n == 4 && bytes.Equal(magic, []byte("PK\x03\x04")):
		var st os.FileInfo
		if st, err = f.Stat(); err == nil {
			err = x.zip(f, st.Size())
		}
	default:
		err = fmt.Errorf("%w: neither tar.gz nor zip", ErrUnsafeArchive)
	}
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(x.files))
	for p := range x.files {
		names = append(names, p)
	}
	return names, nil
}

// cleanName validates an archive entry name and returns it as a clean relative slash path ("" for the root
// directory entry). Absolute paths, drive letters, backslashes, NUL, "." or ".." components and
// non-canonical forms are refused.
func cleanName(name string) (string, error) {
	unsafe := func() (string, error) { return "", fmt.Errorf("%w: entry name %q", ErrUnsafeArchive, name) }
	if strings.ContainsAny(name, "\\:\x00") || strings.HasPrefix(name, "/") {
		return unsafe()
	}
	n := strings.TrimSuffix(strings.TrimPrefix(name, "./"), "/")
	if n == "" || n == "." {
		return "", nil
	}
	if path.Clean(n) != n || n == ".." || strings.HasPrefix(n, "../") {
		return unsafe()
	}
	return n, nil
}

// target maps a clean relative name to a path under dest, double-checking it stays inside.
func (x *extractor) target(rel string) (string, error) {
	p := filepath.Join(x.dest, filepath.FromSlash(rel))
	r, err := filepath.Rel(x.dest, p)
	if err != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) || filepath.IsAbs(r) {
		return "", fmt.Errorf("%w: entry %q leaves the bundle directory", ErrUnsafeArchive, rel)
	}
	return p, nil
}

// count registers one more entry against MaxEntries.
func (x *extractor) count() error {
	x.entries++
	if x.entries > x.lim.MaxEntries {
		return fmt.Errorf("%w: more than %d entries", ErrArchiveTooLarge, x.lim.MaxEntries)
	}
	return nil
}

// dir creates a directory entry.
func (x *extractor) dir(name string) error {
	rel, err := cleanName(name)
	if err != nil {
		return err
	}
	if err := x.count(); err != nil {
		return err
	}
	if rel == "" {
		return nil
	}
	p, err := x.target(rel)
	if err != nil {
		return err
	}
	return os.MkdirAll(p, 0o755)
}

// file writes one regular file from r. declared is the size the archive announces; modeBits are its unix mode.
// The file is created exclusively, so a repeated name or a name that already exists as a directory fails.
func (x *extractor) file(name string, r io.Reader, declared int64, modeBits uint32) error {
	rel, err := cleanName(name)
	if err != nil {
		return err
	}
	if rel == "" {
		return fmt.Errorf("%w: file entry without a name", ErrUnsafeArchive)
	}
	if err := x.count(); err != nil {
		return err
	}
	if declared > x.lim.MaxFileBytes {
		return fmt.Errorf("%w: %s is %d bytes (limit %d)", ErrArchiveTooLarge, rel, declared, x.lim.MaxFileBytes)
	}
	p, err := x.target(rel)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	perm := os.FileMode(0o644)
	if modeBits&0o111 != 0 {
		perm = 0o755
	}
	out, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return err
	}
	allowed := x.lim.MaxFileBytes
	if left := x.lim.MaxTotalBytes - x.total; left < allowed {
		allowed = left
	}
	n, err := io.Copy(out, io.LimitReader(r, allowed+1))
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	if n > allowed {
		return fmt.Errorf("%w: %s exceeds the per-file or total limit", ErrArchiveTooLarge, rel)
	}
	x.total += n
	x.files[rel] = true
	return nil
}

// tarGz extracts a gzip-compressed tar stream. Only regular files and directories are accepted; symlinks,
// hardlinks, devices, fifos and sparse files are refused.
func (x *extractor) tarGz(r io.Reader) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnsafeArchive, err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("%w: %v", ErrUnsafeArchive, err)
		}
		switch h.Typeflag {
		case tar.TypeReg:
			err = x.file(h.Name, tr, h.Size, uint32(h.Mode))
		case tar.TypeDir:
			err = x.dir(h.Name)
		case tar.TypeXGlobalHeader:
			continue
		default:
			err = fmt.Errorf("%w: %q has type %q (only plain files and directories are allowed)", ErrUnsafeArchive, h.Name, string(h.Typeflag))
		}
		if err != nil {
			return err
		}
	}
}

// zip extracts a zip archive of size bytes read from r, with the same rules as tarGz.
func (x *extractor) zip(r io.ReaderAt, size int64) error {
	zr, err := zip.NewReader(r, size)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnsafeArchive, err)
	}
	for _, f := range zr.File {
		mode := f.Mode()
		switch {
		case mode.IsDir():
			err = x.dir(f.Name)
		case mode.IsRegular():
			err = x.zipFile(f)
		default:
			err = fmt.Errorf("%w: %q has mode %v (only plain files and directories are allowed)", ErrUnsafeArchive, f.Name, mode)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// zipFile extracts one regular zip entry.
func (x *extractor) zipFile(f *zip.File) error {
	rc, err := f.Open()
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUnsafeArchive, err)
	}
	defer rc.Close()
	return x.file(f.Name, rc, int64(f.UncompressedSize64), uint32(f.Mode().Perm()))
}

// setExecBits makes the agent and its helpers executable on unix (a no-op on Windows). A file is made executable
// when the bundle manifest recorded an exec bit for it or when its name is one of the known binaries.
func setExecBits(dir string, files map[string]uint32) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	for rel, mode := range files {
		name := strings.TrimSuffix(path.Base(rel), ".exe")
		if mode&0o111 == 0 && !(execNames[name] && path.Dir(rel) == ".") {
			continue
		}
		if err := os.Chmod(filepath.Join(dir, filepath.FromSlash(rel)), 0o755); err != nil {
			return err
		}
	}
	return nil
}
