package files

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	kflate "github.com/klauspost/compress/flate"

	"github.com/ZaidQamhieh/rfe/agent/internal/next/api"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/authz"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/rootfs"
)

// zipDeflateLevel is klauspost's level 5, about 1.5x faster than the standard
// library's default at roughly 1% larger output.
const zipDeflateLevel = 5

// Archive formats Extract accepts, matched on the lower-cased file name.
const (
	extZip   = ".zip"
	extTarGz = ".tar.gz"
	extTgz   = ".tgz"
)

// Extraction bounds, against zip and tar bombs from a crafted archive: the most
// entries (files, folders and links) and the most expanded bytes one archive
// may produce. Bytes are counted on the real decompressed stream, not on the
// sizes an archive declares.
const (
	maxArchiveEntries          = 100_000
	maxArchiveTotalBytes int64 = 2 << 30
	// maxLinkTextBytes bounds the text of a link entry (a path-length limit).
	maxLinkTextBytes = 4096
	// maxConcurrentExtracts is how many archives expand at once in the process,
	// which makes archiveByteLimit a ceiling on the disk a burst can fill.
	maxConcurrentExtracts = 1
	// extractOwnerRW is added to the permission an entry records, so an
	// extracted file is always readable and writable by its owner.
	extractOwnerRW fs.FileMode = 0o600
)

// archiveEntryLimit and archiveByteLimit are the limits in force: the constants
// above, as variables only so a test can lower them instead of building a 2 GiB
// archive.
var (
	archiveEntryLimit = maxArchiveEntries
	archiveByteLimit  = maxArchiveTotalBytes
)

// ErrArchiveTooLarge is returned when an archive goes over the entry or the
// expanded-size limit (413 PAYLOAD_TOO_LARGE).
var ErrArchiveTooLarge = fmt.Errorf("%w: archive exceeds the extraction limits", api.ErrPayloadTooLarge)

// extractSlots is the process-wide extraction semaphore.
var extractSlots = make(chan struct{}, maxConcurrentExtracts)

// archiveSource is one path to compress with the name it gets in the archive.
type archiveSource struct {
	h    *rootfs.Handle
	info fs.FileInfo
	name string
}

// Compress writes a zip archive of the sources (files or folders, recursively)
// to dest and returns its entry. Every source is checked first (a folder that
// contains the protected data folder is refused, not archived without it). A
// taken dest name is never replaced: the archive becomes "name (N).zip". It is
// built under a hidden name and renamed into place, so a failure leaves no
// partial archive. Symlinks inside a folder are left out, never followed.
func (s *Service) Compress(sources []string, dest string) (*Entry, error) {
	if s.set.IsReadOnly() {
		return nil, authz.ErrReadOnly
	}
	if len(sources) == 0 {
		return nil, fmt.Errorf("%w: no sources", api.ErrBadRequest)
	}
	hd, err := s.open(dest)
	if err != nil {
		return nil, err
	}
	srcs := make([]archiveSource, 0, len(sources))
	seen := make(map[string]bool, len(sources))
	for _, p := range sources {
		h, err := s.open(p)
		if err != nil {
			return nil, err
		}
		name, err := entryName(h.Path())
		if err != nil {
			return nil, err
		}
		if seen[name] {
			return nil, fmt.Errorf("%w: two sources are both called %q", api.ErrBadRequest, name)
		}
		seen[name] = true
		info, err := entryInfo(h)
		if err != nil {
			return nil, err
		}
		if err := h.CheckTree(); err != nil {
			return nil, classify(err)
		}
		srcs = append(srcs, archiveSource{h: h, info: info, name: name})
	}
	final := hd
	if _, found, err := probe(hd); err != nil {
		return nil, err
	} else if found {
		if final, err = s.autoName(hd); err != nil {
			return nil, err
		}
	}
	if err := s.ensureParent(final); err != nil {
		return nil, err
	}
	dir := filepath.Dir(final.Path())
	tmp, err := s.stage(dir, newFilePerm, func(w io.Writer) error { return s.writeZip(w, srcs, dir) })
	if err != nil {
		return nil, err
	}
	if err := tmp.RenameNoReplace(final); err != nil {
		return nil, s.discard(tmp, err)
	}
	return s.Meta(final.Path())
}

