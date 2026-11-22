package api_test

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/next/api"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/apps"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/authz"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/files"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/rootfs"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/system"
)

// Values the fs-extra tests use.
const (
	xPNGWidth, xPNGHeight = 7, 5
	xStartMode            = fs.FileMode(0o644)
	xNewMode              = fs.FileMode(0o600)
	xBusyScans            = 2 // the number of scans the handler admits at once
	xFixedRemote          = "10.8.0.1:4000"
	xBudgetHeader         = "X-Search-Time-Budget"
)

// xClock is a settable clock.
type xClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *xClock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *xClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// xAdapter serves api.FileService and api.FsExtraService from a files.Service;
// recentHook, when set, replaces the real recent scan.
type xAdapter struct {
	filesAdapter
	recentHook func(ctx context.Context, req api.RecentRequest) (*api.RecentPage, error)
}

func (a xAdapter) Recent(ctx context.Context, req api.RecentRequest) (*api.RecentPage, error) {
	if a.recentHook != nil {
		return a.recentHook(ctx, req)
	}
	res, err := a.Service.Recent(ctx, files.RecentRequest{Root: req.Root, Roots: req.Roots, Limit: req.Limit})
	if err != nil {
		return nil, err
	}
	page := &api.RecentPage{Partial: res.Partial}
	for _, e := range res.Entries {
		page.Entries = append(page.Entries, api.FileEntry(e))
	}
	return page, nil
}

func (a xAdapter) MediaInfo(p string) (*api.FileMediaInfo, error) {
	m, err := a.Service.MediaInfo(p)
	if err != nil {
		return nil, err
	}
	out := api.FileMediaInfo(*m)
	return &out, nil
}

func (a xAdapter) Chmod(p string, mode fs.FileMode) (*api.FileEntry, error) {
	return entryOf(a.Service.Chmod(p, mode))
}

// xProvider gives each caller a service over the whole set, or over a set
// narrowed to its jail, like the real provider.
type xProvider struct {
	t          *testing.T
	set        *rootfs.Set
	recentHook func(ctx context.Context, req api.RecentRequest) (*api.RecentPage, error)
}

func (p *xProvider) For(pr authz.Principal, _ authz.Policy) (api.FileService, error) {
	set := p.set
	if pr.JailRoot != "" {
		n, err := p.set.Narrow(pr.JailRoot)
		if err != nil {
			return nil, err
		}
		p.t.Cleanup(func() { _ = n.Close() })
		set = n
	}
	return xAdapter{filesAdapter{files.New(set)}, p.recentHook}, nil
}

// xRig is the HTTP stack with the fs-extra routes served by real services.
type xRig struct {
	*rig
	root    string
	admin   string
	adminID string
	clock   *xClock
}

// newXRig builds the stack over a fresh root folder and an admin device.
func newXRig(t *testing.T, hook func(context.Context, api.RecentRequest) (*api.RecentPage, error)) *xRig {
	t.Helper()
	r := newRig(t, rigOptions{})
	root := t.TempDir()
	set, err := rootfs.New([]string{root}, rootfs.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = set.Close() })
	sys, err := system.New(system.Config{
		Version: testVersion, APIVersion: testAPIVer, Started: startedAt, Fingerprint: testFingerprint,
		Port: "8765", Settings: r.set, Disk: fakeDisk{}, Network: fakeNet{}, Counts: r.db, Host: fakeHost{},
	})
	if err != nil {
		t.Fatal(err)
	}
	clock := &xClock{t: time.Date(2024, time.March, 1, 12, 0, 0, 0, time.UTC)}
	h, err := api.New(api.Config{
		Pairing: panicPairing{r.svc}, Devices: r.db, System: sys, Policy: r.set, Logger: r.logs,
		Apps:  apps.New(apps.Config{Lister: fakeLister{}, Launcher: r.launcher}),
		Files: &xProvider{t: t, set: set, recentHook: hook}, Clock: clock,
	})
	if err != nil {
		t.Fatal(err)
	}
	r.h = h
	tok, id := r.registerAdmin(newPhone("admin-1", 1))
	return &xRig{rig: r, root: root, admin: tok, adminID: id, clock: clock}
}

