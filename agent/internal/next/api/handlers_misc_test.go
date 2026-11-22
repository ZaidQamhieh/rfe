package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZaidQamhieh/rfe/agent/internal/next/api"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/apps"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/audit"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/rootfs"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/search"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/settings"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/share"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/system"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/thumbs"
)

// Names and sizes the misc tests use.
const (
	fileText       = "hello world"
	thumbSourceW   = 200
	thumbSourceH   = 100
	pageSize       = 2
	searchFiles    = 5
	oversizeQuery  = 600 // longer than the search service's query bound
	hugeQueryBytes = 9000
	shareTestHost  = "files.example.test"
)

// stubSystem answers the system routes, which these tests do not use.
type stubSystem struct{}

func (stubSystem) Health(bool) system.Health      { return system.Health{} }
func (stubSystem) Status() (system.Status, error) { return system.Status{}, nil }
func (stubSystem) Metrics() system.Metrics        { return system.Metrics{} }

// searchAdapter connects the search service to api.SearchService: it parses the
// query and narrows the folders to the device's jail.
type searchAdapter struct {
	set   *rootfs.Set
	roots []string
}

func (a searchAdapter) Search(ctx context.Context, jail string, q url.Values) (api.SearchPage, error) {
	req, err := search.ParseQuery(q)
	if err != nil {
		return api.SearchPage{}, err
	}
	set, roots := a.set, a.roots
	if jail != "" {
		roots = []string{jail} // a jailed device searches from its jail folder, not the shared roots
		narrow, err := a.set.Narrow(jail)
		if err != nil {
			return api.SearchPage{}, err
		}
		defer narrow.Close()
		set = narrow
	}
	res, err := search.New(set, search.Config{Roots: roots}).Search(ctx, req)
	if err != nil {
		return api.SearchPage{}, err
	}
	return api.SearchPage{Entries: res.Entries, NextCursor: res.NextCursor, Truncated: res.Truncated, TimeBudget: res.TimeBudget}, nil
}

// shareAdapter connects the share service to api.ShareService.
type shareAdapter struct{ svc *share.Service }

func caller(c api.ShareCaller) share.Caller {
	return share.Caller{Principal: c.Principal, Label: c.Label}
}

func events(in []share.Event) []audit.Event {
	out := make([]audit.Event, 0, len(in))
	for _, e := range in {
		out = append(out, audit.Event{Action: e.Action, Actor: e.Actor, Target: e.Target, Detail: e.Detail})
	}
	return out
}

func (a shareAdapter) Mint(c api.ShareCaller, req api.ShareMintRequest) (api.ShareMinted, error) {
	m, err := a.svc.Mint(caller(c), share.MintRequest{Path: req.Path, ExpiresInSeconds: req.ExpiresInSeconds})
	if err != nil {
		return api.ShareMinted{}, err
	}
	return api.ShareMinted{Token: m.Token, TokenHash: m.TokenHash, ExpiresAt: m.ExpiresAt, Events: events(m.Events)}, nil
}

func (a shareAdapter) List(c api.ShareCaller) ([]api.ShareSummary, error) {
	links, err := a.svc.List(caller(c))
	if err != nil {
		return nil, err
	}
	out := make([]api.ShareSummary, 0, len(links))
	for _, l := range links {
		out = append(out, api.ShareSummary{TokenHash: l.TokenHash, Path: l.Path, ExpiresAt: l.ExpiresAt})
	}
	return out, nil
}

func (a shareAdapter) Revoke(c api.ShareCaller, hash string) ([]audit.Event, error) {
	ev, err := a.svc.Revoke(caller(c), hash)
	return events(ev), err
}

func (a shareAdapter) Resolve(token, ip string) (*api.ShareDownload, error) {
	d, err := a.svc.Resolve(token, ip)
	if err != nil {
		return nil, err
	}
	return &api.ShareDownload{
		Content: d.Content, Name: d.Name, ModTime: d.ModTime, Header: d.Header, RecordErr: d.RecordErr, Close: d.Close,
	}, nil
}

// misc is the stack with real search, thumbnail, share, settings and audit
// services over a temporary folder.
type misc struct {
	*rig
	root  string
	log   *audit.Log
	set   *settings.Service
	admin string
	// dev is an ordinary device holding the browse, download and share grants.
	dev, devID string
}

