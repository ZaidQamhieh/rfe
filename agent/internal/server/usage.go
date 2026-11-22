// Package server — storage-usage handler.
//
// GET /v1/fs/usage?path= answers "what is eating space in this folder": the total size of each immediate child (a
// folder's size is everything under it), largest first. Like search and recent it is a live walk under a time budget,
// through the rooted handle so a symlink cannot lead it outside the jail; symlinks are counted by their own size and
// never followed, and hidden and dependency-cache folders are skipped as in search.
package server

import (
	"context"
	"io/fs"
	"net/http"
	"os"
	"sort"
	"strings"

	"github.com/ZaidQamhieh/rfe/agent/internal/fsops"
)

const (
	usageMaxConcurrentScans = 2
	usageMaxEntries         = 100
)

// usageEntry is one immediate child of the scanned folder with its aggregated size.
type usageEntry struct {
	Name  string `json:"name"`
	IsDir bool   `json:"isDir"`
	Bytes int64  `json:"bytes"`
}

// usageResponse is the body of GET /fs/usage.
type usageResponse struct {
	Path       string       `json:"path"`
	TotalBytes int64        `json:"totalBytes"`
	Partial    bool         `json:"partial"`
	Entries    []usageEntry `json:"entries"`
}

// scanUsage adds up file sizes under rootPath per immediate child, stopping when ctx is done.
func scanUsage(ctx context.Context, ops *fsops.Ops, rootPath string) usageResponse {
	sizes := map[string]*usageEntry{}
	var total int64
	walkLiveRoot(ctx, ops, rootPath, func(_ *os.Root, relPath string, d fs.DirEntry) error {
		if relPath == "." {
			return nil
		}
		first, rest, nested := strings.Cut(relPath, "/")
		e := sizes[first]
		if e == nil {
			e = &usageEntry{Name: first, IsDir: nested || d.IsDir()}
			sizes[first] = e
		}
		_ = rest
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		e.Bytes += info.Size()
		total += info.Size()
		return nil
	})
	out := make([]usageEntry, 0, len(sizes))
	for _, e := range sizes {
		out = append(out, *e)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Bytes != out[j].Bytes {
			return out[i].Bytes > out[j].Bytes
		}
		return out[i].Name < out[j].Name
	})
	if len(out) > usageMaxEntries {
		out = out[:usageMaxEntries]
	}
	return usageResponse{Path: rootPath, TotalBytes: total, Partial: ctx.Err() != nil, Entries: out}
}

// usageHandler serves GET /v1/fs/usage?path=.
func usageHandler(ops *fsops.Ops) http.HandlerFunc {
	scans := make(chan struct{}, usageMaxConcurrentScans)
	return func(w http.ResponseWriter, r *http.Request) {
		ops := opsFromContext(r.Context(), ops)
		path := r.URL.Query().Get("path")
		if strings.TrimSpace(path) == "" {
			writeError(w, http.StatusBadRequest, "BAD_REQUEST", "path is required")
			return
		}
		resolved, err := ops.Resolve(path)
		if err != nil {
			handleFsError(w, err)
			return
		}
		if st, err := os.Stat(resolved); err != nil || !st.IsDir() {
			writeError(w, http.StatusBadRequest, codeBadRequest, "path is not a folder")
			return
		}
		select {
		case scans <- struct{}{}:
			defer func() { <-scans }()
		default:
			w.Header().Set("Retry-After", busyRetryAfter)
			writeError(w, http.StatusTooManyRequests, "USAGE_BUSY", "storage scan capacity is full; retry shortly")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), searchTimeBudget)
		defer cancel()
		res := scanUsage(ctx, ops, resolved)
		if res.Partial {
			w.Header().Set(headerSearchTimeBudget, "1")
		}
		writeJSON(w, http.StatusOK, res)
	}
}
