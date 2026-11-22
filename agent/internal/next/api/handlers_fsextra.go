package api

import (
	"context"
	"io/fs"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Limits, query names and fixed words of GET /fs/recent, GET /fs/media-info and
// POST /fs/chmod.
const (
	queryRecentRoot = "root"

	// recentDefaultLimit and recentMaxLimit are the default and the ceiling of
	// the "limit" query value (openapi /fs/recent).
	recentDefaultLimit = 100
	recentMaxLimit     = 500
	// recentMaxScans is how many recursive scans may run at once; more get 429.
	recentMaxScans = 2
	// recentTimeBudget ends a scan; the answer is then marked partial.
	recentTimeBudget = 15 * time.Second
	// recentCacheTTL is how long a complete answer is reused.
	recentCacheTTL = 5 * time.Second
	// recentRetryAfterSeconds is the Retry-After of a 429 RECENT_BUSY.
	recentRetryAfterSeconds = "1"
	// cacheKeySeparator joins the parts of a cache key; NUL cannot be in a path.
	cacheKeySeparator = "\x00"

	// modeBase and modeBits are the base and bit size chmod parses its mode with.
	modeBase = 8
	modeBits = 32

	msgChmodRequired = "path and mode are required"
	msgBadMode       = "mode must be an octal number such as 644"
	msgModeTooWide   = "mode must be at most 777 (octal); setuid, setgid and sticky are not supported"
)

// msgRecentBusy is the text of a 429 RECENT_BUSY; it carries no request data.
const msgRecentBusy = "recent-file scan capacity is full; retry shortly"

// FileMediaInfo is the JSON form of what a photo or video header says (openapi
// schema MediaInfo). Every field is left out when the file does not say.
type FileMediaInfo struct {
	Width      int    `json:"width,omitempty"`
	Height     int    `json:"height,omitempty"`
	TakenAt    string `json:"takenAt,omitempty"`
	DurationMs int64  `json:"durationMs,omitempty"`
}

// RecentRequest is one recent-files query. Root is the folder the caller named
// ("" for none); Roots are the folders scanned otherwise (the caller's jail, or
// the agent's allowed roots; empty means the user's home folder). Limit is
// already within 1..recentMaxLimit.
type RecentRequest struct {
	Root  string
	Roots []string
	Limit int
}

// RecentPage is the answer of a recent-files scan: files newest first, and
// whether the time budget (or the client) ended the scan early.
type RecentPage struct {
	Entries []FileEntry
	Partial bool
}

// FsExtraService is the part of the file service that GET /fs/recent, GET
// /fs/media-info and POST /fs/chmod use. The FileService a FilesProvider
// returns for a caller also implements it; when it does not, these routes
// answer 501. Errors must be domain errors with client-safe text.
type FsExtraService interface {
	Recent(ctx context.Context, req RecentRequest) (*RecentPage, error)
	MediaInfo(path string) (*FileMediaInfo, error)
	Chmod(path string, mode fs.FileMode) (*FileEntry, error)
}

// registerFsExtraHandlers adds the handlers of GET /fs/recent, GET /fs/media-info and POST /fs/chmod.
func registerFsExtraHandlers(s *Server, m map[string]handlerFunc) {
	recent := newRecentScanner(s)
	s.addFileRoute(m, routeKey(get, "/fs/recent"), recent.serve)
	s.addFileRoute(m, routeKey(get, "/fs/media-info"), s.fsMediaInfo)
	s.addFileRoute(m, routeKey(post, "/fs/chmod"), s.fsChmod)
	registerUsageHandlers(s, m)
}

// fsExtraFor returns the extra file operations for the request's caller, or
// notYet when the configured file service does not offer them.
func (s *Server) fsExtraFor(r *http.Request) (FsExtraService, error) {
	svc, err := s.filesFor(r)
	if err != nil {
		return nil, err
	}
	x, ok := svc.(FsExtraService)
	if !ok {
		return nil, notYet
	}
	return x, nil
}

// fsMediaInfo answers GET /fs/media-info: the pixel size, capture date and
// duration read from the header of a photo or video; `{}` for a folder or a
// file of an unsupported type.
func (s *Server) fsMediaInfo(w http.ResponseWriter, r *http.Request) error {
	path, err := queryPathOf(r)
	if err != nil {
		return err
	}
	svc, err := s.fsExtraFor(r)
	if err != nil {
		return err
	}
	info, err := svc.MediaInfo(path)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, info)
	return nil
}