// newMisc builds the stack; mutate may swap a service before the handler is built.
func newMisc(t *testing.T, mutate func(*api.MiscServices)) *misc {
	t.Helper()
	r := newRig(t, rigOptions{})
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	write := func(rel, data string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a.txt", fileText)
	write("dir/b.txt", "bee")
	for i := range searchFiles {
		write("pages/note"+string(rune('1'+i))+".txt", "n")
	}
	var img bytes.Buffer
	if err := png.Encode(&img, image.NewRGBA(image.Rect(0, 0, thumbSourceW, thumbSourceH))); err != nil {
		t.Fatal(err)
	}
	write("pic.png", img.String())

	fileSet, err := rootfs.New([]string{root}, rootfs.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fileSet.Close() })
	st, err := settings.Open(r.db, settings.Options{RootsExplicit: true, SeedRoots: []string{root}, AgentName: testName})
	if err != nil {
		t.Fatal(err)
	}
	thumbSvc, err := thumbs.New(thumbs.Config{CacheDir: filepath.Join(t.TempDir(), "thumbs")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { thumbSvc.Close() })
	log := audit.New(r.db, audit.Options{})
	svcs := api.MiscServices{
		Search:    searchAdapter{set: fileSet, roots: []string{root}},
		Thumbs:    thumbSvc,
		ThumbSets: thumbSetFor(fileSet),
		Shares:    shareAdapter{share.New(share.Config{Set: fileSet, Store: r.db})},
		Settings:  st,
		Audit:     log,
	}
	if mutate != nil {
		mutate(&svcs)
	}
	h, err := api.New(api.Config{
		Pairing: panicPairing{r.svc}, Devices: r.db, System: stubSystem{}, Policy: st, Logger: r.logs,
		Apps: apps.New(apps.Config{Lister: fakeLister{}, Launcher: r.launcher}), Misc: svcs,
	})
	if err != nil {
		t.Fatal(err)
	}
	r.h = h
	m := &misc{rig: r, root: root, log: log, set: st}
	m.admin, _ = r.registerAdmin(newPhone("admin-phone", 1))
	m.dev, m.devID = r.pairDevice(newPhone("dev-phone", 2))
	m.grant(m.devID, true, true, true)
	return m
}

// thumbSetFor gives every device the whole set, or its jail narrowed.
func thumbSetFor(set *rootfs.Set) api.ThumbSetFor {
	return func(jail string) (*rootfs.Set, func(), error) {
		if jail == "" {
			return set, func() {}, nil
		}
		n, err := set.Narrow(jail)
		if err != nil {
			return nil, nil, err
		}
		return n, func() { n.Close() }, nil
	}
}

// grant sets the browse, download and share grants of a device.
func (m *misc) grant(id string, browse, download, shareLinks bool) {
	m.t.Helper()
	if err := m.db.SetDeviceFileGrants(id, [6]bool{browse, download, false, false, false, shareLinks}); err != nil {
		m.t.Fatal(err)
	}
}

// get sends an authenticated GET.
func (m *misc) get(path, token string) result {
	m.t.Helper()
	return m.do(http.MethodGet, path, token, nil)
}

// noHostPath fails when a body names the temporary folder.
func (m *misc) noHostPath(res result) {
	m.t.Helper()
	if strings.Contains(res.Body.String(), m.root) {
		m.t.Fatalf("body leaks the host path: %s", res.Body.String())
	}
}

// entryNames returns the names in a search answer.
func entryNames(t *testing.T, res result) []string {
	t.Helper()
	var es []struct {
		Name string `json:"name"`
	}
	res.json(t, &es)
	out := make([]string, 0, len(es))
	for _, e := range es {
		out = append(out, e.Name)
	}
	return out
}

func TestSearchPagesThroughCursor(t *testing.T) {
	m := newMisc(t, nil)
	seen := map[string]bool{}
	cursor := ""
	for page := 0; page < searchFiles+2; page++ {
		path := "/search?q=note&limit=" + "2"
		if cursor != "" {
			path += "&cursor=" + url.QueryEscape(cursor)
		}
		res := m.get(path, m.dev)
		if res.Code != http.StatusOK {
			t.Fatalf("page %d = %d %s", page, res.Code, res.Body.String())
		}
		names := entryNames(t, res)
		if len(names) > pageSize {
			t.Fatalf("page %d has %d entries", page, len(names))
		}
		for _, n := range names {
			if seen[n] {
				t.Fatalf("entry %s repeated across pages", n)
			}
			seen[n] = true
		}
		cursor = res.Header().Get("X-Search-Next-Cursor")
		if cursor == "" {
			if res.Header().Get("X-Search-Truncated") != "" {
				t.Fatal("last page is marked truncated")
			}
			break
		}
		if res.Header().Get("X-Search-Truncated") != "1" {
			t.Fatalf("page %d has a cursor but no truncated flag: %v", page, res.Header())
		}
	}
	if len(seen) != searchFiles {
		t.Fatalf("found %d of %d files: %v", len(seen), searchFiles, seen)
	}
}

func TestSearchEmptyResultIsArray(t *testing.T) {
	m := newMisc(t, nil)
	res := m.get("/search?q=zzzz-no-such-name", m.dev)
	if res.Code != http.StatusOK || strings.TrimSpace(res.Body.String()) != "[]" {
		t.Fatalf("= %d %q", res.Code, res.Body.String())
	}
}

func TestSearchRefusals(t *testing.T) {
	m := newMisc(t, nil)
	cases := []struct {
		name, path, token string
		status            int
		code              string
	}{
		{"no token", "/search?q=a", "", 401, "UNAUTHORIZED"},
		{"missing q", "/search", m.dev, 400, "BAD_REQUEST"},
		{"blank q", "/search?q=%20%20", m.dev, 400, "BAD_REQUEST"},
		{"q too long", "/search?q=" + strings.Repeat("a", oversizeQuery), m.dev, 400, "BAD_REQUEST"},
		{"query string too long", "/search?q=a&x=" + strings.Repeat("b", hugeQueryBytes), m.dev, 400, "BAD_REQUEST"},
		{"unknown type", "/search?q=a&types=bogus", m.dev, 400, "BAD_REQUEST"},
		{"bad size", "/search?q=a&minSize=big", m.dev, 400, "BAD_REQUEST"},
		{"bad cursor", "/search?q=a&cursor=not-a-cursor", m.dev, 400, "BAD_REQUEST"},
		{"root outside", "/search?q=a&root=" + url.QueryEscape(filepath.Dir(m.root)), m.dev, 403, "FORBIDDEN"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res := m.get(c.path, c.token)
			res.wantError(t, c.status, c.code)
			m.noHostPath(res)
		})
	}
	m.grant(m.devID, false, true, true)
	m.get("/search?q=a", m.dev).wantError(t, 403, "CAPABILITY_DENIED")
}

