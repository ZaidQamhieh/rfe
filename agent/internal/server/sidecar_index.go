package server

import (
	"container/heap"
	"context"
	"io/fs"
	"log"
	"mime"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/fsops"
	"github.com/ZaidQamhieh/rfe/agent/internal/sidecar"
	"github.com/ZaidQamhieh/rfe/agent/internal/thumbs"
)

// indexBackend is what searchHandler needs from a search index: the rfe-indexd sidecar or noIndex
// sidecar behind sidecarIndex.
type indexBackend interface {
	query(filters *searchFilters, roots []string, limit int) (results []fsops.Entry, truncated bool, ok bool)
}

// indexRebuildInterval is how often the sidecar index is re-walked when live updates are not available.
const indexRebuildInterval = 5 * time.Minute

// maxIndexDutyCycle bounds the share of wall-clock time re-walks may consume: a walk that took longer than the
// interval waits ten times its own duration, so cost scales with the tree instead of the clock.
const maxIndexDutyCycle = 10

// Limits handed to rfe-indexd for one snapshot. The byte ceiling stops unusually long names and paths from
// defeating the entry cap.
const (
	indexMaxEntries        = 2_000_000
	indexMaxEstimatedBytes = 128 << 20
)

// newIndexBackend picks the search index: rfe-indexd when enabled and installed, else none (live walks).
func newIndexBackend(ops *fsops.Ops) indexBackend {
	if sidecarEnabled("indexd") {
		if cfg, ok := sidecarConfig("rfe-indexd"); ok {
			log.Printf("search index: using sidecar %s", cfg.Path)
			return newSidecarIndex(ops, cfg)
		}
		log.Printf("search index: RFE_SIDECARS enables indexd but rfe-indexd was not found; searching by live walk")
	}
	warnSidecarMissing("indexd", "search")
	return noIndex{}
}

// warnSidecarMissing says once at startup that a component runs in-process because no verified sidecar came with
// this install (a `go build` or a hand-copied agent), unless the owner turned sidecars off on purpose.
func warnSidecarMissing(name, what string) {
	if raw := os.Getenv("RFE_SIDECARS"); raw != "" && !sidecarEnabled(name) {
		return
	}
	if _, ok, _ := locateSidecar("rfe-" + name); ok {
		return
	}
	log.Printf("WARNING: rfe-%s is not installed next to the agent: %s runs in-process. Install the packaged release (tools/package-agent.sh) to use the sidecar", name, what)
}

// sidecarIndex keeps the index in rfe-indexd and answers queries from it. While the sidecar is down or has no
// snapshot yet, query reports ok=false and the handler walks live, exactly as before the first Go index build.
type sidecarIndex struct {
	ops  *fsops.Ops
	sup  *sidecar.Supervisor
	kick chan struct{}

	// builtMu guards built: the roots of the last finished index.build. Coverage is judged against these, not
	// the live jail roots, which the owner can change while the index still holds the old ones.
	builtMu sync.Mutex
	built   []string
}

func newSidecarIndex(ops *fsops.Ops, cfg sidecar.Config) *sidecarIndex {
	s := &sidecarIndex{ops: ops, kick: make(chan struct{}, 1)}
	s.sup = sidecar.NewSupervisor(cfg, func(*sidecar.Client) { s.rebuildSoon() })
	s.sup.Run()
	go s.loop()
	return s
}

func (s *sidecarIndex) rebuildSoon() {
	select {
	case s.kick <- struct{}{}:
	default:
	}
}

type buildReq struct {
	Roots      []string `json:"roots"`
	MaxEntries int      `json:"maxEntries"`
	MaxBytes   int64    `json:"maxBytes"`
}

type buildResp struct {
	Entries   int  `json:"entries"`
	Truncated bool `json:"truncated"`
}

// indexLiveReconcileInterval is how often a sidecar that watches the file system still re-walks everything. The
// walk is the authority that catches whatever an event missed; with live updates it is a safety net, not the
// way changes arrive.
const indexLiveReconcileInterval = 30 * time.Minute

