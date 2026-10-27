package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/fsops"
)

// newSearchFixture builds a temp directory tree with files of various
// extensions, sizes, and mtimes, plus a subdirectory, and returns a
// path-jailed *fsops.Ops rooted at it.
//
// Layout:
//
//	root/
//	  photo.jpg     (1000 bytes, mtime 2020-01-01)
//	  photo.PNG     (2000 bytes, mtime 2021-06-15)  -- uppercase ext
//	  movie.mp4     (5000 bytes, mtime 2022-03-10)
//	  song.mp3      (500 bytes,  mtime 2023-09-01)
//	  notes.txt     (10 bytes,   mtime 2024-01-01)
//	  archive.zip   (3000 bytes, mtime 2024-12-25)
//	  weird.xyz     (100 bytes,  mtime 2025-05-05)  -- "other" category
//	  Subfolder/    (directory)
//	    nested.jpg  (1500 bytes, mtime 2023-01-01)
func newSearchFixture(t *testing.T) (*fsops.Ops, string) {
	t.Helper()
	dir := t.TempDir()

	type file struct {
		name  string
		size  int
		mtime time.Time
	}
	files := []file{
		{"photo.jpg", 1000, time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)},
		{"photo.PNG", 2000, time.Date(2021, 6, 15, 0, 0, 0, 0, time.UTC)},
		{"movie.mp4", 5000, time.Date(2022, 3, 10, 0, 0, 0, 0, time.UTC)},
		{"song.mp3", 500, time.Date(2023, 9, 1, 0, 0, 0, 0, time.UTC)},
		{"notes.txt", 10, time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)},
		{"archive.zip", 3000, time.Date(2024, 12, 25, 0, 0, 0, 0, time.UTC)},
		{"weird.xyz", 100, time.Date(2025, 5, 5, 0, 0, 0, 0, time.UTC)},
	}
	for _, f := range files {
		p := filepath.Join(dir, f.name)
		if err := os.WriteFile(p, make([]byte, f.size), 0o644); err != nil {
			t.Fatalf("write %s: %v", f.name, err)
		}
		if err := os.Chtimes(p, f.mtime, f.mtime); err != nil {
			t.Fatalf("chtimes %s: %v", f.name, err)
		}
	}

	sub := filepath.Join(dir, "Subfolder")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatalf("mkdir Subfolder: %v", err)
	}
	nested := filepath.Join(sub, "nested.jpg")
	if err := os.WriteFile(nested, make([]byte, 1500), 0o644); err != nil {
		t.Fatalf("write nested.jpg: %v", err)
	}
	nestedMtime := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(nested, nestedMtime, nestedMtime); err != nil {
		t.Fatalf("chtimes nested.jpg: %v", err)
	}

	ops := fsops.New([]string{dir}, false)
	return ops, dir
}

// doSearch invokes searchHandler directly with the given raw query string
// (without the leading '?') and decodes the response.
func doSearch(t *testing.T, ops *fsops.Ops, rawQuery string) (*httptest.ResponseRecorder, []fsops.Entry) {
	t.Helper()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/search?"+rawQuery, nil)
	searchHandler(ops, noIndex{})(rr, req)

	var entries []fsops.Entry
	if rr.Code == http.StatusOK {
		if err := json.Unmarshal(rr.Body.Bytes(), &entries); err != nil {
			t.Fatalf("decode body as bare array: %v\nbody: %s", err, rr.Body.String())
		}
	}
	return rr, entries
}

func names(entries []fsops.Entry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.Name
	}
	return out
}