// writeZip writes the zip of srcs to w. skipDir is the folder the archive is
// being built in: working files in it are not archived (the archive itself may
// sit inside a folder it compresses).
func (s *Service) writeZip(w io.Writer, srcs []archiveSource, skipDir string) error {
	zw := zip.NewWriter(w)
	zw.RegisterCompressor(zip.Deflate, func(out io.Writer) (io.WriteCloser, error) {
		return kflate.NewWriter(out, zipDeflateLevel)
	})
	for _, src := range srcs {
		if err := s.addToZip(zw, src.h, src.info, src.name, skipDir, true); err != nil {
			return err
		}
	}
	return zw.Close()
}

// addToZip adds the entry h under name: a folder with everything in it, a
// regular file with its content. A link or special file inside a folder is
// skipped; named directly as a source it is refused (top).
func (s *Service) addToZip(zw *zip.Writer, h *rootfs.Handle, info fs.FileInfo, name, skipDir string, top bool) error {
	switch {
	case info.IsDir():
		if _, err := zw.Create(name + "/"); err != nil {
			return err
		}
		des, err := h.ReadDir()
		if err != nil {
			return classify(err)
		}
		sort.Slice(des, func(i, j int) bool { return des[i].Name() < des[j].Name() })
		for _, de := range des {
			if h.Path() == skipDir && strings.HasPrefix(de.Name(), tempPrefix) {
				continue
			}
			ci, err := de.Info()
			if isMissing(err) {
				continue // removed since the folder was read
			}
			if err != nil {
				return classify(err)
			}
			ch, err := s.open(filepath.Join(h.Path(), de.Name()))
			if err != nil {
				return err
			}
			if err := s.addToZip(zw, ch, ci, name+"/"+de.Name(), skipDir, false); err != nil {
				return err
			}
		}
		return nil
	case info.Mode().IsRegular():
		hdr, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		hdr.Name = name
		hdr.Method = zip.Deflate
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			return err
		}
		f, err := h.Open()
		if err != nil {
			return classify(err)
		}
		defer f.Close()
		_, err = io.Copy(w, f)
		return err
	case top:
		return fmt.Errorf("%w: links and special files cannot be archived", api.ErrNotAFile)
	}
	return nil
}

// archiveKind says what an archive item is.
type archiveKind int

const (
	kindFile archiveKind = iota
	kindDir
	kindLink
	kindOther // hard link, device, ...: skipped
)

// archiveItem is one entry read from an archive, whatever its format.
type archiveItem struct {
	name     string
	kind     archiveKind
	perm     fs.FileMode
	target   string // link text, for kindLink
	declared uint64 // size the archive claims, 0 when it does not say
	open     func() (io.ReadCloser, error)
}

// Extract unpacks archive (.zip, .tar.gz or .tgz) into destDir, created if
// absent, and returns destDir's entry. Entries are unpacked into a hidden
// staging folder first, so an entry with an absolute name or one climbing out
// of destDir ("../x"), a path through an archive's own link, an archive over
// archiveEntryLimit entries or archiveByteLimit expanded bytes, or any
// write error fails the whole extraction (403 or 413) and removes everything it
// created; what was already in destDir is untouched. Links are recreated only
// when relative and staying inside the extracted tree, else skipped; hard links
// and devices are skipped. A file whose name is taken is kept and the new one
// becomes "name (N)", unless overwrite is set, which replaces files (never a
// folder with a file or the reverse).
func (s *Service) Extract(archive, destDir string, overwrite bool) (*Entry, error) {
	if s.set.IsReadOnly() {
		return nil, authz.ErrReadOnly
	}
	ha, err := s.open(archive)
	if err != nil {
		return nil, err
	}
	hdst, err := s.open(destDir)
	if err != nil {
		return nil, err
	}
	lower := strings.ToLower(filepath.Base(ha.Path()))
	var each func(*os.File, int64, func(archiveItem) error) error
	switch {
	case strings.HasSuffix(lower, extZip):
		each = eachZip
	case strings.HasSuffix(lower, extTarGz), strings.HasSuffix(lower, extTgz):
		each = eachTarGz
	default:
		return nil, fmt.Errorf("%w: %s", api.ErrUnsupportedFormat, filepath.Ext(lower))
	}
	f, err := ha.Open()
	if err != nil {
		return nil, classify(err)
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, classify(err)
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("%w: the archive is not a file", api.ErrNotAFile)
	}

	extractSlots <- struct{}{}
	defer func() { <-extractSlots }()

	made, err := s.ensureDirTracked(hdst)
	if err == nil {
		err = s.extractInto(hdst, f, fi.Size(), each, overwrite)
	}
	if err != nil {
		for i := len(made) - 1; i >= 0; i-- {
			_ = made[i].Remove() // only if empty: anything else stays
		}
		return nil, err
	}
	return s.Meta(hdst.Path())
}

