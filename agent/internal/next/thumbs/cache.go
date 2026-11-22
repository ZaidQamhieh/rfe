package thumbs

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

// Cache layout and housekeeping.
const (
	// cacheSuffix ends every cache file name; the rest is a hex SHA-256.
	cacheSuffix = ".jpg"
	// tmpPrefix starts the name of a cache file still being written.
	tmpPrefix = ".thumb-"
	// tmpRandBytes is the random part of a temp name, in bytes before hex.
	tmpRandBytes = 8
	// cacheDirMode and cacheFileMode keep thumbnails private to the agent's user.
	cacheDirMode  fs.FileMode = 0o700
	cacheFileMode fs.FileMode = 0o600
	// cacheTrimPercent is the share of the size cap a prune cuts down to, so
	// the next write does not have to prune again.
	cacheTrimPercent = 90
	// cachePruneInterval is how often a write also checks entry ages.
	cachePruneInterval = time.Hour
)

// cache is the on-disk thumbnail store. All access goes through an os.Root on
// the cache directory, so a symlink planted inside it cannot redirect a read
// or write. Entries are evicted least recently used first once the byte cap is
// exceeded, and when unused longer than the age limit. Recency is the file's
// modification time, which a hit refreshes.
type cache struct {
	root     *os.Root
	maxBytes int64
	maxAge   time.Duration
	now      func() time.Time

	mu        sync.Mutex // guards bytes, lastPrune and pruning
	bytes     int64
	lastPrune time.Time
}

// newCache creates dir if needed, removes half-written files a crash left
// behind and applies the size and age limits once.
func newCache(dir string, maxBytes int64, maxAge time.Duration, now func() time.Time) (*cache, error) {
	if err := os.MkdirAll(dir, cacheDirMode); err != nil {
		return nil, fmt.Errorf("thumbs: create cache dir: %w", err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, fmt.Errorf("thumbs: open cache dir: %w", err)
	}
	c := &cache{root: root, maxBytes: maxBytes, maxAge: maxAge, now: now}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.removeTempsLocked()
	c.pruneLocked()
	return c, nil
}

// close releases the directory handle.
func (c *cache) close() error { return c.root.Close() }

// isCacheName reports whether name looks like a file this cache wrote, so
// eviction never touches anything else in the directory.
func isCacheName(name string) bool {
	hexPart, ok := strings.CutSuffix(name, cacheSuffix)
	if !ok || len(hexPart) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(hexPart)
	return err == nil
}

// get returns the cached bytes for name and marks the entry as just used. An
// entry unused for longer than the age limit counts as absent.
func (c *cache) get(name string) ([]byte, bool) {
	info, err := c.root.Lstat(name)
	if err != nil || !info.Mode().IsRegular() || info.Size() > c.maxBytes {
		return nil, false
	}
	now := c.now()
	if now.Sub(info.ModTime()) > c.maxAge {
		return nil, false
	}
	data, err := c.root.ReadFile(name)
	if err != nil {
		return nil, false
	}
	// Recency is only an eviction hint; a failed update costs nothing but order.
	_ = c.root.Chtimes(name, now, now)
	return data, true
}

// put stores data under name atomically (temp file, then rename), then evicts
// if the cache is over its cap or the periodic age check is due. Data larger
// than the whole cache is not stored.
func (c *cache) put(name string, data []byte) error {
	if int64(len(data)) > c.maxBytes {
		return nil
	}
	tmp, err := c.writeTemp(data)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	var replaced int64
	if info, err := c.root.Lstat(name); err == nil && info.Mode().IsRegular() {
		replaced = info.Size()
	}
	if err := c.root.Rename(tmp, name); err != nil {
		_ = c.root.Remove(tmp)
		return err
	}
	now := c.now()
	_ = c.root.Chtimes(name, now, now) // eviction order follows the injected clock
	c.bytes += int64(len(data)) - replaced
	if c.bytes > c.maxBytes || now.Sub(c.lastPrune) >= cachePruneInterval {
		c.pruneLocked()
	}
	return nil
}

// writeTemp writes data to a new uniquely named temp file and returns its
// name; on any failure the temp file is removed.
func (c *cache) writeTemp(data []byte) (string, error) {
	var rnd [tmpRandBytes]byte
	if _, err := rand.Read(rnd[:]); err != nil {
		return "", err
	}
	tmp := tmpPrefix + hex.EncodeToString(rnd[:])
	f, err := c.root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, cacheFileMode)
	if err != nil {
		return "", err
	}
	_, werr := f.Write(data)
	cerr := f.Close()
	if err := errors.Join(werr, cerr); err != nil {
		_ = c.root.Remove(tmp)
		return "", err
	}
	return tmp, nil
}

// entries lists the cache directory.
func (c *cache) entries() ([]fs.DirEntry, error) {
	dir, err := c.root.Open(".")
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	return dir.ReadDir(-1)
}

// removeTempsLocked deletes half-written files. Only called at start, when no
// write of this process can be in progress.
func (c *cache) removeTempsLocked() {
	list, err := c.entries()
	if err != nil {
		return
	}
	for _, e := range list {
		if e.Type().IsRegular() && strings.HasPrefix(e.Name(), tmpPrefix) {
			_ = c.root.Remove(e.Name())
		}
	}
}

// cached is one cache file seen by a prune.
type cached struct {
	name string
	size int64
	mod  time.Time
}

// pruneLocked removes entries unused past the age limit, then the least
// recently used ones until the total is at most cacheTrimPercent of the cap.
// It only ever removes files that carry this cache's naming.
func (c *cache) pruneLocked() {
	now := c.now()
	c.lastPrune = now
	list, err := c.entries()
	if err != nil {
		return
	}
	var files []cached
	var total int64
	for _, e := range list {
		if !isCacheName(e.Name()) {
			continue
		}
		info, err := e.Info()
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		if now.Sub(info.ModTime()) > c.maxAge {
			if c.root.Remove(e.Name()) == nil {
				continue
			}
		}
		files = append(files, cached{e.Name(), info.Size(), info.ModTime()})
		total += info.Size()
	}
	if total > c.maxBytes {
		sort.Slice(files, func(i, j int) bool {
			if files[i].mod.Equal(files[j].mod) {
				return files[i].name < files[j].name
			}
			return files[i].mod.Before(files[j].mod)
		})
		target := c.maxBytes / 100 * cacheTrimPercent
		for _, f := range files {
			if total <= target {
				break
			}
			if c.root.Remove(f.name) == nil {
				total -= f.size
			}
		}
	}
	c.bytes = total
}
