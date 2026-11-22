package files

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/next/api"
)

// fxEpoch is the base of the modification times the recent tests set, so they
// never depend on the real clock.
var fxEpoch = time.Date(2024, time.March, 1, 12, 0, 0, 0, time.UTC)

// fxTouch writes a file and sets its modification time to fxEpoch plus age.
func fxTouch(t *testing.T, p string, age time.Duration) string {
	t.Helper()
	write(t, p, "x")
	if err := os.Chtimes(p, fxEpoch.Add(age), fxEpoch.Add(age)); err != nil {
		t.Fatal(err)
	}
	return p
}

// fxRecentNames runs Recent and returns the entry names in order.
func fxRecentNames(t *testing.T, s *Service, req RecentRequest) []string {
	t.Helper()
	res, err := s.Recent(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if res.Partial {
		t.Fatal("unexpected partial result")
	}
	out := make([]string, len(res.Entries))
	for i, e := range res.Entries {
		out[i] = e.Name
	}
	return out
}

func fxEqual(a, b []string) bool { return fmt.Sprint(a) == fmt.Sprint(b) }

func TestRecentIsNewestFirstAndSkipsFolders(t *testing.T) {
	e := newEnv(t, false)
	fxTouch(t, filepath.Join(e.root, "old.txt"), 0)
	fxTouch(t, filepath.Join(e.root, "sub", "mid.txt"), time.Hour)
	fxTouch(t, filepath.Join(e.root, "sub", "deep", "new.txt"), 2*time.Hour)
	if err := os.Mkdir(filepath.Join(e.root, "emptydir"), 0o755); err != nil {
		t.Fatal(err)
	}
	got := fxRecentNames(t, e.svc, RecentRequest{Roots: []string{e.root}})
	if want := []string{"new.txt", "mid.txt", "old.txt"}; !fxEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestRecentLimitKeepsTheNewestAndClamps(t *testing.T) {
	e := newEnv(t, false)
	const total = 7
	for i := 0; i < total; i++ {
		fxTouch(t, filepath.Join(e.root, fmt.Sprintf("f%d.txt", i)), time.Duration(i)*time.Minute)
	}
	got := fxRecentNames(t, e.svc, RecentRequest{Roots: []string{e.root}, Limit: 3})
	if want := []string{"f6.txt", "f5.txt", "f4.txt"}; !fxEqual(got, want) {
		t.Fatalf("limit 3: got %v, want %v", got, want)
	}
	// A limit of zero or less means the default, which holds all seven here.
	if got := fxRecentNames(t, e.svc, RecentRequest{Roots: []string{e.root}, Limit: -1}); len(got) != total {
		t.Fatalf("default limit: got %v", got)
	}
	// A limit above the maximum is clamped, not refused.
	if got := fxRecentNames(t, e.svc, RecentRequest{Roots: []string{e.root}, Limit: RecentMaxLimit * 10}); len(got) != total {
		t.Fatalf("huge limit: got %v", got)
	}
}

func TestRecentTiesAreOrderedByPath(t *testing.T) {
	e := newEnv(t, false)
	for _, n := range []string{"c.txt", "a.txt", "b.txt"} {
		fxTouch(t, filepath.Join(e.root, n), 0)
	}
	got := fxRecentNames(t, e.svc, RecentRequest{Roots: []string{e.root}, Limit: 2})
	if want := []string{"a.txt", "b.txt"}; !fxEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestRecentNamedRootScopesTheWalk(t *testing.T) {
	e := newEnv(t, false)
	fxTouch(t, filepath.Join(e.root, "a", "in.txt"), 0)
	fxTouch(t, filepath.Join(e.root, "b", "out.txt"), time.Hour)
	got := fxRecentNames(t, e.svc, RecentRequest{Root: filepath.Join(e.root, "a"), Roots: []string{e.root}})
	if want := []string{"in.txt"}; !fxEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestRecentWalksEveryDefaultRootAndIgnoresAForbiddenOne(t *testing.T) {
	e := newEnv(t, false)
	fxTouch(t, filepath.Join(e.root, "one.txt"), 0)
	fxTouch(t, filepath.Join(e.other, "two.txt"), time.Hour)
	forbidden := t.TempDir()
	fxTouch(t, filepath.Join(forbidden, "no.txt"), 2*time.Hour)
	got := fxRecentNames(t, e.svc, RecentRequest{Roots: []string{e.root, e.other, forbidden, filepath.Join(e.root, "gone")}})
	if want := []string{"two.txt", "one.txt"}; !fxEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestRecentWithoutRootsUsesTheHomeFolder(t *testing.T) {
	e := newEnv(t, false)
	fxTouch(t, filepath.Join(e.root, "home.txt"), 0)
	home := func() (string, error) { return e.root, nil }
	if got := fxRecentNames(t, e.svc, RecentRequest{HomeDir: home}); !fxEqual(got, []string{"home.txt"}) {
		t.Fatalf("home: got %v", got)
	}
	// A home folder the Set does not allow gives an empty list, not an error.
	elsewhere := func() (string, error) { return t.TempDir(), nil }
	if got := fxRecentNames(t, e.svc, RecentRequest{HomeDir: elsewhere}); len(got) != 0 {
		t.Fatalf("home outside roots: got %v", got)
	}
}

func TestRecentPrunesHiddenAndCacheFoldersButNotHiddenFiles(t *testing.T) {
	e := newEnv(t, false)
	fxTouch(t, filepath.Join(e.root, ".cache", "c.txt"), time.Hour)
	fxTouch(t, filepath.Join(e.root, "node_modules", "m.js"), time.Hour)
	fxTouch(t, filepath.Join(e.root, ".hidden-file"), 0)
	fxTouch(t, filepath.Join(e.root, "keep", "k.txt"), 0)
	got := fxRecentNames(t, e.svc, RecentRequest{Roots: []string{e.root}})
	if len(got) != 2 {
		t.Fatalf("got %v, want the hidden file and k.txt only", got)
	}
}

func TestRecentDoesNotFollowSymlinksOrRevealProtectedLinks(t *testing.T) {
	e := newEnv(t, true)
	outside := t.TempDir()
	fxTouch(t, filepath.Join(outside, "secret-outside.txt"), 5*time.Hour)
	fxTouch(t, filepath.Join(e.root, "plain.txt"), 0)
	fxTouch(t, filepath.Join(e.root, "realdir", "inner.txt"), -time.Hour)
	fxSymlink(t, "realdir", filepath.Join(e.root, "dirlink"))
	fxSymlink(t, outside, filepath.Join(e.root, "escapedirlink"))
	fxSymlink(t, filepath.Join(e.root, dataDirName, "secret.key"), filepath.Join(e.root, "keylink"))
	fxSymlink(t, filepath.Join(outside, "secret-outside.txt"), filepath.Join(e.root, "filelink"))

	res, err := e.svc.Recent(context.Background(), RecentRequest{Roots: []string{e.root}})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, en := range res.Entries {
		got[en.Name] = true
	}
	// Absent: "secret.key" (protected), "keylink" (leads into the protected
	// folder), "dirlink" (a link to a folder) and "secret-outside.txt" (only
	// reachable through a link out of the roots). Links that cannot be resolved
	// inside the roots are listed as themselves, never opened.
	want := map[string]bool{"plain.txt": true, "inner.txt": true, "filelink": true, "escapedirlink": true}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestRecentErrorsOfANamedRoot(t *testing.T) {
	e := newEnv(t, true)
	file := write(t, filepath.Join(e.root, "a.txt"), "x")
	cases := []struct {
		name string
		root string
		want error
	}{
		{"missing", filepath.Join(e.root, "nope"), api.ErrPathNotFound},
		{"a file", file, api.ErrBadRequest},
		{"outside the roots", t.TempDir(), api.ErrForbidden},
		{"the protected folder", filepath.Join(e.root, dataDirName), api.ErrForbidden},
		{"relative", "sub", api.ErrBadRequest},
	}
	for _, c := range cases {
		if _, err := e.svc.Recent(context.Background(), RecentRequest{Root: c.root}); !errors.Is(err, c.want) {
			t.Errorf("%s: err = %v, want %v", c.name, err, c.want)
		}
	}
}

func TestRecentStopsWhenTheContextEnds(t *testing.T) {
	e := newEnv(t, false)
	fxTouch(t, filepath.Join(e.root, "a.txt"), 0)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res, err := e.svc.Recent(ctx, RecentRequest{Roots: []string{e.root}})
	if err != nil || !res.Partial {
		t.Fatalf("res = %+v, err = %v; want a partial result", res, err)
	}
	if res.Entries == nil {
		t.Fatal("Entries must be an empty slice, not nil")
	}
}

func TestRecentInsideANarrowedSetCannotSeeOutsideIt(t *testing.T) {
	e := newEnv(t, false)
	fxTouch(t, filepath.Join(e.root, "jail", "in.txt"), 0)
	fxTouch(t, filepath.Join(e.root, "out.txt"), time.Hour)
	jail := filepath.Join(e.root, "jail")
	narrow, err := e.set.Narrow(jail)
	if err != nil {
		t.Fatal(err)
	}
	defer narrow.Close()
	s := New(narrow)
	if got := fxRecentNames(t, s, RecentRequest{Roots: []string{jail}}); !fxEqual(got, []string{"in.txt"}) {
		t.Fatalf("jailed: got %v", got)
	}
	if _, err := s.Recent(context.Background(), RecentRequest{Root: e.root}); !errors.Is(err, api.ErrForbidden) {
		t.Fatalf("root above the jail: err = %v, want api.ErrForbidden", err)
	}
}

func TestPrunedDir(t *testing.T) {
	for path, want := range map[string]bool{
		filepath.Join("x", ".git"):         true,
		filepath.Join("x", "node_modules"): true,
		filepath.Join("x", "photos"):       false,
		filepath.Join("x", "a.b"):          false,
		string(filepath.Separator):         false,
	} {
		if got := PrunedDir(path); got != want {
			t.Errorf("PrunedDir(%q) = %v, want %v", path, got, want)
		}
	}
}
