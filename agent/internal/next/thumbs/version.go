package thumbs

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"time"
)

// etagHexLen is how many hex digits of the key digest the ETag carries.
const etagHexLen = 32

// version names one content version of a source file: where it is and its
// size, modification time and (where the system has one) device and inode. A
// changed or replaced file has a different version, so it never shares a
// cached thumbnail with the old one.
type version struct {
	path  string
	size  int64
	mod   time.Time
	ident string
}

// versionOf builds the version from the already-open file's information.
func versionOf(path string, info fs.FileInfo) version {
	return version{path: path, size: info.Size(), mod: info.ModTime(), ident: fileIdentity(info)}
}

// same reports whether info still describes this version.
func (v version) same(info fs.FileInfo) bool {
	return info.Size() == v.size && info.ModTime().Equal(v.mod) && fileIdentity(info) == v.ident
}

// digest hashes the version and the bucket into a hex string.
func (v version) digest(bucket int) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%d\x00%d\x00%d", v.path, v.ident, v.size, v.mod.UnixNano(), bucket)))
	return hex.EncodeToString(sum[:])
}

// cacheName is the cache file name for this version at a bucket.
func (v version) cacheName(bucket int) string { return v.digest(bucket) + cacheSuffix }

// result wraps thumbnail bytes with the values for the response headers.
func (v version) result(data []byte, bucket int, cached bool) *Result {
	return &Result{
		Data:        data,
		ContentType: ContentType,
		ETag:        `W/"` + v.digest(bucket)[:etagHexLen] + `"`,
		ModTime:     v.mod.UTC(),
		Size:        bucket,
		Cached:      cached,
	}
}

// crashKey is the version as the crash log remembers it.
func (v version) crashKey() crashKey {
	return crashKey{path: v.path, size: v.size, mod: v.mod.UnixNano(), ident: v.ident}
}
