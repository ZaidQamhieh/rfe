// Package backup creates, verifies and restores backups of the agent's identity and state: the database
// (as a consistent snapshot), the TLS certificate and key phones pin, and the account-server signing key.
// Each backup is one tar.gz holding those files plus a manifest of their SHA-256 hashes.
package backup

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"crypto/tls"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	_ "modernc.org/sqlite" // pure-Go SQLite driver, the same one the store uses

	"github.com/ZaidQamhieh/rfe/agent/internal/security"
	"github.com/ZaidQamhieh/rfe/agent/internal/store"
)

// Member names inside an archive. They are fixed names, never taken from the archive's own headers
// when writing to disk, so an archive cannot place a file anywhere else.
const (
	DBMember     = store.DBFile
	CertMember   = security.CertFile
	KeyMember    = security.KeyFile
	AccountKey   = store.AccountKeyFile
	ManifestName = "manifest.json"
)

// FormatVersion is the manifest layout version this package writes and accepts.
const FormatVersion = 1

// Backup labels used in archive names and by the retention policy.
const (
	LabelPremigration = "premigration"
	LabelDaily        = "daily"
	LabelWeekly       = "weekly"
	LabelManual       = "manual"
)

// sqliteBusyTimeoutMs is how long the snapshot waits for a lock held by the running daemon.
const sqliteBusyTimeoutMs = 5000

// maxManifestBytes bounds the manifest read from an archive.
const maxManifestBytes = 1 << 20

// archiveTimeLayout is the UTC timestamp in an archive's file name; it sorts chronologically.
const archiveTimeLayout = "20060102T150405Z"

// archiveNameRE matches a backup file name: rfe-backup-<label>-<timestamp>.tar.gz.
var archiveNameRE = regexp.MustCompile(`^rfe-backup-([a-z0-9]+)-(\d{8}T\d{6}Z)\.tar\.gz$`)

var labelRE = regexp.MustCompile(`^[a-z0-9]+$`)

// The data dir holds no other configuration file: settings, roots and device grants are rows of
// agent.db, which the snapshot already contains. Caches (thumbs, transfers, updates, trash) and
// sidecar-stats.json are rebuilt by the daemon and are deliberately not backed up.
var (
	requiredMembers = []string{DBMember, CertMember, KeyMember}
	optionalMembers = []string{AccountKey}
)

