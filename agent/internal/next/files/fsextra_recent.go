package files

import (
	"container/heap"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/next/api"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/rootfs"
)

// Recent-files limits: a request gets RecentDefaultLimit entries unless it asks
// for another count, and never more than RecentMaxLimit. A walk goes at most
// recentMaxDepth folders deep.
const (
	RecentDefaultLimit = 100
	RecentMaxLimit     = 500
	recentMaxDepth     = 128
)

// virtualLinuxDirs are pseudo-filesystem mount points and ostree internals that
// are huge and irrelevant to a walk; they are pruned when met inside a walk,
// not when given as the start folder.
var virtualLinuxDirs = map[string]bool{
	"/proc":    true,
	"/sys":     true,
	"/dev":     true,
	"/sysroot": true,
}

// noisyDirNames are folder names of dependency caches no walk should enter.
// Folders whose name starts with a dot are pruned too (see PrunedDir).
var noisyDirNames = map[string]bool{
	"node_modules": true,
}

// hiddenPrefix starts the name of a hidden folder.
const hiddenPrefix = '.'

// PrunedDir reports whether a walk over many folders (recent files, search)
// leaves the folder at path out: a Linux pseudo-filesystem, a dot-prefixed
// folder or a known dependency cache. A lone "." or ".." name is not hidden.
func PrunedDir(path string) bool {
	if runtime.GOOS == "linux" && virtualLinuxDirs[filepath.Clean(path)] {
		return true
	}
	name := filepath.Base(path)
	return (len(name) > 1 && name[0] == hiddenPrefix) || noisyDirNames[name]
}

// RecentRequest describes one recent-files query.
type RecentRequest struct {
	// Root is the folder to scan. Empty means the default folders (Roots).
	Root string
	// Roots are scanned when Root is empty: the caller's jail or the configured
	// roots. A folder the Set does not allow, or that is gone, is left out. When
	// Roots is empty the user's home folder is scanned.
	Roots []string
	// Limit is how many files to return: 0 or less means RecentDefaultLimit,
	// and anything above RecentMaxLimit is RecentMaxLimit.
	Limit int
	// HomeDir overrides os.UserHomeDir (tests).
	HomeDir func() (string, error)
}

// RecentResult is the answer of Recent: the files newest first and whether the
// walk was cut short by its context.
type RecentResult struct {
	Entries []Entry
	// Partial is true when the context ended before the walk finished, so more
	// recent files may exist than the ones returned.
	Partial bool
}

// recentCandidate is a file seen by the walk: just enough to rank it. Its full
// Entry is built only for the files that make the final list.
type recentCandidate struct {
	path     string
	modified time.Time
}

// recentBefore reports whether a is listed before b: newest first, and among
// equal times the one that sorts first by path.
func recentBefore(a, b recentCandidate) bool {
	if !a.modified.Equal(b.modified) {
		return a.modified.After(b.modified)
	}
	return a.path < b.path
}

// recentHeap is a min-heap of candidates in which the root is the one to evict
// first: the oldest, and among equal times the one that sorts last by path, so
// the result is the same whatever order the folders are read in.
type recentHeap []recentCandidate

func (h recentHeap) Len() int { return len(h) }
func (h recentHeap) Less(i, j int) bool {
	return recentBefore(h[j], h[i])
}
func (h recentHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h *recentHeap) Push(x any)   { *h = append(*h, x.(recentCandidate)) }
func (h *recentHeap) Pop() any {
	old := *h
	last := old[len(old)-1]
	*h = old[:len(old)-1]
	return last
}

// beats reports whether c should replace the heap's current worst candidate.
func (h recentHeap) beats(c recentCandidate) bool {
	return recentBefore(c, h[0])
}

