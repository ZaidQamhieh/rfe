// Package search is the file search of the rewritten agent: it finds files and
// folders by name (glob or substring, case-insensitive) or by the text inside
// files, below the folders of one rootfs.Set.
//
// The live walk in this package is the default and the oracle: it goes through
// rootfs only, so it never follows a symlink out of an allowed folder and
// never enters or returns the protected data folder. An optional Indexer (the
// rfe-indexd sidecar) may answer the first page of a name search faster; its
// answers are only paths, re-read through the same rootfs layer, and any
// failure falls back to the walk. Results come in depth-first order with the
// names of each folder sorted, which makes a result cursor stable.
//
// Failures match the domain errors of package api; their text carries no host
// path.
package search

import (
	"context"
	"encoding/base64"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/next/api"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/files"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/rootfs"
)

// Result limits. A page holds DefaultLimit entries unless the caller asks for
// another size, and never more than MaxLimit. A search stops at TimeBudget and
// returns what it found so far, with a cursor to continue.
const (
	DefaultLimit = 100
	MaxLimit     = 500
	TimeBudget   = 15 * time.Second
)

// Input limits: the query text and the cursor are bounded because they come
// from outside, and a walk never goes deeper than MaxDepth folders.
const (
	MaxQueryBytes  = 512
	MaxCursorBytes = 4096
	MaxDepth       = 128
)

// Events passed to Config.Note when the index could not serve a query (the
// names of protocol/sidecar.md).
const (
	EventIndexFallback = "indexd.search-fallback"
	EventIndexNotReady = "indexd.search-not-ready"
)

// cursorSeparator splits the start folder from the path inside it in a
// decoded cursor. A NUL cannot occur in a path.
const cursorSeparator = "\x00"

// relSeparator joins the names of a path inside a start folder in a cursor.
const relSeparator = "/"

// virtualLinuxDirs are pseudo-filesystem mount points and ostree internals
// that are huge and irrelevant to a search; they are skipped when met inside a
// walk, not when given as the start folder.
var virtualLinuxDirs = map[string]bool{
	"/proc":    true,
	"/sys":     true,
	"/dev":     true,
	"/sysroot": true,
}

// noisyDirNames are folder names of dependency caches that are never worth
// searching. Folders whose name starts with a dot are skipped too (see
// skipDir); hidden files are not.
var noisyDirNames = map[string]bool{
	"node_modules": true,
}

// Config configures a Service.
type Config struct {
	// Roots are the folders searched when a request names no root. Empty
	// means the user's home folder (HomeDir); a folder the Set does not allow
	// is skipped, so an empty Set finds nothing.
	Roots []string
	// Indexer, when set, serves the first page of name searches.
	Indexer Indexer
	// Budget overrides TimeBudget.
	Budget time.Duration
	// HomeDir overrides os.UserHomeDir (tests).
	HomeDir func() (string, error)
	// Note, when set, receives an Event* name each time the index could not
	// serve a query.
	Note func(event string)
}

// Result is one page of a search.
type Result struct {
	Entries []files.Entry
	// NextCursor continues the search after the last entry; nil when the
	// search finished.
	NextCursor *string
	// Truncated is true when the page is full and more matches exist.
	Truncated bool
	// TimeBudget is true when the time budget ended the search early; the
	// result is partial and NextCursor continues it. Never set together with
	// Truncated.
	TimeBudget bool
}

// Service searches inside the folders of one rootfs.Set. A per-device jail is
// expressed by the Set the caller passes (Set.Narrow), never by a flag here.
type Service struct {
	set   *rootfs.Set
	files *files.Service
	cfg   Config
}

// New returns a Service that searches inside set, which must not be nil.
func New(set *rootfs.Set, cfg Config) *Service {
	if cfg.Budget <= 0 {
		cfg.Budget = TimeBudget
	}
	if cfg.HomeDir == nil {
		cfg.HomeDir = os.UserHomeDir
	}
	return &Service{set: set, files: files.New(set), cfg: cfg}
}

// opError pairs a domain error (kind) with its cause; its text is the kind's
// alone, so it is safe to show a client.
type opError struct {
	kind  error
	cause error
}

func (e *opError) Error() string   { return e.kind.Error() }
func (e *opError) Unwrap() []error { return []error{e.kind, e.cause} }

