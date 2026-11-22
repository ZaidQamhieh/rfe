// Package bundle describes one installed version of the agent as a directory of files plus a manifest.json that
// lists every file with its SHA-256, size and mode. Build writes the manifest, Verify re-hashes everything, and
// VerifyFile checks one file (the launcher uses it for the agent binary, the agent for its helpers).
package bundle

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// ManifestName is the file Build writes inside the bundle directory; it is never listed in itself.
const ManifestName = "manifest.json"

// maxManifestBytes bounds how much of a manifest is read, so a hostile or damaged file cannot exhaust memory.
const maxManifestBytes = 4 << 20

// File is one manifest entry. Path is slash-separated and relative to the bundle directory.
type File struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
	Mode   uint32 `json:"mode"`
}

// Manifest is the content of manifest.json.
type Manifest struct {
	Version string `json:"version"`
	GOOS    string `json:"goos"`
	GOARCH  string `json:"goarch"`
	Files   []File `json:"files"`
}

// Info is what a successful VerifyFile learned about a file.
type Info struct {
	Version string
	SHA256  string
}

// ErrMismatch is wrapped by every error that says a file differs from its manifest entry.
var ErrMismatch = errors.New("bundle file does not match its manifest")

// ErrNoManifest is wrapped when the directory has no manifest.json (check with errors.Is(err, fs.ErrNotExist) too).
var ErrNoManifest = errors.New("bundle has no " + ManifestName)

// hashFile returns the lowercase hex SHA-256 of the regular file at p and its size, opening it without following
// a final symlink where the platform allows it (the caller has already Lstat-ed it).
func hashFile(p string) (string, int64, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

// Build hashes every regular file under dir (except manifest.json), writes manifest.json there atomically
// (temp file, fsync, rename) and returns it. Symlinks and other special files are an error: a bundle is plain files.
func Build(dir, version, goos, goarch string) (*Manifest, error) {
	if version == "" {
		return nil, errors.New("bundle version is empty")
	}
	m := &Manifest{Version: version, GOOS: goos, GOARCH: goarch, Files: []File{}}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == ManifestName || strings.HasPrefix(rel, ManifestName+".tmp") {
			return nil
		}
		st, err := d.Info()
		if err != nil {
			return err
		}
		if !st.Mode().IsRegular() {
			return fmt.Errorf("bundle file %s is not a regular file", rel)
		}
		sum, n, err := hashFile(p)
		if err != nil {
			return err
		}
		m.Files = append(m.Files, File{Path: rel, SHA256: sum, Size: n, Mode: uint32(st.Mode().Perm())})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(m.Files, func(i, j int) bool { return m.Files[i].Path < m.Files[j].Path })
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := writeAtomic(filepath.Join(dir, ManifestName), append(data, '\n')); err != nil {
		return nil, err
	}
	return m, nil
}

// writeAtomic writes data to a temp file beside target, syncs it and renames it over target.
func writeAtomic(target string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(target), ManifestName+".tmp*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp, target)
	}
	if err != nil {
		os.Remove(tmp)
	}
	return err
}

// Read parses dir/manifest.json. A missing file wraps ErrNoManifest and fs.ErrNotExist.
func Read(dir string) (*Manifest, error) {
	f, err := os.Open(filepath.Join(dir, ManifestName))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("%w: %w", ErrNoManifest, err)
		}
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxManifestBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxManifestBytes {
		return nil, fmt.Errorf("%s is larger than %d bytes", ManifestName, maxManifestBytes)
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("%s is not valid JSON: %w", ManifestName, err)
	}
	if m.Version == "" {
		return nil, fmt.Errorf("%s has no version", ManifestName)
	}
	for _, e := range m.Files {
		if !validRel(e.Path) {
			return nil, fmt.Errorf("%s lists an unsafe path %q", ManifestName, e.Path)
		}
	}
	return &m, nil
}

// validRel reports whether p is a clean, relative, slash-separated path that stays inside the bundle directory.
func validRel(p string) bool {
	if p == "" || strings.ContainsAny(p, "\\:") || path.IsAbs(p) {
		return false
	}
	return path.Clean(p) == p && p != "." && p != ".." && !strings.HasPrefix(p, "../")
}

// checkEntry re-hashes the file for e under dir, refusing symlinks and any size or hash difference.
func checkEntry(dir string, e File) error {
	p := filepath.Join(dir, filepath.FromSlash(e.Path))
	st, err := os.Lstat(p)
	if err != nil {
		return fmt.Errorf("%w: %s: %w", ErrMismatch, e.Path, err)
	}
	if !st.Mode().IsRegular() {
		return fmt.Errorf("%w: %s is not a regular file", ErrMismatch, e.Path)
	}
	sum, n, err := hashFile(p)
	if err != nil {
		return fmt.Errorf("%w: %s: %w", ErrMismatch, e.Path, err)
	}
	if n != e.Size || !strings.EqualFold(sum, e.SHA256) {
		return fmt.Errorf("%w: %s (sha256 %s, expected %s)", ErrMismatch, e.Path, sum, e.SHA256)
	}
	return nil
}

// Verify re-hashes every file the manifest in dir lists and returns the manifest. The first difference is an error
// wrapping ErrMismatch. Files the manifest does not list are ignored.
func Verify(dir string) (*Manifest, error) {
	m, err := Read(dir)
	if err != nil {
		return nil, err
	}
	for _, e := range m.Files {
		if err := checkEntry(dir, e); err != nil {
			return nil, err
		}
	}
	return m, nil
}

// VerifyFileInfo checks the single file rel (slash-separated, inside dir) against the manifest in dir and returns
// the bundle version and the file's digest. A file the manifest does not list is a mismatch.
func VerifyFileInfo(dir, rel string) (Info, error) {
	if !validRel(rel) {
		return Info{}, fmt.Errorf("unsafe bundle path %q", rel)
	}
	m, err := Read(dir)
	if err != nil {
		return Info{}, err
	}
	for _, e := range m.Files {
		if e.Path == rel {
			if err := checkEntry(dir, e); err != nil {
				return Info{}, err
			}
			return Info{Version: m.Version, SHA256: strings.ToLower(e.SHA256)}, nil
		}
	}
	return Info{}, fmt.Errorf("%w: %s is not listed in %s", ErrMismatch, rel, ManifestName)
}

// VerifyFile is VerifyFileInfo without the result: nil means the file still matches the manifest.
func VerifyFile(dir, rel string) error {
	_, err := VerifyFileInfo(dir, rel)
	return err
}