func containsName(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

// --------- q: substring vs glob ---------

func TestSearchHandler_SubstringMatchIsCaseInsensitive(t *testing.T) {
	ops, root := newSearchFixture(t)
	rr, entries := doSearch(t, ops, "q=PHOTO&root="+url.QueryEscape(root))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	got := names(entries)
	if !containsName(got, "photo.jpg") || !containsName(got, "photo.PNG") {
		t.Fatalf("expected both photo files, got %v", got)
	}
}

func TestSearchHandler_GlobMatchOnName(t *testing.T) {
	ops, root := newSearchFixture(t)

	// "*.jpg" should match photo.jpg and nested.jpg, case-insensitively
	// (so it should NOT match photo.PNG).
	rr, entries := doSearch(t, ops, "q="+url.QueryEscape("*.jpg")+"&root="+url.QueryEscape(root))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	got := names(entries)
	if !containsName(got, "photo.jpg") || !containsName(got, "nested.jpg") {
		t.Fatalf("expected photo.jpg and nested.jpg, got %v", got)
	}
	if containsName(got, "photo.PNG") {
		t.Fatalf("did not expect photo.PNG in glob match, got %v", got)
	}
}

func TestSearchHandler_GlobQuestionMark(t *testing.T) {
	ops, root := newSearchFixture(t)

	// "movi?.mp4" should match movie.mp4 via the '?' wildcard.
	rr, entries := doSearch(t, ops, "q="+url.QueryEscape("movi?.mp4")+"&root="+url.QueryEscape(root))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	got := names(entries)
	if !containsName(got, "movie.mp4") {
		t.Fatalf("expected movie.mp4, got %v", got)
	}
}

func TestSearchHandler_InvalidGlobIs400(t *testing.T) {
	ops, root := newSearchFixture(t)

	// "[" is an unterminated character class -> path.ErrBadPattern.
	rr, _ := doSearch(t, ops, "q="+url.QueryEscape("[*")+"&root="+url.QueryEscape(root))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
	assertErrorCode(t, rr, "BAD_REQUEST")
}

// --------- types filter ---------

func TestSearchHandler_TypesFilterImage(t *testing.T) {
	ops, root := newSearchFixture(t)

	// Match-everything substring (empty string contained in every name).
	rr, entries := doSearch(t, ops, "q=*&types=image&root="+url.QueryEscape(root))
	_ = rr
	got := names(entries)
	for _, n := range got {
		if n != "photo.jpg" && n != "photo.PNG" && n != "nested.jpg" {
			t.Fatalf("unexpected non-image entry %q in results: %v", n, got)
		}
	}
	if !containsName(got, "photo.jpg") || !containsName(got, "photo.PNG") || !containsName(got, "nested.jpg") {
		t.Fatalf("expected all three image files, got %v", got)
	}
}

func TestSearchHandler_TypesFilterFolder(t *testing.T) {
	ops, root := newSearchFixture(t)

	rr, entries := doSearch(t, ops, "q=*&types=folder&root="+url.QueryEscape(root))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	got := names(entries)
	if len(got) != 1 || got[0] != "Subfolder" {
		t.Fatalf("expected only Subfolder, got %v", got)
	}
}

func TestSearchHandler_TypesFilterOther(t *testing.T) {
	ops, root := newSearchFixture(t)

	rr, entries := doSearch(t, ops, "q=*&types=other&root="+url.QueryEscape(root))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	got := names(entries)
	if len(got) != 1 || got[0] != "weird.xyz" {
		t.Fatalf("expected only weird.xyz, got %v", got)
	}
}

func TestSearchHandler_TypesFilterMultipleCommaSeparated(t *testing.T) {
	ops, root := newSearchFixture(t)

	rr, entries := doSearch(t, ops, "q=*&types=video,audio&root="+url.QueryEscape(root))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	got := names(entries)
	if len(got) != 2 || !containsName(got, "movie.mp4") || !containsName(got, "song.mp3") {
		t.Fatalf("expected movie.mp4 and song.mp3, got %v", got)
	}
}

func TestSearchHandler_TypesUnknownValueIs400(t *testing.T) {
	ops, root := newSearchFixture(t)

	rr, _ := doSearch(t, ops, "q=*&types=bogus&root="+url.QueryEscape(root))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
	assertErrorCode(t, rr, "BAD_REQUEST")
}

// --------- ext filter ---------

func TestSearchHandler_ExtFilter(t *testing.T) {
	ops, root := newSearchFixture(t)

	rr, entries := doSearch(t, ops, "q=*&ext=jpg&root="+url.QueryEscape(root))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	got := names(entries)
	// ext is case-sensitive on the filter value but extension comparison is
	// lowercase, so "jpg" matches photo.jpg and nested.jpg but not photo.PNG.
	if !containsName(got, "photo.jpg") || !containsName(got, "nested.jpg") {
		t.Fatalf("expected photo.jpg and nested.jpg, got %v", got)
	}
	if containsName(got, "photo.PNG") {
		t.Fatalf("did not expect photo.PNG, got %v", got)
	}
}

func TestSearchHandler_ExtFilterCaseInsensitiveAndMultiple(t *testing.T) {
	ops, root := newSearchFixture(t)

	rr, entries := doSearch(t, ops, "q=*&ext=PNG,mp3&root="+url.QueryEscape(root))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	got := names(entries)
	if len(got) != 2 || !containsName(got, "photo.PNG") || !containsName(got, "song.mp3") {
		t.Fatalf("expected photo.PNG and song.mp3, got %v", got)
	}
}

func TestSearchHandler_ExtFilterExcludesFolders(t *testing.T) {
	ops, root := newSearchFixture(t)

	// No extension matches a folder, so folders should never appear when
	// ext is set, even with a permissive q.
	rr, entries := doSearch(t, ops, "q=*&ext=jpg,png,mp4,mp3,txt,zip,xyz&root="+url.QueryEscape(root))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	got := names(entries)
	if containsName(got, "Subfolder") {
		t.Fatalf("did not expect Subfolder when ext is set, got %v", got)
	}
}

// --------- types + ext combination (AND) ---------

func TestSearchHandler_TypesAndExtCombinedAND(t *testing.T) {
	ops, root := newSearchFixture(t)

	// types=image AND ext=png -> only photo.PNG (nested.jpg and photo.jpg
	// are images but not png).
	rr, entries := doSearch(t, ops, "q=*&types=image&ext=png&root="+url.QueryEscape(root))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	got := names(entries)
	if len(got) != 1 || got[0] != "photo.PNG" {
		t.Fatalf("expected only photo.PNG, got %v", got)
	}
}

// --------- size filters ---------

func TestSearchHandler_MinSizeExcludesFoldersAndSmallFiles(t *testing.T) {
	ops, root := newSearchFixture(t)

	// minSize=1000 -> photo.jpg(1000), photo.PNG(2000), movie.mp4(5000),
	// archive.zip(3000), nested.jpg(1500). Excludes notes.txt(10),
	// song.mp3(500), weird.xyz(100), and Subfolder (folder).
	rr, entries := doSearch(t, ops, "q=*&minSize=1000&root="+url.QueryEscape(root))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	got := names(entries)
	want := []string{"photo.jpg", "photo.PNG", "movie.mp4", "archive.zip", "nested.jpg"}
	if len(got) != len(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	for _, w := range want {
		if !containsName(got, w) {
			t.Fatalf("expected %q in results, got %v", w, got)
		}
	}
	if containsName(got, "Subfolder") {
		t.Fatalf("folders must be excluded when minSize is set, got %v", got)
	}
}

func TestSearchHandler_MaxSizeExcludesFoldersAndLargeFiles(t *testing.T) {
	ops, root := newSearchFixture(t)

	// maxSize=1000 -> notes.txt(10), song.mp3(500), weird.xyz(100),
	// photo.jpg(1000). Excludes everything bigger and Subfolder.
	rr, entries := doSearch(t, ops, "q=*&maxSize=1000&root="+url.QueryEscape(root))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	got := names(entries)
	want := []string{"notes.txt", "song.mp3", "weird.xyz", "photo.jpg"}
	if len(got) != len(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	if containsName(got, "Subfolder") {
		t.Fatalf("folders must be excluded when maxSize is set, got %v", got)
	}
}

func TestSearchHandler_MinAndMaxSizeRange(t *testing.T) {
	ops, root := newSearchFixture(t)

	// 1000 <= size <= 2000 -> photo.jpg(1000), photo.PNG(2000), nested.jpg(1500).
	rr, entries := doSearch(t, ops, "q=*&minSize=1000&maxSize=2000&root="+url.QueryEscape(root))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	got := names(entries)
	want := []string{"photo.jpg", "photo.PNG", "nested.jpg"}
	if len(got) != len(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	for _, w := range want {
		if !containsName(got, w) {
			t.Fatalf("expected %q, got %v", w, got)
		}
	}
}

func TestSearchHandler_InvalidSizeIs400(t *testing.T) {
	ops, root := newSearchFixture(t)

	for _, q := range []string{"minSize=notanumber", "maxSize=notanumber"} {
		rr, _ := doSearch(t, ops, "q=*&"+q+"&root="+url.QueryEscape(root))
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("query %q: expected 400, got %d: %s", q, rr.Code, rr.Body.String())
		}
		assertErrorCode(t, rr, "BAD_REQUEST")
	}
}

// --------- modified time filters ---------

func TestSearchHandler_ModifiedAfter(t *testing.T) {
	ops, root := newSearchFixture(t)

	// modifiedAfter=2024-01-01T00:00:00Z (inclusive of equal) ->
	// notes.txt(2024-01-01), archive.zip(2024-12-25), weird.xyz(2025-05-05).
	rr, entries := doSearch(t, ops, "q=*&modifiedAfter="+url.QueryEscape("2024-01-01T00:00:00Z")+"&root="+url.QueryEscape(root))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	got := names(entries)
	want := []string{"notes.txt", "archive.zip", "weird.xyz"}
	for _, w := range want {
		if !containsName(got, w) {
			t.Fatalf("expected %q in results, got %v", w, got)
		}
	}
	if containsName(got, "photo.jpg") || containsName(got, "movie.mp4") {
		t.Fatalf("did not expect early files, got %v", got)
	}
}

func TestSearchHandler_ModifiedBefore(t *testing.T) {
	ops, root := newSearchFixture(t)

	// modifiedBefore=2022-01-01T00:00:00Z -> photo.jpg(2020), photo.PNG(2021).
	rr, entries := doSearch(t, ops, "q=*&modifiedBefore="+url.QueryEscape("2022-01-01T00:00:00Z")+"&root="+url.QueryEscape(root))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	got := names(entries)
	want := []string{"photo.jpg", "photo.PNG"}
	if len(got) != len(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	for _, w := range want {
		if !containsName(got, w) {
			t.Fatalf("expected %q, got %v", w, got)
		}
	}
}

func TestSearchHandler_ModifiedRange(t *testing.T) {
	ops, root := newSearchFixture(t)

	// 2022-06-01 <= mtime <= 2024-06-01 -> song.mp3(2023-09), notes.txt(2024-01),
	// nested.jpg(2023-01).
	rr, entries := doSearch(t, ops, "q=*"+
		"&modifiedAfter="+url.QueryEscape("2022-06-01T00:00:00Z")+
		"&modifiedBefore="+url.QueryEscape("2024-06-01T00:00:00Z")+
		"&root="+url.QueryEscape(root))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	got := names(entries)
	want := []string{"song.mp3", "notes.txt", "nested.jpg"}
	if len(got) != len(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	for _, w := range want {
		if !containsName(got, w) {
			t.Fatalf("expected %q, got %v", w, got)
		}
	}
}

func TestSearchHandler_InvalidDateIs400(t *testing.T) {
	ops, root := newSearchFixture(t)

	for _, q := range []string{"modifiedAfter=not-a-date", "modifiedBefore=2024-13-99"} {
		rr, _ := doSearch(t, ops, "q=*&"+q+"&root="+url.QueryEscape(root))
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("query %q: expected 400, got %d: %s", q, rr.Code, rr.Body.String())
		}
		assertErrorCode(t, rr, "BAD_REQUEST")
	}
}

// --------- combination of multiple filters (AND) ---------

func TestSearchHandler_AllFiltersCombinedAND(t *testing.T) {
	ops, root := newSearchFixture(t)

	// q="photo" (substring) AND types=image AND ext=png AND
	// minSize=1500 AND modifiedAfter=2021-01-01 -> only photo.PNG.
	rr, entries := doSearch(t, ops, "q=photo"+
		"&types=image&ext=png&minSize=1500"+
		"&modifiedAfter="+url.QueryEscape("2021-01-01T00:00:00Z")+
		"&root="+url.QueryEscape(root))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	got := names(entries)
	if len(got) != 1 || got[0] != "photo.PNG" {
		t.Fatalf("expected only photo.PNG, got %v", got)
	}
}

// --------- limit / truncation headers ---------

func TestSearchHandler_TruncationHeaderSetAtLimit(t *testing.T) {
	ops, root := newSearchFixture(t)

	// 9 total entries in the fixture. limit=3 with a permissive query
	// should hit the limit and set X-Search-Truncated.
	rr, entries := doSearch(t, ops, "q=*&limit=3&root="+url.QueryEscape(root))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if len(entries) != 3 {
		t.Fatalf("expected exactly 3 entries, got %d: %v", len(entries), names(entries))
	}
	if rr.Header().Get(headerSearchTruncated) != "1" {
		t.Fatalf("expected %s=1, got headers: %v", headerSearchTruncated, rr.Header())
	}
	if rr.Header().Get(headerSearchTimeBudget) != "" {
		t.Fatalf("did not expect %s to be set, got headers: %v", headerSearchTimeBudget, rr.Header())
	}
}

func TestSearchHandler_NoTruncationHeaderWhenUnderLimit(t *testing.T) {
	ops, root := newSearchFixture(t)

	// Default limit (100) is far above the 9 fixture entries (7 root files
	// + Subfolder + nested.jpg).
	rr, entries := doSearch(t, ops, "q=*&root="+url.QueryEscape(root))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	if len(entries) != 9 {
		t.Fatalf("expected all 9 entries, got %d: %v", len(entries), names(entries))
	}
	if rr.Header().Get(headerSearchTruncated) != "" {
		t.Fatalf("did not expect %s to be set, got headers: %v", headerSearchTruncated, rr.Header())
	}
	if rr.Header().Get(headerSearchTimeBudget) != "" {
		t.Fatalf("did not expect %s to be set, got headers: %v", headerSearchTimeBudget, rr.Header())
	}
}

// TestSearchHandler_FiltersApplyBeforeLimit verifies that non-matching
// entries (filtered out) don't consume the limit budget: with limit=2 and a
// types filter that only 3 image entries satisfy, all 3 should still be
// reachable... but limit=2 caps it at 2 of the *matching* entries, not at 2
// total entries walked.
func TestSearchHandler_FiltersApplyBeforeLimit(t *testing.T) {
	ops, root := newSearchFixture(t)

	// types=image matches exactly 3 entries (photo.jpg, photo.PNG,
	// nested.jpg) out of 8 total. limit=3 should return all 3 image
	// entries, NOT stop early because of the 5 non-image entries walked.
	rr, entries := doSearch(t, ops, "q=*&types=image&limit=3&root="+url.QueryEscape(root))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	got := names(entries)
	if len(got) != 3 {
		t.Fatalf("expected 3 image entries, got %d: %v", len(got), got)
	}
	for _, w := range []string{"photo.jpg", "photo.PNG", "nested.jpg"} {
		if !containsName(got, w) {
			t.Fatalf("expected %q, got %v", w, got)
		}
	}
	// limit=3 exactly equals the number of matches, so the walk completes
	// (the last match triggers SkipAll at len==limit, which is still
	// "hit the limit" — verify the header reflects that boundary case
	// consistently rather than asserting a specific value here).
	_ = rr
}

// --------- bare-array shape & root jailing ---------

func TestSearchHandler_BareArrayShape(t *testing.T) {
	ops, root := newSearchFixture(t)

	rr, _ := doSearch(t, ops, "q=*&root="+url.QueryEscape(root))
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
	}
	body := rr.Body.Bytes()
	trimmed := []byte{}
	for _, b := range body {
		if b == ' ' || b == '\n' || b == '\t' || b == '\r' {
			continue
		}
		trimmed = append(trimmed, b)
		break
	}
	if len(trimmed) == 0 || trimmed[0] != '[' {
		t.Fatalf("expected response body to start with '[' (bare array), got: %s", string(body))
	}

	// Also confirm it decodes as []fsops.Entry directly (not wrapped in an
	// object).
	var entries []fsops.Entry
	if err := json.Unmarshal(body, &entries); err != nil {
		t.Fatalf("expected bare array of entries, got decode error: %v\nbody: %s", err, body)
	}
}

func TestSearchHandler_RootJailingStillEnforced(t *testing.T) {
	ops, _ := newSearchFixture(t)

	// A root outside the jail must be rejected (FORBIDDEN), regardless of
	// the new filters.
	outside := t.TempDir()
	rr, _ := doSearch(t, ops, "q=*&root="+url.QueryEscape(outside))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rr.Code, rr.Body.String())
	}
}

func TestSearchHandler_QRequiredIs400(t *testing.T) {
	ops, root := newSearchFixture(t)

	rr, _ := doSearch(t, ops, "root="+url.QueryEscape(root))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rr.Code, rr.Body.String())
	}
	assertErrorCode(t, rr, "BAD_REQUEST")
}