func TestSearchStaysInsideJail(t *testing.T) {
	m := newMisc(t, nil)
	if err := m.db.SetDeviceJail(m.devID, filepath.Join(m.root, "dir")); err != nil {
		t.Fatal(err)
	}
	res := m.get("/search?q=txt", m.dev)
	if res.Code != http.StatusOK {
		t.Fatalf("= %d %s", res.Code, res.Body.String())
	}
	if names := entryNames(t, res); len(names) != 1 || names[0] != "b.txt" {
		t.Fatalf("jailed device sees %v", names)
	}
	if names := entryNames(t, m.get("/search?q=txt", m.admin)); len(names) < 2 {
		t.Fatalf("admin without a jail sees %v", names)
	}
}

func TestSearchNotWiredIs501(t *testing.T) {
	m := newMisc(t, func(s *api.MiscServices) { s.Search = nil })
	m.get("/search?q=a", m.dev).wantError(t, 501, "NOT_IMPLEMENTED")
}

// fakeThumbs returns a fixed error from Get.
type fakeThumbs struct{ err error }

func (f fakeThumbs) Get(context.Context, *rootfs.Set, string, int) (*thumbs.Result, error) {
	return nil, f.err
}

func TestThumbServesJPEGWithETag(t *testing.T) {
	m := newMisc(t, nil)
	pic := url.QueryEscape(filepath.Join(m.root, "pic.png"))
	res := m.get("/thumb?path="+pic+"&size=100", m.dev)
	if res.Code != http.StatusOK {
		t.Fatalf("= %d %s", res.Code, res.Body.String())
	}
	if ct := res.Header().Get("Content-Type"); ct != "image/jpeg" {
		t.Fatalf("Content-Type = %q", ct)
	}
	if cc := res.Header().Get("Cache-Control"); !strings.HasPrefix(cc, "private") {
		t.Fatalf("Cache-Control = %q", cc)
	}
	etag := res.Header().Get("ETag")
	if etag == "" || res.Header().Get("Last-Modified") == "" {
		t.Fatalf("validators missing: %v", res.Header())
	}
	img, err := png.Decode(bytes.NewReader(nil))
	_ = img
	_ = err
	cfg, _, err := image.DecodeConfig(bytes.NewReader(res.Body.Bytes()))
	if err != nil || cfg.Width > 128 || cfg.Width == 0 {
		t.Fatalf("thumbnail width = %d, err %v (size 100 rounds up to the 128 bucket)", cfg.Width, err)
	}
	again := m.do(http.MethodGet, "/thumb?path="+pic+"&size=100", m.dev, nil, withHeader("If-None-Match", etag))
	if again.Code != http.StatusNotModified || again.Body.Len() != 0 {
		t.Fatalf("revalidation = %d with %d body bytes", again.Code, again.Body.Len())
	}
	if again.Header().Get("ETag") != etag {
		t.Fatalf("304 lost the ETag: %v", again.Header())
	}
	stale := m.do(http.MethodGet, "/thumb?path="+pic+"&size=100", m.dev, nil, withHeader("If-None-Match", `W/"other"`))
	if stale.Code != http.StatusOK {
		t.Fatalf("a different ETag = %d", stale.Code)
	}
}