// ensureDirTracked makes sure h is a folder, creating it and any missing
// parents, and returns the folders it created in creation order (also on
// failure), so a failed extraction can remove them again.
func (s *Service) ensureDirTracked(h *rootfs.Handle) ([]*rootfs.Handle, error) {
	var missing []*rootfs.Handle
	for cur := h; ; {
		info, err := cur.Stat()
		if err == nil {
			if !info.IsDir() {
				return nil, fmt.Errorf("%w: destination is not a folder", api.ErrBadRequest)
			}
			break
		}
		if !isMissing(err) {
			return nil, classify(err)
		}
		missing = append(missing, cur)
		parent := filepath.Dir(cur.Path())
		if parent == cur.Path() {
			break
		}
		if cur, err = s.open(parent); err != nil {
			return nil, err
		}
	}
	var made []*rootfs.Handle
	for i := len(missing) - 1; i >= 0; i-- {
		if err := missing[i].Mkdir(rootfs.DirMode); err != nil && !errors.Is(err, fs.ErrExist) {
			return made, classify(err)
		}
		made = append(made, missing[i])
	}
	return made, nil
}

// extraction is the state of one Extract call.
type extraction struct {
	s         *Service
	stage     *rootfs.Handle
	overwrite bool
	remaining int64
	entries   int
	links     map[string]bool // archive paths (clean, slash form) of links made so far
	placed    []*rootfs.Handle
}

// extractInto unpacks the archive into a staging folder inside dest, then moves
// the result into dest. On failure everything it created is removed.
func (s *Service) extractInto(dest *rootfs.Handle, f *os.File, size int64, each func(*os.File, int64, func(archiveItem) error) error, overwrite bool) error {
	stage, err := s.reserve(dest.Path(), tempPrefix, func(h *rootfs.Handle) error { return h.Mkdir(ownerRWX) })
	if err != nil {
		return err
	}
	x := &extraction{s: s, stage: stage, overwrite: overwrite, remaining: archiveByteLimit, links: map[string]bool{}}
	err = each(f, size, x.add)
	if err == nil {
		err = x.mergeDir(stage, dest)
	}
	if err != nil {
		for i := len(x.placed) - 1; i >= 0; i-- {
			_ = x.placed[i].RemoveAll()
		}
		return s.discardTree(stage, err)
	}
	_ = stage.RemoveAll() // a failure leaves only the hidden staging folder, now empty of content
	return nil
}