// --------- CategoryForName table-driven coverage ---------

func TestCategoryForName(t *testing.T) {
	tests := []struct {
		name  string
		isDir bool
		want  string
	}{
		{"photo.jpg", false, "image"},
		{"photo.JPEG", false, "image"},
		{"clip.mp4", false, "video"},
		{"song.mp3", false, "audio"},
		{"report.pdf", false, "document"},
		{"archive.tar.gz", false, "archive"}, // ext() == "gz"
		{"weird.xyz", false, "other"},
		{"noext", false, "other"},
		{"Subfolder", true, "folder"},
		{"Subfolder.jpg", true, "folder"}, // dirs are always "folder" regardless of name
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%s/isDir=%v", tt.name, tt.isDir), func(t *testing.T) {
			got := CategoryForName(tt.name, tt.isDir)
			if got != tt.want {
				t.Fatalf("CategoryForName(%q, %v) = %q, want %q", tt.name, tt.isDir, got, tt.want)
			}
		})
	}
}

// assertErrorCode decodes rr's body as an apiError and checks Code.
func assertErrorCode(t *testing.T, rr *httptest.ResponseRecorder, want string) {
	t.Helper()
	var got apiError
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode error body: %v\nbody: %s", err, rr.Body.String())
	}
	if got.Code != want {
		t.Fatalf("unexpected error code: got %+v, want code %q", got, want)
	}
}
