package search

import (
	"fmt"
	"net/url"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/next/api"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/files"
)

// Query parameter names of GET /search (protocol/openapi.yaml).
const (
	paramQuery          = "q"
	paramRoot           = "root"
	paramLimit          = "limit"
	paramCursor         = "cursor"
	paramContent        = "content"
	paramTypes          = "types"
	paramExt            = "ext"
	paramMinSize        = "minSize"
	paramMaxSize        = "maxSize"
	paramModifiedAfter  = "modifiedAfter"
	paramModifiedBefore = "modifiedBefore"
)

// Pseudo-categories of the types filter that are not extension based.
const (
	categoryFolder = "folder"
	categoryOther  = "other"
)

// listSeparator separates the values of the types and ext parameters.
const listSeparator = ","

// globChars are the characters that make a query a glob pattern.
const globChars = "*?"

// categoryExtensions maps each types category to the lowercase, dot-free file
// extensions that belong to it. It is the single source of truth for the types
// and ext filters and equals the table of the previous agent.
var categoryExtensions = map[string][]string{
	"image":    {"jpg", "jpeg", "png", "gif", "webp", "bmp", "heic", "heif", "svg", "avif", "tiff"},
	"video":    {"mp4", "mkv", "avi", "mov", "wmv", "flv", "webm", "m4v", "3gp", "mpg", "mpeg"},
	"audio":    {"mp3", "wav", "flac", "ogg", "m4a", "aac", "wma", "opus", "mid"},
	"document": {"pdf", "doc", "docx", "xls", "xlsx", "ppt", "pptx", "txt", "md", "odt", "ods", "odp", "rtf", "csv", "epub"},
	"archive":  {"zip", "rar", "7z", "tar", "gz", "bz2", "xz", "zst", "iso", "jar"},
}

// validCategories is the set of accepted types values, including the two
// pseudo-categories.
var validCategories = func() map[string]bool {
	set := map[string]bool{categoryFolder: true, categoryOther: true}
	for cat := range categoryExtensions {
		set[cat] = true
	}
	return set
}()

// extCategory is the reverse index of categoryExtensions.
var extCategory = func() map[string]string {
	m := make(map[string]string)
	for cat, exts := range categoryExtensions {
		for _, ext := range exts {
			m[ext] = cat
		}
	}
	return m
}()

// Request is one search. Zero values mean "not set".
type Request struct {
	// Query is matched case-insensitively against entry names (a glob when it
	// contains * or ?, otherwise a substring) or, with Content set, against
	// the text inside files.
	Query string
	// Root narrows the search to this folder; empty searches the configured
	// folders.
	Root string
	// Limit is the page size (DefaultLimit when 0, at most MaxLimit).
	Limit int
	// Cursor continues a previous result (Result.NextCursor).
	Cursor string
	// Content searches inside files instead of file names.
	Content bool

	Types          []string
	Exts           []string
	MinSize        *int64
	MaxSize        *int64
	ModifiedAfter  *time.Time
	ModifiedBefore *time.Time
}

// filters is a validated Request: lowercase query and the optional filters,
// all ANDed.
type filters struct {
	glob      string          // lowercase glob pattern, "" for a substring query
	needle    string          // lowercase substring, "" for a glob query
	types     map[string]bool // nil = no filter
	exts      map[string]bool // nil = no filter
	minSize   *int64
	maxSize   *int64
	modAfter  *time.Time
	modBefore *time.Time
}

// badRequest returns an error that the api layer maps to 400.
func badRequest(format string, args ...any) error {
	return fmt.Errorf("%w: %s", api.ErrBadRequest, fmt.Sprintf(format, args...))
}

// extOf returns the lowercase, dot-free extension of name, or "".
func extOf(name string) string {
	return strings.ToLower(strings.TrimPrefix(filepath.Ext(name), "."))
}

// CategoryForName returns the types category of a name: "folder" for a folder,
// the category of a known extension, or "other".
func CategoryForName(name string, isDir bool) string {
	if isDir {
		return categoryFolder
	}
	if cat, ok := extCategory[extOf(name)]; ok {
		return cat
	}
	return categoryOther
}

// compile validates req and builds its filters. Every failure matches
// api.ErrBadRequest.
func compile(req Request) (*filters, error) {
	q := strings.TrimSpace(req.Query)
	if q == "" {
		return nil, badRequest("q is required")
	}
	if len(q) > MaxQueryBytes {
		return nil, badRequest("q is longer than %d bytes", MaxQueryBytes)
	}
	f := &filters{minSize: req.MinSize, maxSize: req.MaxSize, modAfter: req.ModifiedAfter, modBefore: req.ModifiedBefore}
	lower := strings.ToLower(q)
	// In a content search the query is plain text, never a pattern.
	if !req.Content && strings.ContainsAny(q, globChars) {
		f.glob = lower
		// Validate eagerly so a bad pattern is refused instead of silently
		// matching nothing.
		if _, err := path.Match(f.glob, ""); err != nil {
			return nil, badRequest("invalid glob pattern in q")
		}
	} else {
		f.needle = lower
	}
	for _, raw := range req.Types {
		t := strings.ToLower(strings.TrimSpace(raw))
		if t == "" {
			continue
		}
		if !validCategories[t] {
			return nil, badRequest("invalid types value: %s", t)
		}
		if f.types == nil {
			f.types = make(map[string]bool)
		}
		f.types[t] = true
	}
	for _, raw := range req.Exts {
		e := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(raw)), ".")
		if e == "" {
			continue
		}
		if f.exts == nil {
			f.exts = make(map[string]bool)
		}
		f.exts[e] = true
	}
	return f, nil
}