// Member describes one file in the archive.
type Member struct {
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// Manifest is the archive's table of contents, stored as manifest.json.
type Manifest struct {
	Format          int       `json:"format"`
	Created         time.Time `json:"created"`
	AgentVersion    string    `json:"agent_version"`
	UserVersion     int       `json:"user_version"`
	CertFingerprint string    `json:"cert_fingerprint"`
	Members         []Member  `json:"members"`
}

// Options controls Create.
type Options struct {
	Label        string           // archive label, lowercase letters and digits; default "manual"
	AgentVersion string           // recorded in the manifest
	Now          func() time.Time // clock; default time.Now
}

// ArchiveName returns the file name for a backup with the given label taken at t.
func ArchiveName(label string, t time.Time) string {
	return "rfe-backup-" + label + "-" + t.UTC().Format(archiveTimeLayout) + ".tar.gz"
}

// sqliteURI builds a file: URI for path (escaping ? # % and spaces) with the given raw query.
func sqliteURI(path, query string) string {
	p := filepath.ToSlash(path)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	u := url.URL{Scheme: "file", Path: p, RawQuery: query}
	return u.String()
}

// Snapshot writes a consistent copy of dataDir/agent.db to destDir/agent.db using VACUUM INTO, which
// reads through SQLite itself, so the daemon may keep running and the -wal and -shm files are never
// copied. The copy is fsynced and checked with PRAGMA integrity_check before its path is returned.
func Snapshot(dataDir, destDir string) (string, error) {
	src, err := filepath.Abs(filepath.Join(dataDir, store.DBFile))
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(src); err != nil {
		return "", fmt.Errorf("agent database: %w", err)
	}
	dst, err := filepath.Abs(filepath.Join(destDir, DBMember))
	if err != nil {
		return "", err
	}
	if _, err := os.Lstat(dst); err == nil {
		return "", fmt.Errorf("snapshot target %s already exists", dst)
	}
	db, err := sql.Open("sqlite", sqliteURI(src, fmt.Sprintf("mode=rw&_pragma=busy_timeout(%d)", sqliteBusyTimeoutMs)))
	if err != nil {
		return "", fmt.Errorf("open database: %w", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	if _, err := db.Exec("VACUUM INTO ?", dst); err != nil {
		os.Remove(dst)
		return "", fmt.Errorf("snapshot database: %w", err)
	}
	if err := syncFile(dst); err != nil {
		os.Remove(dst)
		return "", err
	}
	if _, err := inspectDB(dst); err != nil {
		os.Remove(dst)
		return "", fmt.Errorf("snapshot failed its integrity check: %w", err)
	}
	return dst, nil
}

// inspectDB opens the database file read-only (without locking, so no -wal or -shm file is made),
// runs PRAGMA integrity_check and returns its user_version. Anything but a single "ok" is an error.
func inspectDB(path string) (int, error) {
	db, err := sql.Open("sqlite", sqliteURI(path, "mode=ro&immutable=1"))
	if err != nil {
		return 0, err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	rows, err := db.Query("PRAGMA integrity_check")
	if err != nil {
		return 0, fmt.Errorf("integrity_check: %w", err)
	}
	var results []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			rows.Close()
			return 0, err
		}
		results = append(results, s)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, fmt.Errorf("integrity_check: %w", err)
	}
	rows.Close()
	if len(results) != 1 || results[0] != "ok" {
		return 0, fmt.Errorf("integrity_check reported: %s", strings.Join(results, "; "))
	}
	var uv int
	if err := db.QueryRow("PRAGMA user_version").Scan(&uv); err != nil {
		return 0, err
	}
	return uv, nil
}

// syncFile fsyncs the file at path.
func syncFile(path string) error {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// syncDir fsyncs a directory so a rename inside it survives a crash. Windows cannot open directories
// for this, so it is skipped there.
func syncDir(dir string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	if err := d.Sync(); err != nil {
		d.Close()
		return err
	}
	return d.Close()
}

// memberMode is the file mode a member gets when restored: the certificate is public, the rest is secret.
func memberMode(name string) os.FileMode {
	if name == CertMember {
		return 0o644
	}
	return 0o600
}

// Create writes one backup archive of dataDir into destDir and returns its path and manifest. The
// archive is written to a temporary file, fsynced and renamed, so a crash never leaves a half-written
// backup under the final name. A missing certificate or key is an error (security.ErrIdentityMissing),
// because a backup without the identity cannot restore a working host. Manifest.Created is taken
// before the database snapshot, so it is a safe cutoff for replaying the revocation log.
func Create(dataDir, destDir string, opt Options) (string, *Manifest, error) {
	now := opt.Now
	if now == nil {
		now = time.Now
	}
	label := opt.Label
	if label == "" {
		label = LabelManual
	}
	if !labelRE.MatchString(label) {
		return "", nil, fmt.Errorf("invalid backup label %q: use lowercase letters and digits", label)
	}
	cert, err := security.LoadCert(dataDir)
	if err != nil {
		return "", nil, err
	}
	if err := os.MkdirAll(destDir, 0o700); err != nil {
		return "", nil, err
	}
	created := now().UTC()
	final := filepath.Join(destDir, ArchiveName(label, created))
	if _, err := os.Lstat(final); err == nil {
		return "", nil, fmt.Errorf("backup %s already exists", final)
	}

	staging, err := os.MkdirTemp(destDir, ".staging-*")
	if err != nil {
		return "", nil, err
	}
	defer os.RemoveAll(staging)
	dbPath, err := Snapshot(dataDir, staging)
	if err != nil {
		return "", nil, err
	}
	userVersion, err := inspectDB(dbPath)
	if err != nil {
		return "", nil, err
	}

	type source struct{ name, path string }
	sources := []source{
		{DBMember, dbPath},
		{CertMember, filepath.Join(dataDir, CertMember)},
		{KeyMember, filepath.Join(dataDir, KeyMember)},
	}
	for _, name := range optionalMembers {
		p := filepath.Join(dataDir, name)
		if _, err := os.Stat(p); err == nil {
			sources = append(sources, source{name, p})
		} else if !errors.Is(err, fs.ErrNotExist) {
			return "", nil, err
		}
	}

	tmp, err := os.CreateTemp(destDir, ".rfe-backup-*.tmp")
	if err != nil {
		return "", nil, err
	}
	tmpName := tmp.Name()
	committed := false
	defer func() {
		if !committed {
			tmp.Close()
			os.Remove(tmpName)
		}
	}()
	gz := gzip.NewWriter(tmp)
	tw := tar.NewWriter(gz)
	manifest := &Manifest{
		Format:          FormatVersion,
		Created:         created,
		AgentVersion:    opt.AgentVersion,
		UserVersion:     userVersion,
		CertFingerprint: security.Fingerprint(cert),
	}
	for _, s := range sources {
		m, err := addFile(tw, s.name, s.path, created)
		if err != nil {
			return "", nil, err
		}
		manifest.Members = append(manifest.Members, m)
	}
	raw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return "", nil, err
	}
	if err := writeTarBytes(tw, ManifestName, raw, created); err != nil {
		return "", nil, err
	}
	if err := tw.Close(); err != nil {
		return "", nil, err
	}
	if err := gz.Close(); err != nil {
		return "", nil, err
	}
	if err := tmp.Sync(); err != nil {
		return "", nil, err
	}
	if err := tmp.Close(); err != nil {
		return "", nil, err
	}
	if err := os.Rename(tmpName, final); err != nil {
		return "", nil, err
	}
	committed = true
	if err := syncDir(destDir); err != nil {
		return final, manifest, fmt.Errorf("backup written but its directory could not be synced: %w", err)
	}
	return final, manifest, nil
}

// addFile copies the file at path into the tar as name, hashing exactly the bytes it writes.
func addFile(tw *tar.Writer, name, path string, mod time.Time) (Member, error) {
	f, err := os.Open(path)
	if err != nil {
		return Member{}, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return Member{}, err
	}
	if !info.Mode().IsRegular() {
		return Member{}, fmt.Errorf("%s is not a regular file", path)
	}
	hdr := &tar.Header{Name: name, Mode: int64(memberMode(name)), Size: info.Size(), ModTime: mod, Typeflag: tar.TypeReg}
	if err := tw.WriteHeader(hdr); err != nil {
		return Member{}, err
	}
	h := sha256.New()
	if _, err := io.CopyN(io.MultiWriter(tw, h), f, info.Size()); err != nil {
		return Member{}, fmt.Errorf("copy %s: %w", name, err)
	}
	return Member{Name: name, Size: info.Size(), SHA256: hex.EncodeToString(h.Sum(nil))}, nil
}

// writeTarBytes adds an in-memory file to the tar.
func writeTarBytes(tw *tar.Writer, name string, data []byte, mod time.Time) error {
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(data)), ModTime: mod, Typeflag: tar.TypeReg}); err != nil {
		return err
	}
	_, err := tw.Write(data)
	return err
}