func TestThumbRefusals(t *testing.T) {
	m := newMisc(t, nil)
	text := url.QueryEscape(filepath.Join(m.root, "a.txt"))
	pic := url.QueryEscape(filepath.Join(m.root, "pic.png"))
	outside := url.QueryEscape(filepath.Join(filepath.Dir(m.root), "x.png"))
	cases := []struct {
		name, path string
		status     int
		code       string
	}{
		{"no path", "/thumb", 400, "BAD_REQUEST"},
		{"size not a number", "/thumb?path=" + pic + "&size=big", 400, "BAD_REQUEST"},
		{"size zero", "/thumb?path=" + pic + "&size=0", 400, "BAD_REQUEST"},
		{"size negative", "/thumb?path=" + pic + "&size=-5", 400, "BAD_REQUEST"},
		{"not an image", "/thumb?path=" + text, 404, "NOT_AVAILABLE"},
		{"missing file", "/thumb?path=" + url.QueryEscape(filepath.Join(m.root, "gone.png")), 404, "NOT_AVAILABLE"},
		{"outside the roots", "/thumb?path=" + outside, 403, "FORBIDDEN"},
		{"relative path", "/thumb?path=pic.png", 400, "BAD_REQUEST"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res := m.get(c.path, m.dev)
			res.wantError(t, c.status, c.code)
			m.noHostPath(res)
		})
	}
	m.get("/thumb?path="+pic, "").wantError(t, 401, "UNAUTHORIZED")
	m.grant(m.devID, true, false, true)
	m.get("/thumb?path="+pic, m.dev).wantError(t, 403, "CAPABILITY_DENIED")
}

func TestThumbBusyIs429WithRetryAfter(t *testing.T) {
	m := newMisc(t, func(s *api.MiscServices) { s.Thumbs = fakeThumbs{err: thumbs.ErrBusy} })
	res := m.get("/thumb?path="+url.QueryEscape(filepath.Join(m.root, "pic.png")), m.dev)
	res.wantError(t, 429, "THUMB_BUSY")
	if res.Header().Get("Retry-After") != "1" {
		t.Fatalf("Retry-After = %q", res.Header().Get("Retry-After"))
	}
}

func TestThumbInternalFailureHidesDetail(t *testing.T) {
	m := newMisc(t, func(s *api.MiscServices) { s.Thumbs = fakeThumbs{err: os.ErrInvalid} })
	res := m.get("/thumb?path="+url.QueryEscape(filepath.Join(m.root, "pic.png")), m.dev)
	res.wantError(t, 500, "INTERNAL")
}

// settingsBody is the part of the settings answer the tests read.
type settingsBody struct {
	IsAdmin               bool            `json:"isAdmin"`
	EffectiveScope        string          `json:"effectiveScope"`
	AccessDenied          bool            `json:"accessDenied"`
	ReadOnly              bool            `json:"readOnly"`
	Roots                 []string        `json:"roots"`
	AgentName             string          `json:"agentName"`
	AllowSharing          bool            `json:"allowSharing"`
	PhotoBackupConfigured bool            `json:"photoBackupConfigured"`
	PhotoBackupAvailable  bool            `json:"photoBackupAvailable"`
	PhotoBackupRoot       *string         `json:"photoBackupRoot"`
	FileCapabilities      map[string]bool `json:"fileCapabilities"`
}

