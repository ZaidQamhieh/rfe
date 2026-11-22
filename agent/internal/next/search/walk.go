package search

import (
	"context"
	"io/fs"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ZaidQamhieh/rfe/agent/internal/next/files"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/rootfs"
)

// walker is one live search. It visits entries depth-first with the names of
// each folder sorted, going through rootfs handles only: a symlink is an entry
// but is never entered, and the protected folder is left out of every listing
// by rootfs.
type walker struct {
	s       *Service
	ctx     context.Context
	f       *filters
	limit   int
	content bool
	needle  []byte

	out     []files.Entry
	stopped bool

	// root is the start folder being walked.
	root string
	// posRoot, posDir and posName identify the last visited entry; the emit*
	// fields the last returned one. Directory slices are never modified after
	// creation, so keeping them costs no copy per entry.
	posRoot, emitRoot string
	posDir, emitDir   []string
	posName, emitName string
	hasPos            bool
	truncated         bool
}

// run walks the start folders in order, continuing after cur when it is set,
// and builds the result.
func (w *walker) run(starts []string, cur *cursor) *Result {
	w.out = []files.Entry{}
	for i, start := range starts {
		if cur != nil && i < cur.rootIdx {
			continue
		}
		h, err := w.s.folder(start)
		if err != nil {
			continue // vanished since the request was checked
		}
		w.root = start
		var resume []string
		if cur != nil && i == cur.rootIdx {
			resume = cur.rel
		}
		w.walkDir(h, nil, resume, 0)
		if w.stopped {
			break
		}
	}
	res := &Result{Entries: w.out}
	switch {
	case w.truncated:
		res.Truncated = true
		c := encodeCursor(w.emitRoot, w.emitDir, w.emitName)
		res.NextCursor = &c
	case w.ctx.Err() != nil:
		res.TimeBudget = true
		if w.hasPos {
			c := encodeCursor(w.posRoot, w.posDir, w.posName)
			res.NextCursor = &c
		}
	}
	return res
}

// walkDir visits the entries of the folder h (at rel below the start folder).
// A non-nil resume is the path of the entry an earlier page ended on: entries
// sorted before it are skipped, the entry itself is not processed again but its
// children are, and everything after it is processed normally. An unreadable
// folder is skipped: search is best effort.
func (w *walker) walkDir(h *rootfs.Handle, rel, resume []string, depth int) {
	entries, err := h.ReadDir()
	if err != nil {
		return
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, de := range entries {
		if w.stopped {
			return
		}
		name := de.Name()
		if resume != nil {
			switch {
			case name < resume[0]:
				continue
			case name == resume[0]:
				var sub []string
				if len(resume) > 1 {
					sub = resume[1:]
				}
				resume = nil
				w.descend(h, de, rel, name, sub, depth)
				continue
			default:
				resume = nil
			}
		}
		w.visit(h, de, rel, name, depth)
	}
}

// visit processes one entry: it records the position, tests the entry, then
// enters it when it is a folder that is not pruned.
func (w *walker) visit(parent *rootfs.Handle, de fs.DirEntry, rel []string, name string, depth int) {
	if w.ctx.Err() != nil {
		w.stopped = true
		return
	}
	path := filepath.Join(parent.Path(), name)
	if de.IsDir() && skipDir(path) {
		return
	}
	w.posRoot, w.posDir, w.posName, w.hasPos = w.root, rel, name, true
	if w.content {
		w.considerContent(de, path, rel, name)
	} else {
		w.considerName(de, path, rel, name)
	}
	if w.stopped {
		return
	}
	w.descend(parent, de, rel, name, nil, depth)
}

// descend walks into the entry when it is a real folder (not a symlink), is
// not pruned, and is not deeper than MaxDepth. resume continues an earlier
// page inside it.
func (w *walker) descend(parent *rootfs.Handle, de fs.DirEntry, rel []string, name string, resume []string, depth int) {
	if !de.IsDir() || depth >= MaxDepth {
		return
	}
	path := filepath.Join(parent.Path(), name)
	if skipDir(path) {
		return
	}
	h, err := w.s.set.Open(path)
	if err != nil {
		return
	}
	childRel := append(append([]string(nil), rel...), name)
	w.walkDir(h, childRel, resume, depth+1)
}

// considerName tests one entry against a name query.
func (w *walker) considerName(de fs.DirEntry, path string, rel []string, name string) {
	if !w.f.matchName(strings.ToLower(name)) {
		return
	}
	if de.Type()&fs.ModeSymlink != 0 && w.s.reachesProtected(path) {
		return
	}
	e, err := w.s.files.Meta(path)
	if err != nil || !w.f.matchEntry(e) {
		return
	}
	w.emit(*e, rel, name)
}

// considerContent tests one regular file against a content query. Folders,
// symlinks and other special files are never read.
func (w *walker) considerContent(de fs.DirEntry, path string, rel []string, name string) {
	if !de.Type().IsRegular() {
		return
	}
	info, err := de.Info()
	if err != nil || info.Size() > MaxContentFileBytes || !w.f.matchMeta(name, false, info.Size(), info.ModTime()) {
		return
	}
	h, err := w.s.set.Open(path)
	if err != nil {
		return
	}
	snip, found, err := fileMatch(w.ctx, h, w.needle)
	if err != nil || !found {
		return
	}
	e, err := w.s.files.Meta(path)
	if err != nil {
		return
	}
	e.Snippet = snip
	w.emit(*e, rel, name)
}

// emit adds a match, or, when the page is already full, stops the walk and
// marks the result as truncated: a match beyond the limit proves more exist.
func (w *walker) emit(e files.Entry, rel []string, name string) {
	if len(w.out) >= w.limit {
		w.truncated = true
		w.stopped = true
		return
	}
	w.out = append(w.out, e)
	w.emitRoot, w.emitDir, w.emitName = w.root, rel, name
}