// allowedMember reports whether name is one of the fixed names an archive may contain.
func allowedMember(name string) bool {
	if name == ManifestName {
		return true
	}
	for _, list := range [][]string{requiredMembers, optionalMembers} {
		for _, n := range list {
			if n == name {
				return true
			}
		}
	}
	return false
}

// extract unpacks the archive's members into dir (which must exist) and returns the manifest it carries
// and the size and SHA-256 of what was actually written. Only the fixed member names are accepted, so an
// archive cannot write outside dir; every other entry type or name is an error.
func extract(archive, dir string) (*Manifest, map[string]Member, error) {
	f, err := os.Open(archive)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return nil, nil, fmt.Errorf("not a gzip archive: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	var manifest *Manifest
	got := map[string]Member{}
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, nil, fmt.Errorf("read archive: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg {
			return nil, nil, fmt.Errorf("archive entry %q is not a regular file", hdr.Name)
		}
		if !allowedMember(hdr.Name) {
			return nil, nil, fmt.Errorf("archive entry %q is not a known backup member", hdr.Name)
		}
		if _, dup := got[hdr.Name]; dup || (hdr.Name == ManifestName && manifest != nil) {
			return nil, nil, fmt.Errorf("archive entry %q appears twice", hdr.Name)
		}
		if hdr.Name == ManifestName {
			raw, err := io.ReadAll(io.LimitReader(tr, maxManifestBytes+1))
			if err != nil {
				return nil, nil, fmt.Errorf("read manifest: %w", err)
			}
			if len(raw) > maxManifestBytes {
				return nil, nil, errors.New("manifest is too large")
			}
			manifest = &Manifest{}
			if err := json.Unmarshal(raw, manifest); err != nil {
				return nil, nil, fmt.Errorf("parse manifest: %w", err)
			}
			continue
		}
		m, err := writeMember(tr, filepath.Join(dir, hdr.Name), hdr.Name)
		if err != nil {
			return nil, nil, err
		}
		got[hdr.Name] = m
	}
	if manifest == nil {
		return nil, nil, errors.New("archive has no " + ManifestName)
	}
	return manifest, got, nil
}

