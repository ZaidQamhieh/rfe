// Package server — recent-files handler.
//
// Like search.go, this is a live recursive walk rather than a persistent
// index (personal-use agent, normal-sized home folder — see search.go's doc
// comment for the full reasoning). Unlike search, a recent-files walk can't
// stop early once it has `limit` candidates — the Nth file visited in
// directory-walk order isn't necessarily among the N most recently
// modified — so it keeps a bounded min-heap of the best `limit` candidates
// seen so far instead of collecting everything and sorting at the end.
package server

import (
	"container/heap"
	"context"
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/fsops"
)

const (
	recentDefaultLimit       = 100
	recentMaxLimit           = 500
	recentMaxConcurrentScans = 2
	recentBusyRetryAfter     = "1"
	recentTimeBudget         = 15 * time.Second
	recentCacheTTL           = 5 * time.Second
)

// recentResultCache keeps one short-lived result for the current jail and
// query. Recent-file reads are usually repeated by refresh/rebuild flows; a
// brief cache prevents each refresh from walking the same tree again. The
// single entry bounds memory and naturally discards arbitrary root queries.
type recentResultCache struct {
	mu        sync.Mutex
	key       string
	expiresAt time.Time
	entries   []fsops.Entry
}

func (c *recentResultCache) load(key string, now time.Time) ([]fsops.Entry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if key == "" || c.key != key || !now.Before(c.expiresAt) {
		return nil, false
	}
	entries := make([]fsops.Entry, len(c.entries))
	copy(entries, c.entries)
	return entries, true
}

func (c *recentResultCache) store(key string, entries []fsops.Entry, now time.Time) {
	if key == "" {
		return
	}
	c.mu.Lock()
	c.key = key
	c.expiresAt = now.Add(recentCacheTTL)
	c.entries = make([]fsops.Entry, len(entries))
	copy(c.entries, entries)
	c.mu.Unlock()
}

func recentCacheKey(roots []string, limit int) string {
	if len(roots) == 0 {
		return ""
	}
	orderedRoots := append([]string(nil), roots...)
	sort.Strings(orderedRoots)
	// NUL cannot occur in a filesystem path, so it safely separates roots.
	return strconv.Itoa(limit) + "\x00" + strings.Join(orderedRoots, "\x00")
}

// recentHeap is a min-heap of fsops.Entry keyed by Modified time — the
// oldest entry is always at the root, so it's the one to evict when a newer
// candidate arrives and the heap is already at limit.
type recentHeap []fsops.Entry

func (h recentHeap) Len() int           { return len(h) }
func (h recentHeap) Less(i, j int) bool { return h[i].Modified.Before(h[j].Modified) }
func (h recentHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *recentHeap) Push(x any)        { *h = append(*h, x.(fsops.Entry)) }
func (h *recentHeap) Pop() any {
	old := *h
	n := len(old)
	item := old[n-1]
	*h = old[:n-1]
	return item
}

// sortedNewestFirst returns h's contents ordered newest-first, without
// mutating h.
func (h recentHeap) sortedNewestFirst() []fsops.Entry {
	out := make([]fsops.Entry, len(h))
	copy(out, h)
	sort.Slice(out, func(i, j int) bool { return out[i].Modified.After(out[j].Modified) })
	return out
}

// recentHandler lists the most recently modified files (not directories)
// under the agent's configured roots — GET /v1/fs/recent?limit=&root=.
func recentHandler(ops *fsops.Ops) http.HandlerFunc {
	return recentHandlerWithWalker(ops, walkForRecentWithOps)
}

