package files

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

	"github.com/ZaidQamhieh/rfe/agent/internal/next/api"
)

// Archive listing bounds. A listing returns DefaultArchiveListLimit entries
// unless the caller asks for another count, and never more than
// MaxArchiveListLimit (openapi /fs/archive). Listing a .tar.gz inflates the
// stream up to the entry wanted, so the inflated bytes read are capped at
// maxListInflateBytes: a small file that expands to gigabytes must not cost a
// request that much CPU.
const (
	DefaultArchiveListLimit       = 500
	MaxArchiveListLimit           = 5000
	maxListInflateBytes     int64 = 256 << 20
	extTar                        = ".tar"
)

// listInflateBudget is the inflate cap in force: the constant above, a variable
// only so a test can lower it instead of building a 256 MiB archive.
var listInflateBudget = maxListInflateBytes

// ErrArchiveListTooLarge is returned when listing an archive would inflate more
// than the cap (413 PAYLOAD_TOO_LARGE).
var ErrArchiveListTooLarge = fmt.Errorf("%w: archive is too large to list", api.ErrPayloadTooLarge)

// OpenContent opens the regular file at path for reading, following links only
// inside the allowed folders. The caller must call Close on the result. A folder
// or a special file is refused with api.ErrNotAFile, a missing path with
// api.ErrPathNotFound, a path outside the folders with api.ErrForbidden.
func (s *Service) OpenContent(path string) (*api.ContentFile, error) {
	h, err := s.open(path)
	if err != nil {
		return nil, err
	}
	f, err := h.Open()
	if err != nil {
		return nil, classify(err)
	}
	info, err := f.Stat()
	if err != nil {
		return nil, closeWith(f, classify(err))
	}
	if !info.Mode().IsRegular() {
		return nil, closeWith(f, fmt.Errorf("%w: path is not a file", api.ErrNotAFile))
	}
	return &api.ContentFile{
		Name:    filepath.Base(h.Path()),
		ModTime: info.ModTime(),
		Size:    info.Size(),
		Content: f,
		Close:   f.Close,
	}, nil
}

// ArchiveEntries lists the first limit entries of the .zip, .tar.gz, .tgz or
// .tar file at path without extracting anything. A limit of 0 or less means
// DefaultArchiveListLimit, and one above MaxArchiveListLimit is lowered to it.
// Another file type or a folder is api.ErrUnsupportedFormat, a damaged archive
// api.ErrBadRequest, and a .tar.gz that inflates past the cap
// ErrArchiveListTooLarge.
func (s *Service) ArchiveEntries(path string, limit int) ([]api.ArchiveEntry, error) {
	switch {
	case limit <= 0:
		limit = DefaultArchiveListLimit
	case limit > MaxArchiveListLimit:
		limit = MaxArchiveListLimit
	}
	h, err := s.open(path)
	if err != nil {
		return nil, err
	}
	name := strings.ToLower(h.Path())
	var list func(*os.File, int64, int) ([]api.ArchiveEntry, error)
	switch {
	case strings.HasSuffix(name, extZip):
		list = listZip
	case strings.HasSuffix(name, extTarGz), strings.HasSuffix(name, extTgz):
		list = listTarGz
	case strings.HasSuffix(name, extTar):
		list = listTar
	default:
		return nil, fmt.Errorf("%w: not a zip, tar or tar.gz archive", api.ErrUnsupportedFormat)
	}
	f, err := h.Open()
	if err != nil {
		return nil, classify(err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, classify(err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: path is not a file", api.ErrUnsupportedFormat)
	}
	return list(f, info.Size(), limit)
}

// closeWith closes f and returns err, keeping a close failure behind it.
func closeWith(f *os.File, err error) error {
	if cerr := f.Close(); cerr != nil {
		return withCause(err, cerr)
	}
	return err
}

// listZip reads the central directory of the zip file f and returns its first
// limit entries.
func listZip(f *os.File, size int64, limit int) ([]api.ArchiveEntry, error) {
	zr, err := zip.NewReader(f, size)
	if err != nil {
		return nil, fmt.Errorf("%w: not a valid zip archive", api.ErrBadRequest)
	}
	out := make([]api.ArchiveEntry, 0, min(len(zr.File), limit))
	for _, zf := range zr.File {
		if len(out) >= limit {
			break
		}
		out = append(out, api.ArchiveEntry{
			Path: zf.Name, Size: int64(zf.UncompressedSize64), Modified: zf.Modified, IsDir: zf.FileInfo().IsDir(),
		})
	}
	return out, nil
}

// listTarGz lists a gzip-compressed tar file, reading at most listInflateBudget
// inflated bytes.
func listTarGz(f *os.File, _ int64, limit int) ([]api.ArchiveEntry, error) {
	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("%w: not a valid gzip archive", api.ErrBadRequest)
	}
	defer gz.Close()
	return readTarEntries(tar.NewReader(&budgetReader{r: gz, left: listInflateBudget}), limit)
}

// listTar lists a plain tar file.
func listTar(f *os.File, _ int64, limit int) ([]api.ArchiveEntry, error) {
	return readTarEntries(tar.NewReader(f), limit)
}

// readTarEntries returns the first limit headers of tr.
func readTarEntries(tr *tar.Reader, limit int) ([]api.ArchiveEntry, error) {
	var out []api.ArchiveEntry
	for len(out) < limit {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if errors.Is(err, ErrArchiveListTooLarge) {
			return nil, ErrArchiveListTooLarge
		}
		if err != nil {
			return nil, fmt.Errorf("%w: not a valid tar archive", api.ErrBadRequest)
		}
		out = append(out, api.ArchiveEntry{
			Path: hdr.Name, Size: hdr.Size, Modified: hdr.ModTime, IsDir: hdr.Typeflag == tar.TypeDir,
		})
	}
	return out, nil
}

// budgetReader fails with ErrArchiveListTooLarge once more than left bytes have
// been read through it.
type budgetReader struct {
	r    io.Reader
	left int64
}

func (b *budgetReader) Read(p []byte) (int, error) {
	if b.left <= 0 {
		return 0, ErrArchiveListTooLarge
	}
	if int64(len(p)) > b.left {
		p = p[:b.left]
	}
	n, err := b.r.Read(p)
	b.left -= int64(n)
	return n, err
}
