package files

import (
	"context"
	"path/filepath"
	"sort"
)

// UsageMaxEntries is how many children a usage answer lists, largest first.
const UsageMaxEntries = 100

// UsageEntry is one immediate child of the scanned folder with the size of everything under it.
type UsageEntry struct {
	Name  string
	IsDir bool
	Bytes int64
}

// UsageResult is the answer of Usage. Partial is true when ctx ended before the walk finished.
type UsageResult struct {
	Path       string
	TotalBytes int64
	Partial    bool
	Entries    []UsageEntry
}

// Usage adds up the file sizes under the folder at path, per immediate child, largest first (at most
// UsageMaxEntries). Like Recent it walks through rootfs handles only: a symlink is counted by its own size and never
// entered, and hidden folders, pseudo-filesystems and dependency caches are skipped. An unreadable folder is skipped.
// When ctx ends the walk stops and Partial is set. A path that is missing, not a folder or not allowed is an error.
func (s *Service) Usage(ctx context.Context, path string) (*UsageResult, error) {
	h, err := s.recentFolder(path)
	if err != nil {
		return nil, err
	}
	sizes := map[string]*UsageEntry{}
	var total int64
	var truncated bool
	entries, err := h.ReadDir()
	if err != nil {
		return nil, classify(err)
	}
	for _, de := range entries {
		if ctx.Err() != nil {
			break
		}
		e := &UsageEntry{Name: de.Name(), IsDir: de.IsDir()}
		sizes[de.Name()] = e
		if !de.IsDir() {
			if info, err := de.Info(); err == nil {
				e.Bytes = info.Size()
			}
			total += e.Bytes
			continue
		}
		child := filepath.Join(h.Path(), de.Name())
		if PrunedDir(child) {
			continue
		}
		e.Bytes = s.usageWalk(ctx, child, 0, &truncated)
		total += e.Bytes
	}
	out := make([]UsageEntry, 0, len(sizes))
	for _, e := range sizes {
		out = append(out, *e)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Bytes != out[j].Bytes {
			return out[i].Bytes > out[j].Bytes
		}
		return out[i].Name < out[j].Name
	})
	if len(out) > UsageMaxEntries {
		out = out[:UsageMaxEntries]
	}
	return &UsageResult{Path: h.Path(), TotalBytes: total, Partial: ctx.Err() != nil || truncated, Entries: out}, nil
}

// usageWalk returns the total size of the files under the folder at path.
// A folder nested deeper than recentMaxDepth is not entered and sets *truncated, so the result is reported partial.
func (s *Service) usageWalk(ctx context.Context, path string, depth int, truncated *bool) int64 {
	h, err := s.set.Open(path)
	if err != nil {
		return 0
	}
	entries, err := h.ReadDir()
	if err != nil {
		return 0
	}
	var sum int64
	for _, de := range entries {
		if ctx.Err() != nil {
			return sum
		}
		child := filepath.Join(h.Path(), de.Name())
		if de.IsDir() {
			if PrunedDir(child) {
				continue
			}
			if depth >= recentMaxDepth {
				*truncated = true
				continue
			}
			sum += s.usageWalk(ctx, child, depth+1, truncated)
			continue
		}
		if info, err := de.Info(); err == nil {
			sum += info.Size()
		}
	}
	return sum
}