func TestSettingsViewsByCaller(t *testing.T) {
	m := newMisc(t, nil)
	var admin settingsBody
	m.get("/settings", m.admin).json(t, &admin)
	if !admin.IsAdmin || admin.EffectiveScope != "global" || len(admin.Roots) != 1 || admin.Roots[0] != m.root ||
		admin.PhotoBackupRoot == nil || !admin.FileCapabilities["upload"] {
		t.Fatalf("admin view = %+v", admin)
	}
	var dev settingsBody
	m.get("/settings", m.dev).json(t, &dev)
	if dev.IsAdmin || dev.EffectiveScope != "device" || dev.PhotoBackupRoot != nil ||
		!dev.FileCapabilities["browse"] || dev.FileCapabilities["upload"] || dev.AccessDenied {
		t.Fatalf("device view = %+v", dev)
	}

	jail := filepath.Join(m.root, "dir")
	if err := m.db.SetDeviceJail(m.devID, jail); err != nil {
		t.Fatal(err)
	}
	m.get("/settings", m.dev).json(t, &dev)
	if len(dev.Roots) != 1 || dev.Roots[0] != jail {
		t.Fatalf("jailed view roots = %v", dev.Roots)
	}
	outside := filepath.Dir(m.root)
	if err := m.db.SetDeviceJail(m.devID, outside); err != nil {
		t.Fatal(err)
	}
	dev = settingsBody{}
	res := m.get("/settings", m.dev)
	res.json(t, &dev)
	if !dev.AccessDenied || dev.Roots == nil || len(dev.Roots) != 0 || !strings.Contains(res.Body.String(), `"roots":[]`) {
		t.Fatalf("a jail outside the roots = %+v (%s)", dev, res.Body.String())
	}
	m.get("/settings", "").wantError(t, 401, "UNAUTHORIZED")
}

func TestSettingsPhotoBackupDisclosedOnlyWhenReachable(t *testing.T) {
	m := newMisc(t, nil)
	backup := filepath.Join(m.root, "backup")
	res := m.do(http.MethodPatch, "/settings", m.admin, map[string]any{"photoBackupRoot": backup})
	if res.Code != http.StatusOK {
		t.Fatalf("patch = %d %s", res.Code, res.Body.String())
	}
	var dev settingsBody
	m.get("/settings", m.dev).json(t, &dev)
	if !dev.PhotoBackupConfigured || !dev.PhotoBackupAvailable || dev.PhotoBackupRoot == nil || *dev.PhotoBackupRoot != backup {
		t.Fatalf("reachable backup folder: %+v", dev)
	}
	if err := m.db.SetDeviceJail(m.devID, filepath.Join(m.root, "dir")); err != nil {
		t.Fatal(err)
	}
	dev = settingsBody{}
	m.get("/settings", m.dev).json(t, &dev)
	if !dev.PhotoBackupConfigured || dev.PhotoBackupAvailable || dev.PhotoBackupRoot != nil {
		t.Fatalf("backup folder outside the jail must stay hidden: %+v", dev)
	}
}

func TestSettingsPatch(t *testing.T) {
	m := newMisc(t, nil)
	res := m.do(http.MethodPatch, "/settings", m.admin, map[string]any{"agentName": "Desk", "readOnly": true, "allowSharing": true})
	var got settingsBody
	res.json(t, &got)
	if res.Code != http.StatusOK || got.AgentName != "Desk" || !got.ReadOnly || !got.AllowSharing || len(got.Roots) != 1 {
		t.Fatalf("patch = %d %+v", res.Code, got)
	}
	if now := m.set.Get(); now.AgentName != "Desk" || !now.ReadOnly || !now.AllowSharing {
		t.Fatalf("service did not change: %+v", now)
	}
	// An empty roots list is the explicit choice of unrestricted access.
	res = m.do(http.MethodPatch, "/settings", m.admin, map[string]any{"readOnly": false, "roots": []string{}})
	if res.Code != http.StatusOK || !m.set.Get().Unrestricted() {
		t.Fatalf("roots [] = %d %s", res.Code, res.Body.String())
	}
}

func TestSettingsPatchRefusals(t *testing.T) {
	m := newMisc(t, nil)
	before := m.set.Get()
	m.do(http.MethodPatch, "/settings", m.dev, map[string]any{"readOnly": true}).wantError(t, 403, "FORBIDDEN")
	m.do(http.MethodPatch, "/settings", "", map[string]any{"readOnly": true}).wantError(t, 401, "UNAUTHORIZED")
	secret := "relative/secret-folder"
	res := m.do(http.MethodPatch, "/settings", m.admin, map[string]any{"readOnly": true, "roots": []string{secret}})
	res.wantError(t, 400, "BAD_REQUEST")
	if strings.Contains(res.Body.String(), secret) {
		t.Fatalf("error repeats the refused value: %s", res.Body.String())
	}
	m.do(http.MethodPatch, "/settings", m.admin, "{not json").wantError(t, 400, "BAD_REQUEST")
	m.do(http.MethodPatch, "/settings", m.admin, map[string]any{"agentName": strings.Repeat("n", settings.MaxAgentNameRunes+1)}).wantError(t, 400, "BAD_REQUEST")
	if after := m.set.Get(); after.ReadOnly != before.ReadOnly || after.AgentName != before.AgentName || len(after.Roots) != len(before.Roots) {
		t.Fatalf("a refused patch changed settings: %+v -> %+v", before, after)
	}
}

