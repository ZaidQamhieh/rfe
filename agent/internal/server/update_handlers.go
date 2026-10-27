// Package server — in-app update route handlers (Android APK delivery).
package server

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"strconv"
	"sync"

	"github.com/ZaidQamhieh/rfe/agent/internal/updates"
)

// apkDigest returns the SHA-256 of the APK at path, cached by size and modification time so the listing does
// not re-read a large file on every poll.
type apkDigestCache struct {
	mu    sync.Mutex
	key   string
	value string
}

func (c *apkDigestCache) of(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	key := path + "|" + info.ModTime().String() + "|" + strconv.FormatInt(info.Size(), 10)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.key == key {
		return c.value, nil
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
	c.key, c.value = key, hex.EncodeToString(h.Sum(nil))
	return c.value, nil
}

func latestAppHandler(dir string) http.HandlerFunc {
	var digests apkDigestCache
	return func(w http.ResponseWriter, r *http.Request) {
		rel, err := updates.Latest(dir)
		if err != nil {
			writeInternal(w, "latest app", err)
			return
		}
		if rel == nil {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		// The client verifies the downloaded APK against this before installing it.
		sum, err := digests.of(updates.Path(dir, rel))
		if err != nil {
			writeInternal(w, "latest app", err)
			return
		}
		out := *rel
		out.SHA256 = sum
		writeJSON(w, http.StatusOK, out)
	}
}

func downloadAppHandler(dir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rel, err := updates.Latest(dir)
		if err != nil {
			writeInternal(w, "download app", err)
			return
		}
		if rel == nil {
			writeError(w, http.StatusNotFound, "NOT_FOUND", "no update available")
			return
		}
		f, err := os.Open(updates.Path(dir, rel))
		if err != nil {
			writeInternal(w, "download app", err)
			return
		}
		defer f.Close()
		info, err := f.Stat()
		if err != nil {
			writeInternal(w, "download app", err)
			return
		}
		w.Header().Set("Content-Type", "application/vnd.android.package-archive")
		// ServeContent adds Range support, Content-Length, 206/416 handling.
		http.ServeContent(w, r, rel.Filename, info.ModTime(), f)
	}
}