// entryRel validates an archive entry name and returns it as a clean relative
// path with "/" separators. Backslashes count as separators, as in zips made on
// Windows. An absolute name (leading separator, drive prefix) or one climbing
// out of the folder is refused (403); "." and "./" name the folder itself.
func entryRel(name string) (rel string, self bool, err error) {
	if strings.IndexByte(name, 0) >= 0 {
		return "", false, fmt.Errorf("%w: archive entry name is invalid", api.ErrBadRequest)
	}
	n := strings.ReplaceAll(name, `\`, "/")
	drive := len(n) >= 2 && n[1] == ':' && (('a' <= n[0] && n[0] <= 'z') || ('A' <= n[0] && n[0] <= 'Z'))
	if strings.HasPrefix(n, "/") || drive {
		return "", false, fmt.Errorf("%w: archive entry has an absolute path", api.ErrForbidden)
	}
	c := path.Clean(n)
	switch {
	case c == ".":
		return "", true, nil
	case c == ".." || strings.HasPrefix(c, "../"):
		return "", false, fmt.Errorf("%w: archive entry escapes the destination", api.ErrForbidden)
	}
	return c, false, nil
}

// linkStaysInside reports whether a link at rel with the given text is relative
// and points at a place inside the extracted tree.
func linkStaysInside(rel, target string) bool {
	t := strings.ReplaceAll(target, `\`, "/")
	if t == "" || strings.IndexByte(t, 0) >= 0 || strings.HasPrefix(t, "/") ||
		(len(t) >= 2 && t[1] == ':') || len(t) > maxLinkTextBytes {
		return false
	}
	c := path.Join(path.Dir(rel), t)
	return c != ".." && !strings.HasPrefix(c, "../")
}

// crossesLink reports whether a link made earlier is a folder on the way to rel,
// so writing rel would go through that link.
func (x *extraction) crossesLink(rel string) bool {
	for i := 0; i < len(rel); i++ {
		if rel[i] == '/' && x.links[rel[:i]] {
			return true
		}
	}
	return false
}

// add stores one archive item in the staging folder.
func (x *extraction) add(it archiveItem) error {
	x.entries++
	if x.entries > archiveEntryLimit {
		return fmt.Errorf("%w: more than %d entries", ErrArchiveTooLarge, archiveEntryLimit)
	}
	rel, self, err := entryRel(it.name)
	if err != nil || self {
		return err
	}
	if x.crossesLink(rel) {
		return fmt.Errorf("%w: archive entry goes through a link", api.ErrForbidden)
	}
	h, err := x.s.open(filepath.Join(x.stage.Path(), filepath.FromSlash(rel)))
	if err != nil {
		return err
	}
	switch it.kind {
	case kindDir:
		return classify(h.MkdirAll(rootfs.DirMode))
	case kindFile:
		return x.writeFile(h, it)
	case kindLink:
		if !linkStaysInside(rel, it.target) {
			return nil
		}
		if err := x.prepare(h); err != nil {
			return err
		}
		if err := h.Symlink(filepath.FromSlash(strings.ReplaceAll(it.target, `\`, "/"))); err != nil {
			return classify(err)
		}
		x.links[rel] = true
	}
	return nil
}

// prepare makes the parent folders of h in staging and clears a name an earlier
// entry of the same archive used (the later entry wins, as in tar). A folder
// there is a conflict.
func (x *extraction) prepare(h *rootfs.Handle) error {
	parent, err := x.s.open(filepath.Dir(h.Path()))
	if err != nil {
		return err
	}
	if err := parent.MkdirAll(rootfs.DirMode); err != nil {
		return classify(err)
	}
	info, found, err := probe(h)
	if err != nil {
		return err
	}
	if found {
		if info.IsDir() {
			return fmt.Errorf("%w: archive file has the name of a folder", api.ErrConflict)
		}
		return classify(h.Remove())
	}
	return nil
}

// writeFile writes one regular file into staging, counting the real bytes
// against the expanded-size budget.
func (x *extraction) writeFile(h *rootfs.Handle, it archiveItem) error {
	if it.declared > uint64(x.remaining) {
		return fmt.Errorf("%w: expanded size over %d bytes", ErrArchiveTooLarge, archiveByteLimit)
	}
	if err := x.prepare(h); err != nil {
		return err
	}
	perm := it.perm.Perm()
	if perm == 0 {
		perm = rootfs.FileMode // the archive records no permission
	}
	perm |= extractOwnerRW
	out, err := h.OpenFile(os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return classify(err)
	}
	rc, err := it.open()
	if err != nil {
		_ = out.Close()
		return err
	}
	n, err := io.Copy(out, io.LimitReader(rc, x.remaining+1))
	_ = rc.Close()
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return classify(err)
	}
	if n > x.remaining {
		return fmt.Errorf("%w: expanded size over %d bytes", ErrArchiveTooLarge, archiveByteLimit)
	}
	x.remaining -= n
	return nil
}

// mergeDir moves every entry of the staging folder from into the existing
// folder to.
func (x *extraction) mergeDir(from, to *rootfs.Handle) error {
	des, err := from.ReadDir()
	if err != nil {
		return classify(err)
	}
	for _, de := range des {
		src, err := x.s.open(filepath.Join(from.Path(), de.Name()))
		if err != nil {
			return err
		}
		dst, err := x.s.open(filepath.Join(to.Path(), de.Name()))
		if err != nil {
			return err
		}
		info, err := entryInfo(src)
		if err != nil {
			return err
		}
		if err := x.place(src, dst, info.IsDir()); err != nil {
			return err
		}
	}
	return nil
}

// place moves src to dst without replacing anything it should keep. A folder
// meeting a folder is merged. Otherwise a taken name is replaced with overwrite
// (file for file only) or the newcomer is renamed to a free "name (N)".
func (x *extraction) place(src, dst *rootfs.Handle, isDir bool) error {
	for range tempTries {
		err := src.RenameNoReplace(dst)
		if err == nil {
			x.placed = append(x.placed, dst)
			return nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return classify(err)
		}
		cur, found, err := probe(dst)
		if err != nil {
			return err
		}
		if !found {
			continue // the name vanished again: try once more
		}
		switch {
		case isDir && cur.IsDir():
			return x.mergeDir(src, dst)
		case x.overwrite && !isDir && !cur.IsDir():
			return classify(src.Rename(dst))
		case x.overwrite:
			return fmt.Errorf("%w: a folder and a file have the same name", api.ErrConflict)
		}
		if dst, err = x.s.autoName(dst); err != nil {
			return err
		}
	}
	return fmt.Errorf("%w: destination keeps changing", api.ErrConflict)
}

// eachZip calls fn for every entry of the zip file f, after refusing an archive
// that declares more entries than the limit.
func eachZip(f *os.File, size int64, fn func(archiveItem) error) error {
	zr, err := zip.NewReader(f, size)
	if err != nil {
		return fmt.Errorf("%w: not a valid zip archive", api.ErrBadRequest)
	}
	if len(zr.File) > archiveEntryLimit {
		return fmt.Errorf("%w: more than %d entries", ErrArchiveTooLarge, archiveEntryLimit)
	}
	for _, zf := range zr.File {
		it := archiveItem{name: zf.Name, perm: zf.Mode().Perm(), declared: zf.UncompressedSize64, open: zf.Open}
		switch {
		case zf.FileInfo().IsDir():
			it.kind = kindDir
		case zf.Mode()&fs.ModeSymlink != 0:
			it.kind = kindLink
			rc, err := zf.Open()
			if err != nil {
				return err
			}
			text, err := io.ReadAll(io.LimitReader(rc, maxLinkTextBytes+1))
			_ = rc.Close()
			if err != nil {
				return err
			}
			it.target = string(text)
		case zf.Mode().IsRegular():
			it.kind = kindFile
		default:
			it.kind = kindOther
		}
		if err := fn(it); err != nil {
			return err
		}
	}
	return nil
}

// eachTarGz calls fn for every entry of the gzip-compressed tar file f.
func eachTarGz(f *os.File, _ int64, fn func(archiveItem) error) error {
	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("%w: not a valid gzip archive", api.ErrBadRequest)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("%w: not a valid tar archive", api.ErrBadRequest)
		}
		it := archiveItem{name: hdr.Name, perm: fs.FileMode(hdr.Mode).Perm(), target: hdr.Linkname,
			open: func() (io.ReadCloser, error) { return io.NopCloser(tr), nil }}
		switch hdr.Typeflag {
		case tar.TypeDir:
			it.kind = kindDir
		case tar.TypeReg:
			it.kind = kindFile
		case tar.TypeSymlink:
			it.kind = kindLink
		default:
			it.kind = kindOther
		}
		if err := fn(it); err != nil {
			return err
		}
	}
}