// recentHandlerWithWalker applies a per-handler admission limit to the
// expensive recursive walks. A server registers one handler for this route,
// so the limit is shared by concurrent requests to /fs/recent.
func recentHandlerWithWalker(ops *fsops.Ops, walk func(context.Context, *fsops.Ops, string, int, *recentHeap)) http.HandlerFunc {
	scans := make(chan struct{}, recentMaxConcurrentScans)
	var cache recentResultCache
	return func(w http.ResponseWriter, r *http.Request) {
		ops := opsFromContext(r.Context(), ops)
		query := r.URL.Query()

		limit := recentDefaultLimit
		if l := query.Get("limit"); l != "" {
			if n, err := strconv.Atoi(l); err == nil && n > 0 {
				limit = n
			}
		}
		if limit > recentMaxLimit {
			limit = recentMaxLimit
		}

		var roots []string
		if root := strings.TrimSpace(query.Get("root")); root != "" {
			resolved, err := ops.Resolve(root)
			if err != nil {
				handleFsError(w, err)
				return
			}
			roots = []string{resolved}
		} else {
			roots = ops.Roots()
			if len(roots) == 0 && !ops.IsDenyAll() {
				if home, err := os.UserHomeDir(); err == nil && home != "" {
					roots = []string{home}
				}
			}
		}
		cacheKey := recentCacheKey(roots, limit)
		if r.Context().Err() == nil {
			if entries, ok := cache.load(cacheKey, time.Now()); ok {
				writeJSON(w, http.StatusOK, entries)
				return
			}
		}

		select {
		case scans <- struct{}{}:
			defer func() { <-scans }()
		default:
			w.Header().Set("Retry-After", recentBusyRetryAfter)
			writeError(w, http.StatusTooManyRequests, "RECENT_BUSY", "recent-file scan capacity is full; retry shortly")
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), recentTimeBudget)
		defer cancel()

		h := &recentHeap{}
		heap.Init(h)
		for _, root := range roots {
			walk(ctx, ops, root, limit, h)
			if ctx.Err() != nil {
				break
			}
		}

		if ctx.Err() != nil {
			w.Header().Set(headerSearchTimeBudget, "1")
		}

		entries := h.sortedNewestFirst()
		if ctx.Err() == nil {
			cache.store(cacheKey, entries, time.Now())
		}
		writeJSON(w, http.StatusOK, entries)
	}
}

// walkForRecent recursively walks root, maintaining h as the top-`limit`
// most recently modified files seen so far (directories are never included
// — recents is a files feature). Permission-denied directories are skipped
// silently; other walk errors are ignored too, matching search.go's
// best-effort behavior.
func walkForRecent(ctx context.Context, root string, limit int, h *recentHeap) {
	openedRoot, err := os.OpenRoot(root)
	if err != nil {
		return
	}
	defer openedRoot.Close()
	walkForRecentRoot(ctx, openedRoot, root, limit, h)
}

func walkForRecentWithOps(ctx context.Context, ops *fsops.Ops, rootPath string, limit int, h *recentHeap) {
	openedRoot, err := ops.OpenDir(rootPath)
	if err != nil {
		return
	}
	defer openedRoot.Close()
	walkForRecentRoot(ctx, openedRoot, rootPath, limit, h)
}

func walkForRecentRoot(ctx context.Context, root *os.Root, rootPath string, limit int, h *recentHeap) {
	_ = fs.WalkDir(root.FS(), ".", func(relPath string, d fs.DirEntry, err error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			if errors.Is(err, fs.ErrPermission) {
				if d != nil && d.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if relPath != "." {
				dirPath := filepath.Join(rootPath, filepath.FromSlash(relPath))
				if shouldSkipVirtualDir(dirPath) {
					return fs.SkipDir
				}
			}
			return nil
		}

		info, infoErr := d.Info()
		if infoErr != nil {
			return nil
		}
		// Most files in a large tree will not make the top-K result. Compare
		// metadata first so those entries do not allocate a joined path or a
		// complete fsops.Entry (including MIME/name strings).
		if h.Len() >= limit && !info.ModTime().After((*h)[0].Modified) {
			return nil
		}

		// Symlink metadata is resolved through the open root, like the search index does, so a link cannot make
		// the walk stat anything outside the root it was given.
		entry := fsops.EntryFromRootInfo(root, rootPath, relPath, info)

		if h.Len() < limit {
			heap.Push(h, entry)
		} else if entry.Modified.After((*h)[0].Modified) {
			(*h)[0] = entry
			heap.Fix(h, 0)
		}
		return nil
	})
}
