// Package fsops — archive (compress/extract) operations.
//
// Compress builds a zip from a set of jailed sources; Extract unpacks a
// zip/tar.gz into a jailed destination. Both go through Resolve so the path
// jail and read-only flag apply, and Extract additionally guards every
// archive entry against zip-slip (a "../" entry name escaping destDir).
package fsops

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"errors"
	"fmt"
	kflate "github.com/klauspost/compress/flate"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Compress creates a zip archive at destPath containing each of sources
// (files or directories, recursively). All sources and destPath are
// jail-checked via Resolve. If destPath already exists it is auto-renamed
// ("keep both"), so the call never clobbers an existing file. The archive is
// written to a temp file in the destination directory and renamed into place
// on success, so a failure can't leave a partial .zip behind. Returns the
// created archive's Entry.
func (o *Ops) Compress(sources []string, destPath string) (*Entry, error) {
	if o.settings.IsReadOnly() {
		return nil, ErrReadOnly
	}
	if len(sources) == 0 {
		return nil, fmt.Errorf("%w: no sources", ErrNotFound)
	}

	// The destination is a name to create: an existing entry there, even a
	// dangling symlink, is kept and the archive gets a "(n)" name instead.
	resDest, err := o.accessEntry(destPath)
	if err != nil {
		return nil, err
	}
	defer resDest.close()
	// Resolve (and jail-check) every source up front so a bad path fails the
	// whole operation before any bytes are written.
	resolved := make([]*securePath, 0, len(sources))
	for _, s := range sources {
		rs, err := o.access(s)
		if err != nil {
			for _, opened := range resolved {
				opened.close()
			}
			return nil, err
		}
		resolved = append(resolved, rs)
	}
	defer func() {
		for _, opened := range resolved {
			opened.close()
		}
	}()

	if _, err := resDest.lstat(); err == nil {
		renamed, rerr := autoRenameSecure(resDest)
		if rerr != nil {
			return nil, rerr
		}
		resDest.full = renamed.full
		resDest.name = renamed.name
	}
	if err := resDest.parent().mkdirAll(0o755); err != nil {
		return nil, err
	}

	tmpPath, tmp, err := resDest.createTemp("." + filepath.Base(resDest.full) + ".rfe-tmp-*")
	if err != nil {
		return nil, err
	}
	cleanup := func() { _ = tmpPath.remove() }

	zw := zip.NewWriter(tmp)
	// klauspost's deflate writes the same format about 1.5x faster than the standard library at a ~1% larger size.
	zw.RegisterCompressor(zip.Deflate, func(w io.Writer) (io.WriteCloser, error) { return kflate.NewWriter(w, zipDeflateLevel) })
	for _, src := range resolved {
		if err := addToZipSecure(zw, src, tmpPath.full); err != nil {
			zw.Close()
			tmp.Close()
			cleanup()
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		tmp.Close()
		cleanup()
		return nil, err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		cleanup()
		return nil, err
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return nil, err
	}
	// No-replace: an archive created at the destination since the name was
	// chosen is kept, and this one is dropped.
	if err := tmpPath.renameNoReplaceTo(resDest); err != nil {
		cleanup()
		if errors.Is(err, fs.ErrExist) {
			return nil, ErrConflict
		}
		return nil, err
	}
	return o.Meta(resDest.full)
}

// zipDeflateLevel is klauspost's level 5: 51 MB/s against 33 MB/s for the standard library's default on a
// 221 MB mixed corpus, within about 1% of its output size (see TestZipCompressorSpeed).
const zipDeflateLevel = 5

// addToZipSecure adds src to the archive. skip is the archive's own temp file:
// when the destination sits inside a folder being compressed, the walk would
// otherwise read the half-written archive into itself.
func addToZipSecure(zw *zip.Writer, src *securePath, skip string) error {
	base := filepath.Base(src.full)
	return addToZipSecureEntry(zw, src, base, skip)
}

func addToZipSecureEntry(zw *zip.Writer, src *securePath, name, skip string) error {
	if skip != "" && src.full == skip {
		return nil
	}
	info, err := src.lstat()
	if err != nil {
		return err
	}
	if info.IsDir() {
		if name != "" {
			if _, err := zw.Create(filepath.ToSlash(name) + "/"); err != nil {
				return err
			}
		}
		dir, err := src.open()
		if err != nil {
			return err
		}
		entries, readErr := dir.Readdir(-1)
		closeErr := dir.Close()
		if readErr != nil {
			return readErr
		}
		if closeErr != nil {
			return closeErr
		}
		for _, entry := range entries {
			childName := filepath.Join(name, entry.Name())
			if err := addToZipSecureEntry(zw, src.child(entry.Name()), childName, skip); err != nil {
				return err
			}
		}
		return nil
	}
	if !info.Mode().IsRegular() {
		return nil
	}
	hdr, err := zip.FileInfoHeader(info)
	if err != nil {
		return err
	}
	hdr.Name = filepath.ToSlash(name)
	hdr.Method = zip.Deflate
	w, err := zw.CreateHeader(hdr)
	if err != nil {
		return err
	}
	f, err := src.open()
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(w, f)
	return err
}

// Extract unpacks archivePath (zip, tar.gz or tgz) into destDir, which is
// created if absent. Both paths are jail-checked. Each archive entry's target
// is validated to stay within destDir (zip-slip guard), an absolute entry name
// is refused (checkEntryNameNotAbsolute); non-regular entries
// (symlinks, devices) are skipped. Returns destDir's Entry.
func (o *Ops) Extract(archivePath, destDir string) (*Entry, error) {
	if o.settings.IsReadOnly() {
		return nil, ErrReadOnly
	}
	resArchive, err := o.access(archivePath)
	if err != nil {
		return nil, err
	}
	defer resArchive.close()
	if _, err := resArchive.stat(); err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	resDest, err := o.access(destDir)
	if err != nil {
		return nil, err
	}
	defer resDest.close()
	lower := strings.ToLower(resArchive.full)
	extract := extractZipSecure
	switch {
	case strings.HasSuffix(lower, ".zip"):
	case strings.HasSuffix(lower, ".tar.gz"), strings.HasSuffix(lower, ".tgz"):
		extract = extractTarGzSecure
	default:
		return nil, fmt.Errorf("%w: %s", ErrUnsupported, filepath.Base(resArchive.full))
	}

	extractSlots <- struct{}{}
	defer func() { <-extractSlots }()

	// A failed extraction (an escaping entry, the size budget, a write error)
	// removes everything it created, so a rejected archive leaves no partial
	// tree behind. Content that was already in destDir is never touched.
	created := &extractLog{}
	if err := created.mkdirAll(resDest); err != nil {
		created.rollback()
		return nil, err
	}
	if err := extract(resArchive, resDest, created); err != nil {
		created.rollback()
		return nil, err
	}
	return o.Meta(resDest.full)
}

// Archive extraction bounds (PR-07): a paired client can supply a crafted
// archive, so cap entry count and total expanded bytes to defeat zip/tar
// bombs. These are process-wide ceilings, not per-user quotas.
const (
	maxArchiveEntries    = 100_000
	maxArchiveTotalBytes = int64(2) << 30 // 2 GiB expanded
)

// ErrArchiveTooLarge is returned when an archive exceeds the extraction bounds.
var ErrArchiveTooLarge = errors.New("archive exceeds extraction limits")

// copyBounded copies src into dst, debiting *remaining and failing if the
// archive's total expanded size would exceed the budget. It meters the actual
// decompressed stream rather than trusting a header, so a bomb with lying
// declared sizes is still caught.
func copyBounded(dst io.Writer, src io.Reader, remaining *int64) error {
	limited := io.LimitReader(src, *remaining+1)
	n, err := io.Copy(dst, limited)
	if err != nil {
		return err
	}
	if n > *remaining {
		return fmt.Errorf("%w: expanded size over %d bytes", ErrArchiveTooLarge, maxArchiveTotalBytes)
	}
	*remaining -= n
	return nil
}

// safeJoin joins name onto destDir and guarantees the result stays within
// destDir, defeating zip-slip (entries like "../../etc/passwd"). filepath.Join
// cleans the path (collapsing "..") and isUnder then rejects anything that
// climbed out of the destination.
//
// isUnder alone is only a lexical guarantee, so the secure extractors also
// perform every filesystem operation through the destination's os.Root handle.
// This check preserves the prior policy of rejecting pre-existing symlink
// parents; the handle closes the check/use race.
func safeJoin(destDir, name string) (string, error) {
	target := filepath.Join(destDir, name)
	if !isUnder(target, destDir) {
		return "", fmt.Errorf("%w: archive entry escapes destination: %s", ErrForbidden, name)
	}
	if err := checkNoSymlinkParent(destDir, target); err != nil {
		return "", err
	}
	return target, nil
}

// checkNoSymlinkParent rejects target if any existing component between
// destDir and target (inclusive) is a symlink. Archive entries that ARE links
// are already skipped by the extractors; this covers links that were sitting
// in the destination beforehand, which the entry names alone can't reveal.
func checkNoSymlinkParent(destDir, target string) error {
	rel, err := filepath.Rel(destDir, target)
	if err != nil {
		return fmt.Errorf("%w: archive entry escapes destination: %s", ErrForbidden, target)
	}
	cur := destDir
	for _, part := range strings.Split(rel, string(os.PathSeparator)) {
		cur = filepath.Join(cur, part)
		fi, err := os.Lstat(cur)
		if os.IsNotExist(err) {
			// Nothing from here down exists yet — the extractor creates it.
			return nil
		}
		if err != nil {
			return err
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%w: archive entry path crosses a symlink: %s", ErrForbidden, rel)
		}
	}
	return nil
}

// checkEntryNameNotAbsolute refuses an archive entry name that is absolute in
// any supported form, whatever the host OS: POSIX "/x", a leading backslash
// ("\x", UNC "\\server\share"), and a drive prefix ("C:\x", "C:/x", the
// drive-relative "C:x"). Backslashes count as separators here because
// Windows-made zips use them. Such a name is refused, not re-rooted under
// destDir. A relative name, including one with "..", is left to the
// containment check in safeJoin.
func checkEntryNameNotAbsolute(name string) error {
	slashed := strings.ReplaceAll(name, `\`, "/")
	drive := len(slashed) >= 2 && slashed[1] == ':' &&
		(('a' <= slashed[0] && slashed[0] <= 'z') || ('A' <= slashed[0] && slashed[0] <= 'Z'))
	if strings.HasPrefix(slashed, "/") || drive || filepath.IsAbs(name) || filepath.VolumeName(name) != "" {
		return fmt.Errorf("%w: archive entry has an absolute path: %s", ErrForbidden, name)
	}
	return nil
}

func safeJoinSecure(dest *securePath, name string) (*securePath, error) {
	if err := checkEntryNameNotAbsolute(name); err != nil {
		return nil, err
	}
	target, err := safeJoin(dest.full, name)
	if err != nil {
		return nil, err
	}
	rel, err := filepath.Rel(dest.full, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return nil, fmt.Errorf("%w: archive entry escapes destination: %s", ErrForbidden, name)
	}
	return &securePath{full: target, root: dest.root, name: filepath.Join(dest.name, rel)}, nil
}

func createArchiveFile(path *securePath, mode os.FileMode) (*os.File, error) {
	dst, err := uniqueArchivePath(path)
	if err != nil {
		return nil, err
	}
	return dst.openFile(os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
}

// uniqueArchivePath prepares the location for an extracted file: its parent
// exists and, if the name is taken, a free "name (1)" style sibling is used.
// Extraction never replaces a file that is already there (Compress and Copy
// behave the same way).
func uniqueArchivePath(path *securePath) (*securePath, error) {
	if err := path.parent().mkdirAll(0o755); err != nil {
		return nil, err
	}
	if info, err := path.lstat(); err == nil {
		if info.IsDir() {
			return nil, fmt.Errorf("archive file conflicts with a directory")
		}
		return autoRenameSecure(path)
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	return path, nil
}

// extractLog records the files and directories one extraction created, in
// creation order, so a failed extraction can remove exactly those.
type extractLog struct {
	files []createdFile
	dirs  []*securePath
}

// createdFile pairs a path with the identity of the file the extraction
// made there, so rollback never removes something written over it later.
type createdFile struct {
	path *securePath
	info os.FileInfo
}

// recordFile adds a file this extraction created. The identity is pinned now:
// on Windows a FileInfo from Lstat loads its file ID lazily, inside
// os.SameFile, by reopening the path, so an unpinned one would match whatever
// file is at the path at rollback time. Comparing info with itself loads it.
// If that fails the file is recorded without an identity and rollback keeps it.
func (x *extractLog) recordFile(p *securePath, info os.FileInfo) {
	if info != nil && !os.SameFile(info, info) {
		info = nil
	}
	x.files = append(x.files, createdFile{path: p, info: info})
}

// mkdirAll creates p and any missing parents, recording each directory it
// actually created. Directories that already existed are not recorded.
func (x *extractLog) mkdirAll(p *securePath) error {
	var missing []*securePath
	for cur := p; ; cur = cur.parent() {
		if _, err := cur.lstat(); err == nil {
			break
		} else if !os.IsNotExist(err) {
			return err
		}
		missing = append(missing, cur)
		// Stop at the filesystem root, or at the top of an open jail root.
		if next := cur.parent(); next.full == cur.full || (cur.root != nil && cur.name == ".") {
			break
		}
	}
	for i := len(missing) - 1; i >= 0; i-- {
		if err := missing[i].mkdirAll(0o755); err != nil {
			return err
		}
		x.dirs = append(x.dirs, missing[i])
	}
	// The walk stops at the first entry that exists, which may be a file in
	// the way: fail on it as a plain MkdirAll does instead of skipping it.
	return p.mkdirAll(0o755)
}

// rollback removes the recorded files, then the recorded directories deepest
// first. A directory is removed only if empty, so anything else written into
// it concurrently survives.
func (x *extractLog) rollback() {
	for i := len(x.files) - 1; i >= 0; i-- {
		f := x.files[i]
		if now, err := f.path.lstat(); err == nil && f.info != nil && os.SameFile(now, f.info) {
			_ = f.path.remove()
		}
	}
	for i := len(x.dirs) - 1; i >= 0; i-- {
		_ = x.dirs[i].remove()
	}
}

// extractSlots bounds how many archives expand at once across the whole
// process, which makes maxArchiveTotalBytes a real ceiling on the disk one
// burst of requests can fill, as the constant's comment says.
var extractSlots = make(chan struct{}, 1)

// writeArchiveEntry writes one extracted file within the shared byte budget
// and removes the partial file if anything fails, so an archive that blows the
// limit does not leave gigabytes behind.
func writeArchiveEntry(target *securePath, mode os.FileMode, src io.Reader, remaining *int64, created *extractLog) error {
	if err := created.mkdirAll(target.parent()); err != nil {
		return err
	}
	dst, err := uniqueArchivePath(target)
	if err != nil {
		return err
	}
	out, err := dst.openFile(os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	info, err := out.Stat()
	if err != nil {
		_ = out.Close()
		_ = dst.remove()
		return err
	}
	created.recordFile(dst, info)
	err = copyBounded(out, src, remaining)
	if closeErr := out.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = dst.remove()
	}
	return err
}

func extractZipSecure(archive, dest *securePath, created *extractLog) error {
	input, err := archive.open()
	if err != nil {
		return err
	}
	defer input.Close()
	info, err := archive.stat()
	if err != nil {
		return err
	}
	zr, err := zip.NewReader(input, info.Size())
	if err != nil {
		return err
	}
	remaining := maxArchiveTotalBytes
	for i, f := range zr.File {
		if i >= maxArchiveEntries {
			return fmt.Errorf("%w: over %d entries", ErrArchiveTooLarge, maxArchiveEntries)
		}
		target, err := safeJoinSecure(dest, f.Name)
		if err != nil {
			return err
		}
		if f.FileInfo().IsDir() {
			if err := created.mkdirAll(target); err != nil {
				return err
			}
			continue
		}
		if !f.Mode().IsRegular() {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		mode := f.Mode().Perm()
		if mode == 0 {
			mode = 0o644
		}
		err = writeArchiveEntry(target, mode, rc, &remaining, created)
		rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func extractTarGzSecure(archive, dest *securePath, created *extractLog) error {
	input, err := archive.open()
	if err != nil {
		return err
	}
	defer input.Close()
	gz, err := gzip.NewReader(input)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	remaining := maxArchiveTotalBytes
	entries := 0
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		entries++
		if entries > maxArchiveEntries {
			return fmt.Errorf("%w: over %d entries", ErrArchiveTooLarge, maxArchiveEntries)
		}
		target, err := safeJoinSecure(dest, hdr.Name)
		if err != nil {
			return err
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := created.mkdirAll(target); err != nil {
				return err
			}
		case tar.TypeReg:
			mode := os.FileMode(hdr.Mode).Perm()
			if mode == 0 {
				mode = 0o644
			}
			if err := writeArchiveEntry(target, mode, tr, &remaining, created); err != nil {
				return err
			}
		default:
			continue
		}
	}
	return nil
}