// Recent returns the most recently modified files (never folders) under the
// requested folder or the default folders, newest first, at most the request's
// limit. It walks through rootfs handles only: a symlink is listed but never
// entered, a link into the protected folder is left out, and hidden folders,
// pseudo-filesystems and dependency caches are not entered. It keeps a bounded
// min-heap of the best candidates, so memory stays at the limit however large
// the tree is. An unreadable folder is skipped. When ctx ends the walk stops
// and Partial is set; the files seen so far are still returned. A named root
// that is missing, not a folder or not allowed is an error.
func (s *Service) Recent(ctx context.Context, req RecentRequest) (*RecentResult, error) {
	limit := req.Limit
	if limit <= 0 {
		limit = RecentDefaultLimit
	}
	limit = min(limit, RecentMaxLimit)

	starts, err := s.recentStarts(req)
	if err != nil {
		return nil, err
	}
	best := &recentHeap{}
	for _, start := range starts {
		h, err := s.recentFolder(start)
		if err != nil {
			continue // vanished since it was checked
		}
		s.recentWalk(ctx, h, best, limit, 0)
		if ctx.Err() != nil {
			break
		}
	}

	ranked := make([]recentCandidate, len(*best))
	copy(ranked, *best)
	sort.Slice(ranked, func(i, j int) bool {
		return recentBefore(ranked[i], ranked[j])
	})
	out := make([]Entry, 0, len(ranked))
	for _, c := range ranked {
		e, err := s.Meta(c.path)
		if err != nil || e.IsDir {
			continue // removed or replaced by a folder since the walk
		}
		out = append(out, *e)
	}
	return &RecentResult{Entries: out, Partial: ctx.Err() != nil}, nil
}

// recentStarts returns the folders to walk. A named root must be a usable
// folder; the default folders are best effort.
func (s *Service) recentStarts(req RecentRequest) ([]string, error) {
	if req.Root != "" {
		h, err := s.recentFolder(req.Root)
		if err != nil {
			return nil, err
		}
		return []string{h.Path()}, nil
	}
	candidates := req.Roots
	if len(candidates) == 0 {
		home := req.HomeDir
		if home == nil {
			home = os.UserHomeDir
		}
		if dir, err := home(); err == nil && dir != "" {
			candidates = []string{dir}
		}
	}
	out := make([]string, 0, len(candidates))
	for _, c := range candidates {
		if h, err := s.recentFolder(c); err == nil {
			out = append(out, h.Path())
		}
	}
	return out, nil
}

// recentFolder opens path as a folder inside the Set. A folder that is, or lies
// inside, the protected folder fails here; one that merely contains it is
// allowed, because the walk leaves the protected part out.
func (s *Service) recentFolder(path string) (*rootfs.Handle, error) {
	h, err := s.open(path)
	if err != nil {
		return nil, err
	}
	info, err := h.Stat()
	if err != nil {
		return nil, classify(err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%w: not a folder", api.ErrBadRequest)
	}
	return h, nil
}

// recentWalk visits the entries of the folder h and of the real folders below
// it, offering each non-folder entry to best. It stops as soon as ctx ends.
func (s *Service) recentWalk(ctx context.Context, h *rootfs.Handle, best *recentHeap, limit, depth int) {
	entries, err := h.ReadDir()
	if err != nil {
		return // unreadable: skipped, like search
	}
	for _, de := range entries {
		if ctx.Err() != nil {
			return
		}
		path := filepath.Join(h.Path(), de.Name())
		if de.IsDir() {
			if depth >= recentMaxDepth || PrunedDir(path) {
				continue
			}
			child, err := s.set.Open(path)
			if err != nil {
				continue
			}
			s.recentWalk(ctx, child, best, limit, depth+1)
			continue
		}
		info, err := de.Info()
		if err != nil {
			continue
		}
		c := recentCandidate{path: path, modified: info.ModTime()}
		if best.Len() >= limit && !best.beats(c) {
			continue // most files of a large tree do not make the list
		}
		if de.Type()&fs.ModeSymlink != 0 && s.linkNotListed(path) {
			continue
		}
		if best.Len() < limit {
			heap.Push(best, c)
		} else {
			(*best)[0] = c
			heap.Fix(best, 0)
		}
	}
}

// linkNotListed reports whether the symlink at path is left out of a recent-files
// list: it leads into the protected folder (a link must not reveal that it
// exists) or to a folder (only files are listed). A broken link stays listed.
func (s *Service) linkNotListed(path string) bool {
	h, err := s.set.Open(path)
	if err != nil {
		return false
	}
	target, err := h.Stat()
	if err != nil {
		return errors.Is(err, rootfs.ErrProtected)
	}
	return target.IsDir()
}