// matchName reports whether a name that is already lowercase satisfies the
// query (glob or substring).
func (f *filters) matchName(lower string) bool {
	if f.glob != "" {
		ok, err := path.Match(f.glob, lower)
		return err == nil && ok
	}
	return strings.Contains(lower, f.needle)
}

// matchMeta reports whether an entry with these properties satisfies every
// filter except the query: types, ext, size bounds and modified bounds. Size
// bounds exclude folders.
func (f *filters) matchMeta(name string, isDir bool, size int64, modified time.Time) bool {
	if f.types != nil && !f.types[CategoryForName(name, isDir)] {
		return false
	}
	if f.exts != nil && (isDir || !f.exts[extOf(name)]) {
		return false
	}
	if f.minSize != nil || f.maxSize != nil {
		if isDir {
			return false
		}
		if f.minSize != nil && size < *f.minSize {
			return false
		}
		if f.maxSize != nil && size > *f.maxSize {
			return false
		}
	}
	if f.modAfter != nil && modified.Before(*f.modAfter) {
		return false
	}
	if f.modBefore != nil && modified.After(*f.modBefore) {
		return false
	}
	return true
}

// matchEntry is matchMeta for a finished Entry.
func (f *filters) matchEntry(e *files.Entry) bool {
	return f.matchMeta(e.Name, e.IsDir, e.Size, e.Modified)
}

// indexFilters converts the filters to the wire form of the indexer.
func (f *filters) indexFilters() IndexFilters {
	w := IndexFilters{Glob: f.glob, Needle: f.needle, Types: sortedKeys(f.types), Exts: sortedKeys(f.exts)}
	w.MinSize = f.minSize
	w.MaxSize = f.maxSize
	if f.modAfter != nil {
		ns := f.modAfter.UnixNano()
		w.ModAfterNs = &ns
	}
	if f.modBefore != nil {
		ns := f.modBefore.UnixNano()
		w.ModBeforeNs = &ns
	}
	return w
}

// sortedKeys returns the keys of a set in order, or nil for a nil set.
func sortedKeys(m map[string]bool) []string {
	if m == nil {
		return nil
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ParseQuery builds a Request from the query string of GET /search. A limit
// that is not a positive integer falls back to the default, as before; every
// other malformed value is a bad request.
func ParseQuery(v url.Values) (Request, error) {
	req := Request{
		Query:  strings.TrimSpace(v.Get(paramQuery)),
		Root:   v.Get(paramRoot),
		Cursor: v.Get(paramCursor),
		Types:  splitList(v.Get(paramTypes)),
		Exts:   splitList(v.Get(paramExt)),
	}
	if n, err := strconv.Atoi(v.Get(paramLimit)); err == nil && n > 0 {
		req.Limit = n
	}
	if raw := strings.TrimSpace(v.Get(paramContent)); raw != "" {
		b, err := strconv.ParseBool(raw)
		if err != nil {
			return Request{}, badRequest("invalid %s", paramContent)
		}
		req.Content = b
	}
	var err error
	if req.MinSize, err = parseSize(v, paramMinSize); err != nil {
		return Request{}, err
	}
	if req.MaxSize, err = parseSize(v, paramMaxSize); err != nil {
		return Request{}, err
	}
	if req.ModifiedAfter, err = parseTime(v, paramModifiedAfter); err != nil {
		return Request{}, err
	}
	if req.ModifiedBefore, err = parseTime(v, paramModifiedBefore); err != nil {
		return Request{}, err
	}
	return req, nil
}

// splitList splits a comma-separated parameter into its values.
func splitList(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	return strings.Split(raw, listSeparator)
}

// parseSize reads an optional integer parameter.
func parseSize(v url.Values, name string) (*int64, error) {
	raw := strings.TrimSpace(v.Get(name))
	if raw == "" {
		return nil, nil
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return nil, badRequest("invalid %s", name)
	}
	return &n, nil
}

// parseTime reads an optional RFC 3339 parameter.
func parseTime(v url.Values, name string) (*time.Time, error) {
	raw := strings.TrimSpace(v.Get(name))
	if raw == "" {
		return nil, nil
	}
	ts, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return nil, badRequest("invalid %s", name)
	}
	return &ts, nil
}