type liveStats struct {
	Watching bool   `json:"watching"`
	Watches  int    `json:"watches"`
	Error    string `json:"error"`
}

type statsResp struct {
	Ready bool      `json:"ready"`
	Live  liveStats `json:"live"`
}

// watching reports whether the sidecar has a watch on every directory, retrying briefly because the watches go in
// right after the build answers.
func watching(c *sidecar.Client) (liveStats, bool) {
	var last liveStats
	for i := 0; i < 40; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		var st statsResp
		err := c.Call(ctx, "index.stats", struct{}{}, &st)
		cancel()
		if err != nil {
			return last, false
		}
		last = st.Live
		if st.Live.Watching {
			return last, true
		}
		if st.Live.Error != "" {
			return last, false
		}
		time.Sleep(500 * time.Millisecond)
	}
	return last, false
}

func (s *sidecarIndex) loop() {
	for {
		wait := time.Minute
		if c, err := s.sup.Client(); err == nil {
			started := time.Now()
			wait = indexRebuildInterval
			if err := s.build(c); err != nil {
				log.Printf("search index (sidecar): build failed: %v", err)
				wait = 30 * time.Second
			} else {
				if live, ok := watching(c); ok {
					wait = indexLiveReconcileInterval
					log.Printf("search index (sidecar): live updates on (%d watches), full re-walk every %v", live.Watches, wait)
				} else if live.Error != "" {
					log.Printf("search index (sidecar): live updates off (%s), full re-walk every %v", live.Error, wait)
				}
				if backoff := time.Since(started) * maxIndexDutyCycle; backoff > wait {
					wait = backoff
				}
			}
		}
		select {
		case <-time.After(wait):
		case <-s.kick:
		}
	}
}

// indexRoots are the roots the sidecar index is built over: the jail roots, else the user's home directory.
func (s *sidecarIndex) indexRoots() []string {
	roots := s.ops.Roots()
	if len(roots) == 0 {
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			roots = []string{home}
		}
	}
	return roots
}

// indexCovers reports whether the index can answer a search under root: root is inside an indexed root and no
// directory on the way down is one the index walk prunes (hidden, node_modules, pseudo-filesystems).
func indexCovers(indexed []string, root string) bool {
	root = filepath.Clean(root)
	for _, base := range indexed {
		base = filepath.Clean(base)
		// Exact, case-sensitive comparison on purpose: the index matches paths byte for byte.
		rel := ""
		switch {
		case root == base:
			rel = "."
		case strings.HasPrefix(root, strings.TrimRight(base, string(filepath.Separator))+string(filepath.Separator)):
			rel = root[len(strings.TrimRight(base, string(filepath.Separator)))+1:]
		default:
			continue
		}
		cur, ok := base, true
		if rel != "." {
			for _, part := range strings.Split(rel, string(filepath.Separator)) {
				cur = filepath.Join(cur, part)
				if shouldSkipVirtualDir(cur) {
					ok = false
					break
				}
			}
		}
		if ok {
			return true
		}
	}
	return false
}

func (s *sidecarIndex) build(c *sidecar.Client) error {
	roots := s.indexRoots()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	var resp buildResp
	if err := c.Call(ctx, "index.build", buildReq{Roots: roots, MaxEntries: indexMaxEntries, MaxBytes: indexMaxEstimatedBytes}, &resp); err != nil {
		return err
	}
	s.builtMu.Lock()
	s.built = roots
	s.builtMu.Unlock()
	if resp.Truncated {
		log.Printf("search index (sidecar): truncated at %d entries; remaining files are not searchable", resp.Entries)
	}
	return nil
}

type wireFilters struct {
	Glob        string   `json:"glob"`
	Needle      string   `json:"needle"`
	Types       []string `json:"types"`
	Exts        []string `json:"exts"`
	MinSize     *int64   `json:"minSize"`
	MaxSize     *int64   `json:"maxSize"`
	ModAfterNs  *int64   `json:"modAfterNs"`
	ModBeforeNs *int64   `json:"modBeforeNs"`
}