// put writes a file below the root with the given age relative to the clock's start.
func (x *xRig) put(name string, age time.Duration) string {
	x.t.Helper()
	p := filepath.Join(x.root, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		x.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("x"), xStartMode); err != nil {
		x.t.Fatal(err)
	}
	when := x.clock.Now().Add(age)
	if err := os.Chtimes(p, when, when); err != nil {
		x.t.Fatal(err)
	}
	return p
}

// recentNames asks GET /fs/recent and returns the entry names in order.
func (x *xRig) recentNames(token, query string) []string {
	x.t.Helper()
	res := x.do("GET", "/fs/recent"+query, token, nil)
	res.wantStatus(x.t, 200)
	var entries []struct{ Name string }
	res.json(x.t, &entries)
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.Name
	}
	return out
}

func xPerm(t *testing.T, p string) fs.FileMode {
	t.Helper()
	info, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode().Perm()
}

func TestMediaInfoRoute(t *testing.T) {
	x := newXRig(t, nil)
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, xPNGWidth, xPNGHeight))); err != nil {
		t.Fatal(err)
	}
	pic := filepath.Join(x.root, "a.png")
	if err := os.WriteFile(pic, buf.Bytes(), xStartMode); err != nil {
		t.Fatal(err)
	}
	text := x.put("a.txt", 0)

	var got struct{ Width, Height int }
	res := x.do("GET", "/fs/media-info"+q("path", pic), x.admin, nil)
	res.wantStatus(t, 200)
	res.json(t, &got)
	if got.Width != xPNGWidth || got.Height != xPNGHeight {
		t.Fatalf("png = %s", res.Body.String())
	}
	for _, p := range []string{text, x.root} {
		res = x.do("GET", "/fs/media-info"+q("path", p), x.admin, nil)
		if res.Code != 200 || strings.TrimSpace(res.Body.String()) != "{}" {
			t.Fatalf("%s = %d %s, want {}", p, res.Code, res.Body.String())
		}
	}
}

func TestMediaInfoRouteErrors(t *testing.T) {
	x := newXRig(t, nil)
	outside := filepath.Join(t.TempDir(), "o.png")
	if err := os.WriteFile(outside, []byte("x"), xStartMode); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(x.root, "escape.png")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	for name, c := range map[string]struct {
		query  string
		status int
		code   string
	}{
		"no path":        {"", 400, api.CodeBadRequest},
		"empty path":     {q("path", ""), 400, api.CodeBadRequest},
		"relative path":  {q("path", "a.png"), 400, api.CodeBadRequest},
		"missing":        {q("path", filepath.Join(x.root, "nope.png")), 404, api.CodePathNotFound},
		"outside":        {q("path", outside), 403, authz.CodeForbidden},
		"symlink escape": {q("path", filepath.Join(x.root, "escape.png")), 403, authz.CodeForbidden},
	} {
		res := x.do("GET", "/fs/media-info"+c.query, x.admin, nil)
		if res.Code != c.status {
			t.Errorf("%s: %d %s, want %d", name, res.Code, res.Body.String(), c.status)
			continue
		}
		if e := res.errBody(t); e.Code != c.code {
			t.Errorf("%s: code %s, want %s", name, e.Code, c.code)
		}
	}
}

func TestChmodRoute(t *testing.T) {
	x := newXRig(t, nil)
	p := x.put("a.txt", 0)
	res := x.do("POST", "/fs/chmod", x.admin, map[string]string{"path": p, "mode": "600"})
	res.wantStatus(t, 200)
	var e struct{ Path, Mode string }
	res.json(t, &e)
	if e.Path != p || e.Mode != xNewMode.String() || xPerm(t, p) != xNewMode {
		t.Fatalf("entry = %+v perm %v", e, xPerm(t, p))
	}
	// A leading zero (the openapi example "0755") is the same octal number.
	x.do("POST", "/fs/chmod", x.admin, map[string]string{"path": p, "mode": "0644"}).wantStatus(t, 200)
	if xPerm(t, p) != xStartMode {
		t.Fatalf("perm = %v", xPerm(t, p))
	}
}