// writeMember writes r to path (which must not exist), fsyncs it and returns its size and hash.
func writeMember(r io.Reader, path, name string) (Member, error) {
	out, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, memberMode(name))
	if err != nil {
		return Member{}, err
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(out, h), r)
	if err == nil {
		err = out.Sync()
	}
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return Member{}, fmt.Errorf("extract %s: %w", name, err)
	}
	return Member{Name: name, Size: n, SHA256: hex.EncodeToString(h.Sum(nil))}, nil
}

// checkExtracted compares what was extracted into dir with the manifest: same members, same sizes and
// hashes, a database that passes integrity_check at the recorded user_version, and a certificate and
// key that load together and carry the recorded fingerprint.
func checkExtracted(dir string, m *Manifest, got map[string]Member) error {
	if m.Format != FormatVersion {
		return fmt.Errorf("unsupported manifest format %d (this build reads %d)", m.Format, FormatVersion)
	}
	seen := map[string]bool{}
	for _, want := range m.Members {
		if seen[want.Name] {
			return fmt.Errorf("manifest lists %s twice", want.Name)
		}
		seen[want.Name] = true
		have, ok := got[want.Name]
		if !ok {
			return fmt.Errorf("member %s is in the manifest but not in the archive", want.Name)
		}
		if have.Size != want.Size || !strings.EqualFold(have.SHA256, want.SHA256) {
			return fmt.Errorf("member %s does not match its manifest hash (archive is corrupt or was modified)", want.Name)
		}
	}
	for name := range got {
		if !seen[name] {
			return fmt.Errorf("member %s is in the archive but not in the manifest", name)
		}
	}
	for _, name := range requiredMembers {
		if !seen[name] {
			return fmt.Errorf("manifest has no %s", name)
		}
	}
	uv, err := inspectDB(filepath.Join(dir, DBMember))
	if err != nil {
		return fmt.Errorf("database in archive: %w", err)
	}
	if uv != m.UserVersion {
		return fmt.Errorf("database user_version is %d but the manifest says %d", uv, m.UserVersion)
	}
	cert, err := tls.LoadX509KeyPair(filepath.Join(dir, CertMember), filepath.Join(dir, KeyMember))
	if err != nil {
		return fmt.Errorf("certificate and key in archive: %w", err)
	}
	if fp := security.Fingerprint(cert); fp != m.CertFingerprint {
		return fmt.Errorf("certificate fingerprint is %s but the manifest says %s", fp, m.CertFingerprint)
	}
	return nil
}

// Verify checks a backup archive without touching any data dir: it re-hashes every member against the
// manifest, runs integrity_check on the database and confirms the certificate fingerprint. The archive is
// unpacked into a temporary directory that is removed afterwards.
func Verify(archive string) (*Manifest, error) {
	tmp, err := os.MkdirTemp("", "rfe-verify-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	m, got, err := extract(archive, tmp)
	if err != nil {
		return nil, err
	}
	if err := checkExtracted(tmp, m, got); err != nil {
		return nil, err
	}
	return m, nil
}