func sortedKeys(m map[string]bool) []string {
	if m == nil {
		return nil
	}
	out := make([]string, 0, len(m))
	for k, ok := range m {
		if ok {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

func toWireFilters(f *searchFilters) wireFilters {
	w := wireFilters{Glob: f.glob, Needle: f.needle, Types: sortedKeys(f.types), Exts: sortedKeys(f.exts)}
	if f.types != nil && w.Types == nil {
		w.Types = []string{}
	}
	if f.exts != nil && w.Exts == nil {
		w.Exts = []string{}
	}
	if f.hasMinSize {
		v := f.minSize
		w.MinSize = &v
	}
	if f.hasMaxSize {
		v := f.maxSize
		w.MaxSize = &v
	}
	if f.hasModAfter {
		v := f.modAfter.UnixNano()
		w.ModAfterNs = &v
	}
	if f.hasModBefore {
		v := f.modBefore.UnixNano()
		w.ModBeforeNs = &v
	}
	return w
}

type wireEntry struct {
	Path          string `json:"path"`
	Size          int64  `json:"size"`
	MtimeNs       int64  `json:"mtimeNs"`
	CtimeNs       int64  `json:"ctimeNs"`
	Mode          uint32 `json:"mode"`
	IsDir         bool   `json:"isDir"`
	IsSymlink     bool   `json:"isSymlink"`
	SymlinkTarget string `json:"symlinkTarget"`
}

func (w *wireEntry) toEntry() fsops.Entry {
	e := fsops.Entry{
		Name:          filepath.Base(w.Path),
		Path:          w.Path,
		IsDir:         w.IsDir,
		Size:          w.Size,
		Mode:          fs.FileMode(w.Mode).String(),
		Modified:      time.Unix(0, w.MtimeNs),
		IsSymlink:     w.IsSymlink,
		SymlinkTarget: w.SymlinkTarget,
	}
	if w.CtimeNs != 0 {
		e.Created = time.Unix(0, w.CtimeNs)
	}
	if !w.IsDir {
		// Same as fsops' no-sniff entries: unknown extensions are octet-stream, never empty.
		e.MimeType = mime.TypeByExtension(filepath.Ext(w.Path))
		if e.MimeType == "" {
			e.MimeType = "application/octet-stream"
		}
	}
	return e
}

// acceptSidecarPath is the agent's own jail check on a path a sidecar returned: absolute, already clean, and
// inside one of the roots this request is allowed to see. Anything else is dropped, whatever the sidecar says.
func acceptSidecarPath(p string, scopes []rootScope) bool {
	return filepath.IsAbs(p) && filepath.Clean(p) == p && underAnyRootScopes(p, scopes)
}

type queryReq struct {
	Filters wireFilters `json:"filters"`
	Roots   []string    `json:"roots"`
	Limit   int         `json:"limit"`
}

type queryResp struct {
	Ready     bool        `json:"ready"`
	Entries   []wireEntry `json:"entries"`
	Truncated bool        `json:"truncated"`
}

func (s *sidecarIndex) query(filters *searchFilters, roots []string, limit int) ([]fsops.Entry, bool, bool) {
	// A ready index that does not cover a root would answer [] for it; walk live instead.
	s.builtMu.Lock()
	indexed := s.built
	s.builtMu.Unlock()
	for _, r := range roots {
		if !indexCovers(indexed, r) {
			sidecar.Note("indexd.search-uncovered")
			return nil, false, false
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), searchTimeBudget)
	defer cancel()
	var resp queryResp
	if err := s.sup.Call(ctx, "index.query", queryReq{Filters: toWireFilters(filters), Roots: roots, Limit: limit}, &resp); err != nil {
		sidecar.Note("indexd.search-fallback")
		return nil, false, false
	}
	if !resp.Ready {
		sidecar.Note("indexd.search-not-ready")
		return nil, false, false
	}
	scopes := prepareRootScopes(roots)
	out := make([]fsops.Entry, 0, len(resp.Entries))
	for i := range resp.Entries {
		if !acceptSidecarPath(resp.Entries[i].Path, scopes) {
			continue
		}
		out = append(out, resp.Entries[i].toEntry())
		if len(out) >= limit {
			break
		}
	}
	return out, resp.Truncated, true
}

type recentsReq struct {
	Roots    []string `json:"roots"`
	Limit    int      `json:"limit"`
	BudgetMs int64    `json:"budgetMs"`
}

type recentsResp struct {
	Entries []wireEntry `json:"entries"`
	Partial bool        `json:"partial"`
	// Ready is set by recents.index only: false means the live index could not answer exactly and the caller walks.
	Ready bool `json:"ready"`
}

// sidecarRecentWalker returns a recent-files walker that asks rfe-indexd and falls back to the Go walk when the
// sidecar is unavailable or misbehaves.
func sidecarRecentWalker(sup *sidecar.Supervisor) func(context.Context, *fsops.Ops, string, int, *recentHeap) {
	return func(ctx context.Context, ops *fsops.Ops, root string, limit int, h *recentHeap) {
		budget := 15 * time.Second
		if dl, ok := ctx.Deadline(); ok {
			budget = time.Until(dl)
		}
		req := recentsReq{Roots: []string{root}, Limit: limit, BudgetMs: budget.Milliseconds()}
		var resp recentsResp
		// The live index answers from memory in milliseconds; it declines (ready=false) when it is missing, stale-able
		// (watcher unhealthy), cut by its budget, or does not cover the root, and then the sidecar walks as before.
		err := sup.Call(ctx, "recents.index", req, &resp)
		if err != nil || !resp.Ready {
			resp = recentsResp{}
			err = sup.Call(ctx, "recents.scan", req, &resp)
		}
		if err != nil {
			if ctx.Err() == nil {
				sidecar.Note("indexd.recents-fallback")
				walkForRecentWithOps(ctx, ops, root, limit, h)
			}
			return
		}
		scopes := prepareRootScopes([]string{root})
		for i := range resp.Entries {
			w := &resp.Entries[i]
			// A symlink to a directory is listed with isDir=true, as the Go walk lists it; real directories never arrive.
			if !acceptSidecarPath(w.Path, scopes) {
				continue
			}
			e := w.toEntry()
			if h.Len() < limit {
				heap.Push(h, e)
			} else if e.Modified.After((*h)[0].Modified) {
				(*h)[0] = e
				heap.Fix(h, 0)
			}
		}
		if resp.Partial {
			// The sidecar stopped at the same deadline the handler holds; wait for it so the handler reports the
			// time budget exactly as it does for a slow Go walk.
			<-ctx.Done()
		}
	}
}

// recentWalker returns the walker for /fs/recent: the sidecar's when the search index runs on rfe-indexd (one
// shared process), else the Go walk.
func recentWalker(idx indexBackend) func(context.Context, *fsops.Ops, string, int, *recentHeap) {
	if si, ok := idx.(*sidecarIndex); ok {
		return sidecarRecentWalker(si.sup)
	}
	return walkForRecentWithOps
}

// useThumbSidecar lets the thumbnail renderer try the sandboxed rfe-thumbd first when it is enabled and installed;
// the renderer decodes in-process whenever the sidecar cannot or is not running.
func useThumbSidecar(r *thumbs.Renderer) {
	if !sidecarEnabled("thumbd") {
		warnSidecarMissing("thumbd", "thumbnail decoding")
		return
	}
	cfg, ok := sidecarConfig("rfe-thumbd")
	if !ok {
		log.Printf("thumbnails: RFE_SIDECARS enables thumbd but rfe-thumbd was not found; using the built-in renderer")
		return
	}
	log.Printf("thumbnails: using sidecar %s", cfg.Path)
	sup := sidecar.NewSupervisor(cfg, nil)
	sup.Run()
	r.UseRemote(thumbs.SidecarRemote{Sup: sup})
}