func TestBandwidth(t *testing.T) {
	m := newMisc(t, nil)
	var bw api.BandwidthJSON
	m.get("/settings/bandwidth", m.admin).json(t, &bw)
	if bw.MaxUploadBytesPerSec != 0 || bw.MaxDownloadBytesPerSec != 0 {
		t.Fatalf("default = %+v", bw)
	}
	res := m.do(http.MethodPut, "/settings/bandwidth", m.admin, map[string]any{"maxUploadBytesPerSec": 1000})
	res.json(t, &bw)
	if res.Code != http.StatusOK || bw.MaxUploadBytesPerSec != 1000 || bw.MaxDownloadBytesPerSec != 0 {
		t.Fatalf("put = %d %+v", res.Code, bw)
	}
	m.do(http.MethodPut, "/settings/bandwidth", m.admin, map[string]any{"maxDownloadBytesPerSec": 77}).json(t, &bw)
	if bw.MaxUploadBytesPerSec != 1000 || bw.MaxDownloadBytesPerSec != 77 {
		t.Fatalf("a partial put must keep the other limit: %+v", bw)
	}
	m.do(http.MethodPut, "/settings/bandwidth", m.admin, map[string]any{"maxUploadBytesPerSec": -1}).wantError(t, 400, "BAD_REQUEST")
	m.get("/settings/bandwidth", m.dev).wantError(t, 403, "FORBIDDEN")
	m.do(http.MethodPut, "/settings/bandwidth", m.dev, map[string]any{"maxUploadBytesPerSec": 5}).wantError(t, 403, "FORBIDDEN")
	if m.set.Get().MaxUploadBytesPerSec != 1000 {
		t.Fatal("a refused call changed the limit")
	}
}

func TestSettingsNotWiredStillReadable(t *testing.T) {
	m := newMisc(t, func(s *api.MiscServices) { s.Settings = nil })
	if res := m.get("/settings", m.admin); res.Code != http.StatusOK {
		t.Fatalf("GET falls back to the policy source: %d", res.Code)
	}
	m.do(http.MethodPatch, "/settings", m.admin, map[string]any{"readOnly": true}).wantError(t, 501, "NOT_IMPLEMENTED")
	m.get("/settings/bandwidth", m.admin).wantError(t, 501, "NOT_IMPLEMENTED")
}

// auditPage is the body of GET /audit.
type auditPage struct {
	Entries []struct {
		ID     int64  `json:"id"`
		Action string `json:"action"`
	} `json:"entries"`
}

func TestAuditPagingAndLimitFallback(t *testing.T) {
	m := newMisc(t, nil)
	const total = 5
	for range total {
		m.log.Try(audit.Event{Action: audit.ActionLogin, Actor: "owner"})
	}
	read := func(query string) auditPage {
		t.Helper()
		res := m.get("/audit"+query, m.admin)
		if res.Code != http.StatusOK {
			t.Fatalf("%s = %d %s", query, res.Code, res.Body.String())
		}
		var p auditPage
		res.json(t, &p)
		return p
	}
	first := read("?limit=2")
	if len(first.Entries) != 2 || first.Entries[0].ID <= first.Entries[1].ID {
		t.Fatalf("first page = %+v (newest first expected)", first)
	}
	next := read("?limit=2&before=" + itoa(first.Entries[1].ID))
	if len(next.Entries) == 0 || next.Entries[0].ID >= first.Entries[1].ID {
		t.Fatalf("second page = %+v", next)
	}
	// Out-of-range and unusable limits give the default page, which holds everything here.
	for _, q := range []string{"", "?limit=0", "?limit=-3", "?limit=501", "?limit=99999", "?limit=abc"} {
		if n := len(read(q).Entries); n < total {
			t.Fatalf("%q returned %d entries, want the default page of 100", q, n)
		}
	}
	if n := len(read("?before=junk").Entries); n < total {
		t.Fatalf("an unusable before must give the newest page, got %d", n)
	}
	m.get("/audit", m.dev).wantError(t, 403, "FORBIDDEN")
	m.get("/audit", "").wantError(t, 401, "UNAUTHORIZED")
}