// classify maps a rootfs or OS error to a domain error.
func classify(err error) error {
	if err == nil {
		return nil
	}
	var kind error
	switch {
	case errors.Is(err, rootfs.ErrOutsideRoots), errors.Is(err, rootfs.ErrProtected):
		kind = api.ErrForbidden
	case errors.Is(err, rootfs.ErrInvalidPath):
		kind = api.ErrBadRequest
	case errors.Is(err, fs.ErrNotExist):
		kind = api.ErrPathNotFound
	default:
		return err
	}
	return &opError{kind: kind, cause: err}
}

// clampLimit applies the default and the maximum page size.
func clampLimit(n int) int {
	if n <= 0 {
		return DefaultLimit
	}
	return min(n, MaxLimit)
}

// Search runs req. It returns the context's error when ctx ends (the time
// budget is a separate, softer limit: it ends the search with a partial
// result). With a Cursor the search continues where the earlier page stopped.
func (s *Service) Search(ctx context.Context, req Request) (*Result, error) {
	f, err := compile(req)
	if err != nil {
		return nil, err
	}
	limit := clampLimit(req.Limit)
	starts, err := s.startFolders(req.Root)
	if err != nil {
		return nil, err
	}
	var cur *cursor
	if req.Cursor != "" {
		if cur, err = decodeCursor(req.Cursor, starts); err != nil {
			return nil, err
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	budgetCtx, cancel := context.WithTimeout(ctx, s.cfg.Budget)
	defer cancel()

	var res *Result
	if s.cfg.Indexer != nil && cur == nil && !req.Content {
		res = s.searchIndexed(budgetCtx, f, starts, limit)
	}
	if res == nil {
		w := &walker{s: s, ctx: budgetCtx, f: f, limit: limit, content: req.Content}
		w.needle = []byte(f.needle)
		res = w.run(starts, cur)
	}
	// The caller's own cancellation is an error; the budget is not.
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return res, nil
}

// startFolders returns the folders to search. A named root must be usable
// (an error otherwise); the configured roots are best effort: one the Set does
// not allow, or that is gone, is left out.
func (s *Service) startFolders(root string) ([]string, error) {
	if root != "" {
		h, err := s.folder(root)
		if err != nil {
			return nil, err
		}
		return []string{h.Path()}, nil
	}
	candidates := s.cfg.Roots
	if len(candidates) == 0 {
		if home, err := s.cfg.HomeDir(); err == nil && home != "" {
			candidates = []string{home}
		}
	}
	out := make([]string, 0, len(candidates))
	for _, c := range candidates {
		if h, err := s.folder(c); err == nil {
			out = append(out, h.Path())
		}
	}
	return out, nil
}

// folder opens path as a folder inside the Set. A folder that is, or lies
// inside, the protected folder fails here; one that merely contains it is
// allowed, because the walk leaves the protected part out.
func (s *Service) folder(path string) (*rootfs.Handle, error) {
	h, err := s.set.Open(path)
	if err != nil {
		return nil, classify(err)
	}
	info, err := h.Stat()
	if err != nil {
		return nil, classify(err)
	}
	if !info.IsDir() {
		return nil, badRequest("not a folder")
	}
	return h, nil
}

// reachesProtected reports whether the symlink at path leads into the
// protected folder (a link must not reveal that it exists).
func (s *Service) reachesProtected(path string) bool {
	h, err := s.set.Open(path)
	if err != nil {
		return false
	}
	_, err = h.Stat()
	return errors.Is(err, rootfs.ErrProtected)
}

// skipDir reports whether a folder is pruned from a walk: a Linux
// pseudo-filesystem, a dot-prefixed folder or a known dependency cache.
func skipDir(path string) bool {
	if runtime.GOOS == "linux" && virtualLinuxDirs[filepath.Clean(path)] {
		return true
	}
	name := filepath.Base(path)
	return (len(name) > 1 && name[0] == '.') || noisyDirNames[name]
}

// cursor is the decoded position after which a search continues: a start
// folder and the path of the last processed entry below it.
type cursor struct {
	root    string
	rel     []string
	rootIdx int // index of root in the request's start folders
}

// encodeCursor builds the opaque cursor for the entry dir/name below root.
func encodeCursor(root string, dir []string, name string) string {
	rel := append(append([]string(nil), dir...), name)
	raw := root + cursorSeparator + strings.Join(rel, relSeparator)
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

// decodeCursor parses a cursor and checks that its start folder is one of the
// request's. A cursor is only ever compared with names, never opened as a
// path, so a forged one can at worst skip results.
func decodeCursor(s string, starts []string) (*cursor, error) {
	if len(s) > MaxCursorBytes {
		return nil, badRequest("invalid cursor")
	}
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil, badRequest("invalid cursor")
	}
	root, rel, ok := strings.Cut(string(raw), cursorSeparator)
	if !ok || rel == "" {
		return nil, badRequest("invalid cursor")
	}
	for i, st := range starts {
		if st == root {
			return &cursor{root: root, rel: strings.Split(rel, relSeparator), rootIdx: i}, nil
		}
	}
	return nil, badRequest("cursor does not belong to this search")
}

// searchIndexed answers from the indexer, one start folder at a time. It
// returns nil when the walk must answer instead: the index failed, is not
// ready, or its answer may be incomplete.
func (s *Service) searchIndexed(ctx context.Context, f *filters, starts []string, limit int) *Result {
	res := &Result{Entries: []files.Entry{}}
	var lastRoot string
	var lastDir []string
	var lastName string
	for _, root := range starts {
		need := limit - len(res.Entries) + 1
		ir, err := s.cfg.Indexer.Query(ctx, IndexQuery{Filters: f.indexFilters(), Roots: []string{root}, Limit: need})
		if err != nil || !ir.Ready {
			if ctx.Err() == nil {
				ev := EventIndexFallback
				if errors.Is(err, ErrIndexNotReady) || (err == nil && !ir.Ready) {
					ev = EventIndexNotReady
				}
				s.note(ev)
			}
			return nil
		}
		accepted := s.acceptIndexed(root, ir.Entries, f)
		// An index cut by its own budget, or entries dropped from a full
		// answer, may hide later matches: the walk is the authority then.
		if ir.Truncated && len(accepted) < need {
			s.note(EventIndexFallback)
			return nil
		}
		for _, a := range accepted {
			if len(res.Entries) >= limit {
				res.Truncated = true
				c := encodeCursor(lastRoot, lastDir, lastName)
				res.NextCursor = &c
				return res
			}
			res.Entries = append(res.Entries, a.entry)
			lastRoot, lastDir, lastName = root, a.dir, a.entry.Name
		}
	}
	return res
}

// note reports an index event when a hook is configured.
func (s *Service) note(event string) {
	if s.cfg.Note != nil {
		s.cfg.Note(event)
	}
}

// indexed is an index hit that passed validation: the entry as read now and
// the names of the folders between the start folder and it.
type indexed struct {
	entry files.Entry
	dir   []string
}

// acceptIndexed keeps the index hits that are absolute, clean paths strictly
// below root, not under a pruned folder, that still exist, and that match the
// filters when read through the jailed file layer. The index is not trusted:
// what it says only nominates paths.
func (s *Service) acceptIndexed(root string, hits []IndexEntry, f *filters) []indexed {
	out := make([]indexed, 0, len(hits))
	for _, hit := range hits {
		p := hit.Path
		if !filepath.IsAbs(p) || filepath.Clean(p) != p {
			continue
		}
		rel, err := filepath.Rel(root, p)
		if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		comps := strings.Split(rel, string(filepath.Separator))
		if underSkippedDir(root, comps[:len(comps)-1]) {
			continue
		}
		e, err := s.files.Meta(p)
		if err != nil {
			continue
		}
		if e.IsDir && skipDir(p) {
			continue
		}
		if !f.matchName(strings.ToLower(e.Name)) || !f.matchEntry(e) {
			continue
		}
		if e.IsSymlink && s.reachesProtected(p) {
			continue
		}
		out = append(out, indexed{entry: *e, dir: comps[:len(comps)-1]})
	}
	return out
}

// underSkippedDir reports whether any folder on the way from root down the
// given names is one a walk would have pruned.
func underSkippedDir(root string, dirs []string) bool {
	p := root
	for _, d := range dirs {
		p = filepath.Join(p, d)
		if skipDir(p) {
			return true
		}
	}
	return false
}
