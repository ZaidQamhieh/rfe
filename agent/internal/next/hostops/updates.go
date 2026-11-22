package hostops

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/updates"
)

// APKContentType is the media type of an Android package.
const APKContentType = "application/vnd.android.package-archive"

// Download is the newest APK opened for sending. The caller must call Close.
type Download struct {
	Content io.ReadSeeker
	Name    string
	ModTime time.Time
	Size    int64
	Close   func() error
}

// Updates serves the newest APK found in a folder (files named
// rfe-<versionName>-<versionCode>.apk; the highest versionCode wins).
type Updates struct {
	dir string

	mu       sync.Mutex
	digestOf string // cache key: path, mtime and size
	digest   string
}

// NewUpdates serves APKs from dir. A missing or empty folder means no update.
func NewUpdates(dir string) *Updates { return &Updates{dir: dir} }

// Latest returns the newest release with its SHA-256 filled in (the client
// checks the downloaded APK against it), or nil, nil when there is none. The
// digest is cached by path, size and modification time, so polling does not
// re-read a large file.
func (u *Updates) Latest() (*updates.Release, error) {
	rel, err := updates.Latest(u.dir)
	if err != nil || rel == nil {
		return nil, err
	}
	sum, err := u.sha256Of(updates.Path(u.dir, rel))
	if err != nil {
		return nil, err
	}
	out := *rel
	out.SHA256 = sum
	return &out, nil
}

// Open opens the newest APK for sending, or returns nil, nil when there is
// none. The size is taken from the opened file, so what is sent and what
// Content-Length says come from the same file even if it was replaced since
// the listing.
func (u *Updates) Open() (*Download, error) {
	rel, err := updates.Latest(u.dir)
	if err != nil || rel == nil {
		return nil, err
	}
	f, err := os.Open(updates.Path(u.dir, rel))
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	if !info.Mode().IsRegular() {
		f.Close()
		return nil, fmt.Errorf("update file %q is not a regular file", rel.Filename)
	}
	return &Download{Content: f, Name: rel.Filename, ModTime: info.ModTime(), Size: info.Size(), Close: f.Close}, nil
}

// sha256Of returns the hex SHA-256 of the file at path, cached by size and time.
func (u *Updates) sha256Of(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	key := path + "|" + info.ModTime().String() + "|" + strconv.FormatInt(info.Size(), 10)
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.digestOf == key {
		return u.digest, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	u.digestOf, u.digest = key, hex.EncodeToString(h.Sum(nil))
	return u.digest, nil
}