func TestAuditEmptyIsArray(t *testing.T) {
	m := newMisc(t, nil)
	res := m.get("/audit?before=1", m.admin)
	if res.Code != http.StatusOK || !strings.Contains(res.Body.String(), `"entries":[]`) {
		t.Fatalf("= %d %s", res.Code, res.Body.String())
	}
}

// itoa formats an id for a query string.
func itoa(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}

// mint asks for a link to the file and returns the answer.
type mintAnswer struct {
	Token     string `json:"token"`
	TokenHash string `json:"tokenHash"`
	ExpiresAt int64  `json:"expiresAt"`
	URL       string `json:"url"`
}

func (m *misc) mint(token, path string, expires int64) (result, mintAnswer) {
	m.t.Helper()
	body := map[string]any{"path": path}
	if expires != 0 {
		body["expiresInSeconds"] = expires
	}
	res := m.do(http.MethodPost, "/share/mint", token, body, withHeader("Host", shareTestHost))
	var a mintAnswer
	if res.Code == http.StatusOK {
		res.json(m.t, &a)
	}
	return res, a
}

func (m *misc) enableSharing() {
	m.t.Helper()
	on := true
	if _, err := m.set.Update(settings.Update{AllowSharing: &on}); err != nil {
		m.t.Fatal(err)
	}
}

func TestShareMintFetchOnce(t *testing.T) {
	m := newMisc(t, nil)
	m.enableSharing()
	res, link := m.mint(m.dev, filepath.Join(m.root, "a.txt"), 0)
	if res.Code != http.StatusOK || link.Token == "" || link.TokenHash == "" || link.ExpiresAt == 0 {
		t.Fatalf("mint = %d %s", res.Code, res.Body.String())
	}
	m.noHostPath(res)
	if strings.Contains(res.Body.String(), `"path"`) {
		t.Fatalf("mint echoes the path: %s", res.Body.String())
	}
	if !strings.HasSuffix(link.URL, "/v1/share/"+link.Token) || !strings.HasPrefix(link.URL, "https://") {
		t.Fatalf("url = %q", link.URL)
	}

	list := m.get("/share", m.dev)
	var rows []struct {
		TokenHash string `json:"tokenHash"`
		Path      string `json:"path"`
	}
	list.json(t, &rows)
	if len(rows) != 1 || rows[0].TokenHash != link.TokenHash || strings.Contains(list.Body.String(), link.Token) {
		t.Fatalf("list = %s", list.Body.String())
	}

	got := m.get("/share/"+link.Token, "")
	if got.Code != http.StatusOK || got.Body.String() != fileText {
		t.Fatalf("fetch = %d %q", got.Code, got.Body.String())
	}
	if d := got.Header().Get("Content-Disposition"); !strings.HasPrefix(d, "attachment") {
		t.Fatalf("Content-Disposition = %q", d)
	}
	if got.Header().Get("X-Content-Type-Options") != "nosniff" || got.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("headers = %v", got.Header())
	}
	again := m.get("/share/"+link.Token, "")
	again.wantError(t, 404, "NOT_FOUND")
	m.noHostPath(again)
	if rows := m.get("/share", m.dev); strings.Contains(rows.Body.String(), link.TokenHash) {
		t.Fatalf("a used link is still listed: %s", rows.Body.String())
	}
}

func TestShareFetchHonoursRange(t *testing.T) {
	m := newMisc(t, nil)
	m.enableSharing()
	_, link := m.mint(m.dev, filepath.Join(m.root, "a.txt"), 0)
	res := m.do(http.MethodGet, "/share/"+link.Token, "", nil, withHeader("Range", "bytes=0-4"))
	if res.Code != http.StatusPartialContent || res.Body.String() != fileText[:5] ||
		res.Header().Get("Content-Range") != "bytes 0-4/"+itoa(int64(len(fileText))) {
		t.Fatalf("range fetch = %d %q %v", res.Code, res.Body.String(), res.Header())
	}
}