func TestChmodRouteBadRequests(t *testing.T) {
	x := newXRig(t, nil)
	p := x.put("a.txt", 0)
	for name, body := range map[string]any{
		"no mode":         map[string]string{"path": p},
		"no path":         map[string]string{"mode": "600"},
		"not octal":       map[string]string{"path": p, "mode": "9"},
		"not a number":    map[string]string{"path": p, "mode": "rw"},
		"negative":        map[string]string{"path": p, "mode": "-1"},
		"setuid":          map[string]string{"path": p, "mode": "4755"},
		"above the range": map[string]string{"path": p, "mode": "1000"},
		"not json":        "{",
		"wrong type":      `{"path":1,"mode":2}`,
	} {
		x.do("POST", "/fs/chmod", x.admin, body).wantError(t, 400, api.CodeBadRequest)
		if xPerm(t, p) != xStartMode {
			t.Fatalf("%s: a refused request changed the file to %v", name, xPerm(t, p))
		}
	}
}

func TestChmodRouteJailMissingAndSymlink(t *testing.T) {
	x := newXRig(t, nil)
	outside := filepath.Join(t.TempDir(), "o.txt")
	if err := os.WriteFile(outside, []byte("x"), xStartMode); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(x.root, "escape")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	chmod := func(path string) result {
		return x.do("POST", "/fs/chmod", x.admin, map[string]string{"path": path, "mode": "600"})
	}
	chmod(outside).wantError(t, 403, authz.CodeForbidden)
	chmod(filepath.Join(x.root, "escape")).wantError(t, 403, authz.CodeForbidden)
	chmod(filepath.Join(x.root, "nope")).wantError(t, 404, api.CodePathNotFound)
	if xPerm(t, outside) != xStartMode {
		t.Fatalf("a file outside the roots was changed to %v", xPerm(t, outside))
	}

	// A device jailed to a sub-folder cannot reach the rest of the root.
	p := x.put("elsewhere.txt", 0)
	x.put("jail/in.txt", 0)
	if err := x.db.SetDeviceJail(x.adminID, filepath.Join(x.root, "jail")); err != nil {
		t.Fatal(err)
	}
	chmod(p).wantError(t, 403, authz.CodeForbidden)
	chmod(filepath.Join(x.root, "jail", "in.txt")).wantStatus(t, 200)
	if xPerm(t, p) != xStartMode {
		t.Fatalf("a file outside the jail was changed to %v", xPerm(t, p))
	}
}

func TestChmodRouteGrantAndReadOnly(t *testing.T) {
	x := newXRig(t, nil)
	p := x.put("a.txt", 0)
	body := map[string]string{"path": p, "mode": "600"}

	// A browse-only device is refused by the route table before the handler.
	tok, _ := x.pairDevice(newPhone("phone-1", 2))
	x.do("POST", "/fs/chmod", tok, body).wantError(t, 403, authz.CodeCapabilityDenied)

	x.set.setReadOnly(true)
	x.do("POST", "/fs/chmod", x.admin, body).wantError(t, 403, authz.CodeReadOnly)
	if xPerm(t, p) != xStartMode {
		t.Fatalf("read-only mode let chmod change the file to %v", xPerm(t, p))
	}
	// Reads stay open in read-only mode.
	x.do("GET", "/fs/media-info"+q("path", p), x.admin, nil).wantStatus(t, 200)
}