// fsChmod answers POST /fs/chmod: it sets the nine permission bits named by an
// octal string and answers the updated entry. Anything above the nine bits
// (setuid, setgid, sticky) is refused. Read-only mode is enforced by the route
// guard, the grant check by the route table, and the folders by the service.
func (s *Server) fsChmod(w http.ResponseWriter, r *http.Request) error {
	var req struct {
		Path string `json:"path"`
		Mode string `json:"mode"`
	}
	if err := decodeJSON(r, &req); err != nil {
		return err
	}
	if req.Path == "" || req.Mode == "" {
		return badRequest(msgChmodRequired)
	}
	parsed, err := strconv.ParseUint(req.Mode, modeBase, modeBits)
	if err != nil {
		return badRequest(msgBadMode)
	}
	if parsed > uint64(fs.ModePerm) {
		return badRequest(msgModeTooWide)
	}
	svc, err := s.fsExtraFor(r)
	if err != nil {
		return err
	}
	entry, err := svc.Chmod(req.Path, fs.FileMode(parsed))
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, entry)
	return nil
}

// recentScanner serves GET /fs/recent. It bounds the expensive recursive scans
// to recentMaxScans at a time and reuses one complete answer for
// recentCacheTTL, keyed by the caller's jail and the query so no caller can be
// shown another's folders.
type recentScanner struct {
	s     *Server
	scans chan struct{}

	mu        sync.Mutex
	cacheKey  string
	expires   time.Time
	cacheList []FileEntry
}

// newRecentScanner returns a scanner with its own admission limit and cache.
func newRecentScanner(s *Server) *recentScanner {
	return &recentScanner{s: s, scans: make(chan struct{}, recentMaxScans)}
}

// parseRecentLimit reads the "limit" query value: a missing, unparsable or
// non-positive value means the default, and a larger one is capped.
func parseRecentLimit(raw string) int {
	limit := recentDefaultLimit
	if n, err := strconv.Atoi(raw); err == nil && n > 0 {
		limit = n
	}
	return min(limit, recentMaxLimit)
}

// recentKey identifies a query for the cache: the caller's jail, the effective
// roots, the named root and the limit.
func recentKey(jail string, roots []string, root string, limit int) string {
	sorted := append([]string(nil), roots...)
	sort.Strings(sorted)
	return strings.Join(append([]string{jail, root, strconv.Itoa(limit)}, sorted...), cacheKeySeparator)
}

// cached returns a copy of the stored answer when key matches and it is fresh.
func (c *recentScanner) cached(key string) ([]FileEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cacheKey != key || !c.s.clock.Now().Before(c.expires) {
		return nil, false
	}
	return append([]FileEntry{}, c.cacheList...), true
}

// store keeps entries as the one cached answer for key.
func (c *recentScanner) store(key string, entries []FileEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cacheKey = key
	c.expires = c.s.clock.Now().Add(recentCacheTTL)
	c.cacheList = append([]FileEntry{}, entries...)
}

// serve answers GET /fs/recent with a bare JSON array, newest first. A
// partial answer carries X-Search-Time-Budget: 1 and is never cached; a scan
// refused for capacity is 429 RECENT_BUSY with Retry-After.
func (c *recentScanner) serve(w http.ResponseWriter, r *http.Request) error {
	call, err := callOf(r)
	if err != nil {
		return err
	}
	svc, err := c.s.fsExtraFor(r)
	if err != nil {
		return err
	}
	q := r.URL.Query()
	req := RecentRequest{
		Root:  strings.TrimSpace(q.Get(queryRecentRoot)),
		Roots: call.policy.Roots,
		Limit: parseRecentLimit(q.Get(queryLimit)),
	}
	jail := call.principal.JailRoot
	if jail != "" {
		req.Roots = []string{jail} // a jailed device scans its jail, not the shared roots
	}
	key := recentKey(jail, req.Roots, req.Root, req.Limit)
	if entries, ok := c.cached(key); ok {
		writeJSON(w, http.StatusOK, entries)
		return nil
	}

	select {
	case c.scans <- struct{}{}:
		defer func() { <-c.scans }()
	default:
		w.Header().Set(headerRetryAfter, recentRetryAfterSeconds)
		return &apiError{kind: ErrRecentBusy, msg: msgRecentBusy}
	}
	ctx, cancel := context.WithTimeout(r.Context(), recentTimeBudget)
	defer cancel()
	page, err := svc.Recent(ctx, req)
	if err != nil {
		return err
	}
	entries := page.Entries
	if entries == nil {
		entries = []FileEntry{}
	}
	if page.Partial {
		w.Header().Set(headerSearchTimeBudget, flagOn)
	} else {
		c.store(key, entries)
	}
	writeJSON(w, http.StatusOK, entries)
	return nil
}