func TestShareMintRefusals(t *testing.T) {
	m := newMisc(t, nil)
	file := filepath.Join(m.root, "a.txt")
	// Sharing is off by default.
	res, _ := m.mint(m.dev, file, 0)
	res.wantError(t, 403, "FORBIDDEN")
	m.enableSharing()
	res, _ = m.mint(m.dev, filepath.Join(m.root, "dir"), 0)
	res.wantError(t, 422, "NOT_A_FILE")
	res, _ = m.mint(m.dev, filepath.Join(filepath.Dir(m.root), "x.txt"), 0)
	res.wantError(t, 403, "FORBIDDEN")
	m.noHostPath(res)
	res, _ = m.mint(m.dev, filepath.Join(m.root, "gone.txt"), 0)
	res.wantError(t, 404, "PATH_NOT_FOUND")
	m.noHostPath(res)
	res, _ = m.mint(m.dev, file, 7*24*3600+1)
	res.wantError(t, 400, "BAD_REQUEST")
	res, _ = m.mint(m.dev, "", 0)
	res.wantError(t, 400, "BAD_REQUEST")
	m.do(http.MethodPost, "/share/mint", m.dev, "{bad").wantError(t, 400, "BAD_REQUEST")
	m.do(http.MethodPost, "/share/mint", "", map[string]any{"path": file}).wantError(t, 401, "UNAUTHORIZED")
	m.grant(m.devID, true, true, false)
	res, _ = m.mint(m.dev, file, 0)
	res.wantError(t, 403, "CAPABILITY_DENIED")
}

func TestShareRevokeIsOwnerScoped(t *testing.T) {
	m := newMisc(t, nil)
	m.enableSharing()
	_, link := m.mint(m.dev, filepath.Join(m.root, "a.txt"), 0)
	other, otherID := m.pairDevice(newPhone("other-phone", 3))
	m.grant(otherID, true, true, true)

	m.do(http.MethodDelete, "/share/"+link.TokenHash, other, nil).wantError(t, 404, "NOT_FOUND")
	m.do(http.MethodDelete, "/share/unknown-hash", m.dev, nil).wantError(t, 404, "NOT_FOUND")
	if rows := m.get("/share", other); strings.Contains(rows.Body.String(), link.TokenHash) {
		t.Fatal("a device lists another device's link")
	}
	if rows := m.get("/share", m.admin); !strings.Contains(rows.Body.String(), link.TokenHash) {
		t.Fatalf("an admin must list every link: %s", rows.Body.String())
	}
	if res := m.do(http.MethodDelete, "/share/"+link.TokenHash, m.dev, nil); res.Code != http.StatusNoContent {
		t.Fatalf("owner revoke = %d %s", res.Code, res.Body.String())
	}
	m.get("/share/"+link.Token, "").wantError(t, 404, "NOT_FOUND")
	m.do(http.MethodDelete, "/share/"+link.TokenHash, "", nil).wantError(t, 401, "UNAUTHORIZED")

	// An admin may revoke any link.
	_, second := m.mint(m.dev, filepath.Join(m.root, "a.txt"), 0)
	if res := m.do(http.MethodDelete, "/share/"+second.TokenHash, m.admin, nil); res.Code != http.StatusNoContent {
		t.Fatalf("admin revoke = %d", res.Code)
	}
}

func TestShareEventsReachTheAuditTrail(t *testing.T) {
	m := newMisc(t, nil)
	m.enableSharing()
	_, link := m.mint(m.dev, filepath.Join(m.root, "a.txt"), 0)
	m.do(http.MethodDelete, "/share/"+link.TokenHash, m.dev, nil)
	var p auditPage
	m.get("/audit", m.admin).json(t, &p)
	actions := map[string]bool{}
	for _, e := range p.Entries {
		actions[e.Action] = true
	}
	if !actions[audit.ActionShareCreated] || !actions[audit.ActionShareRevoked] {
		t.Fatalf("audit actions = %v", actions)
	}
}

func TestShareFetchIsRateLimitedPerAddress(t *testing.T) {
	m := newMisc(t, nil)
	var last *httptest.ResponseRecorder
	for range share.RateLimitAttempts + 1 {
		req := httptest.NewRequest(http.MethodGet, "/v1/share/unknown-token", nil)
		req.RemoteAddr = "203.0.113.7:5000"
		last = httptest.NewRecorder()
		m.h.ServeHTTP(last, req)
	}
	result{last}.wantError(t, 429, "RATE_LIMITED")
	if long := m.get("/share/"+strings.Repeat("a", 300), ""); long.Code != http.StatusNotFound {
		t.Fatalf("oversized token = %d", long.Code)
	}
}

func TestShareNotWiredIs501(t *testing.T) {
	m := newMisc(t, func(s *api.MiscServices) { s.Shares = nil })
	m.get("/share", m.dev).wantError(t, 501, "NOT_IMPLEMENTED")
	m.get("/share/abc", "").wantError(t, 501, "NOT_IMPLEMENTED")
}