func TestRecentRoute(t *testing.T) {
	x := newXRig(t, nil)
	x.put("old.txt", 0)
	x.put("sub/new.txt", time.Hour)
	x.put("sub/mid.txt", time.Minute)
	if err := os.Mkdir(filepath.Join(x.root, "emptydir"), 0o755); err != nil {
		t.Fatal(err)
	}
	// The scan starts at the policy roots; with none configured it uses the
	// named root.
	root := q("root", x.root)
	if got := strings.Join(x.recentNames(x.admin, root), ","); got != "new.txt,mid.txt,old.txt" {
		t.Fatalf("recent = %s", got)
	}
	x.clock.advance(time.Hour) // past the cache, so each query below is scanned again
	if got := x.recentNames(x.admin, q("root", x.root, "limit", "2")); len(got) != 2 || got[0] != "new.txt" {
		t.Fatalf("limit 2 = %v", got)
	}
	x.clock.advance(time.Hour)
	if got := x.recentNames(x.admin, q("root", filepath.Join(x.root, "sub"), "limit", "abc")); len(got) != 2 {
		t.Fatalf("bad limit must mean the default, got %v", got)
	}
	x.clock.advance(time.Hour)
	if got := x.recentNames(x.admin, q("root", filepath.Join(x.root, "emptydir"))); got == nil || len(got) != 0 {
		t.Fatalf("empty folder = %v", got)
	}
	res := x.do("GET", "/fs/recent"+q("root", filepath.Join(x.root, "emptydir")), x.admin, nil)
	if strings.TrimSpace(res.Body.String()) != "[]" {
		t.Fatalf("empty body = %q, want []", res.Body.String())
	}
}

func TestRecentRouteBadRoots(t *testing.T) {
	x := newXRig(t, nil)
	file := x.put("a.txt", 0)
	outside := t.TempDir()
	x.do("GET", "/fs/recent"+q("root", filepath.Join(x.root, "nope")), x.admin, nil).wantError(t, 404, api.CodePathNotFound)
	x.do("GET", "/fs/recent"+q("root", file), x.admin, nil).wantError(t, 400, api.CodeBadRequest)
	x.do("GET", "/fs/recent"+q("root", outside), x.admin, nil).wantError(t, 403, authz.CodeForbidden)
	x.do("GET", "/fs/recent"+q("root", "relative"), x.admin, nil).wantError(t, 400, api.CodeBadRequest)
}

func TestRecentRouteStaysInsideTheJailAndNeverSharesItsCache(t *testing.T) {
	x := newXRig(t, nil)
	x.put("out.txt", time.Hour)
	x.put("jail/in.txt", 0)
	whole := x.recentNames(x.admin, q("root", x.root))
	if len(whole) != 2 {
		t.Fatalf("unjailed = %v", whole)
	}
	if err := x.db.SetDeviceJail(x.adminID, filepath.Join(x.root, "jail")); err != nil {
		t.Fatal(err)
	}
	// Same query as the cached one: the jailed caller must not get its answer.
	x.do("GET", "/fs/recent"+q("root", x.root), x.admin, nil).wantError(t, 403, authz.CodeForbidden)
	// With no root the jailed caller scans its jail only.
	if got := x.recentNames(x.admin, ""); len(got) != 1 || got[0] != "in.txt" {
		t.Fatalf("jailed = %v", got)
	}
}

func TestRecentRouteCachesCompleteAnswersUntilTheyExpire(t *testing.T) {
	calls := 0
	hook := func(_ context.Context, _ api.RecentRequest) (*api.RecentPage, error) {
		calls++
		return &api.RecentPage{Entries: []api.FileEntry{{Name: "n"}}}, nil
	}
	x := newXRig(t, hook)
	for i := 0; i < 3; i++ {
		x.recentNames(x.admin, "")
	}
	if calls != 1 {
		t.Fatalf("scans = %d, want 1 within the cache lifetime", calls)
	}
	x.recentNames(x.admin, q("limit", "7")) // another limit is another query
	if calls != 2 {
		t.Fatalf("scans = %d, want 2", calls)
	}
	x.clock.advance(time.Hour)
	x.recentNames(x.admin, q("limit", "7"))
	if calls != 3 {
		t.Fatalf("scans = %d after expiry, want 3", calls)
	}
}

func TestRecentRoutePartialAnswerCarriesTheBudgetHeaderAndIsNotCached(t *testing.T) {
	calls := 0
	var sawLimit int
	var hasDeadline bool
	hook := func(ctx context.Context, req api.RecentRequest) (*api.RecentPage, error) {
		calls++
		sawLimit = req.Limit
		_, hasDeadline = ctx.Deadline()
		return &api.RecentPage{Partial: true}, nil
	}
	x := newXRig(t, hook)
	for i := 0; i < 2; i++ {
		res := x.do("GET", "/fs/recent"+q("limit", "100000"), x.admin, nil)
		res.wantStatus(t, 200)
		if res.Header().Get(xBudgetHeader) != "1" || strings.TrimSpace(res.Body.String()) != "[]" {
			t.Fatalf("partial answer = %v %q", res.Header(), res.Body.String())
		}
	}
	if calls != 2 || sawLimit != 500 || !hasDeadline {
		t.Fatalf("calls %d, limit %d, deadline %v; want 2, 500, true", calls, sawLimit, hasDeadline)
	}
	// A complete answer carries no such header.
	hookless := newXRig(t, nil)
	hookless.put("a.txt", 0)
	res := hookless.do("GET", "/fs/recent"+q("root", hookless.root), hookless.admin, nil)
	if res.Header().Get(xBudgetHeader) != "" {
		t.Fatal("budget header on a complete answer")
	}
}

func TestRecentRouteRefusesScansBeyondTheLimitWithRecentBusy(t *testing.T) {
	started := make(chan struct{}, xBusyScans)
	release := make(chan struct{})
	hook := func(ctx context.Context, _ api.RecentRequest) (*api.RecentPage, error) {
		started <- struct{}{}
		select {
		case <-release:
		case <-ctx.Done():
		}
		return &api.RecentPage{}, nil
	}
	x := newXRig(t, hook)
	get := func(limit string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", "/v1/fs/recent"+q("limit", limit), nil)
		req.RemoteAddr = xFixedRemote
		req.Header.Set("Authorization", "Bearer "+x.admin)
		rec := httptest.NewRecorder()
		x.h.ServeHTTP(rec, req)
		return rec
	}
	var wg sync.WaitGroup
	for i := 0; i < xBusyScans; i++ {
		wg.Add(1)
		go func(limit string) { defer wg.Done(); get(limit) }(string(rune('1' + i))) // distinct queries: no cache hit
	}
	for i := 0; i < xBusyScans; i++ {
		<-started
	}
	busy := get("9")
	close(release)
	wg.Wait()
	if busy.Code != http.StatusTooManyRequests || busy.Header().Get("Retry-After") != "1" {
		t.Fatalf("third scan = %d %v %s", busy.Code, busy.Header(), busy.Body.String())
	}
	if !strings.Contains(busy.Body.String(), api.CodeRecentBusy) {
		t.Fatalf("body = %s", busy.Body.String())
	}
}

func TestFsExtraRoutesAnswer501WithoutTheExtraService(t *testing.T) {
	// A file service that does not offer the extra operations (the plain
	// adapter) and a server with no file service at all.
	f := newFileRig(t, 0)
	p := f.put("a.txt", "x")
	f.do("GET", "/fs/recent", f.admin, nil).wantError(t, 501, api.CodeNotImplemented)
	f.do("GET", "/fs/media-info"+q("path", p), f.admin, nil).wantError(t, 501, api.CodeNotImplemented)
	f.do("POST", "/fs/chmod", f.admin, map[string]string{"path": p, "mode": "600"}).wantError(t, 501, api.CodeNotImplemented)

	r := newRig(t, rigOptions{})
	tok, _ := r.registerAdmin(newPhone("admin-1", 1))
	r.do("GET", "/fs/recent", tok, nil).wantError(t, 501, api.CodeNotImplemented)
	r.do("GET", "/fs/media-info"+q("path", "/"), tok, nil).wantError(t, 501, api.CodeNotImplemented)
	r.do("POST", "/fs/chmod", tok, map[string]string{"path": "/a", "mode": "600"}).wantError(t, 501, api.CodeNotImplemented)
}
