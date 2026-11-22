package api_test

import (
	"archive/zip"
	"bytes"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZaidQamhieh/rfe/agent/internal/next/api"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/apps"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/authz"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/files"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/pairing"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/rootfs"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/system"
)

// Fixed texts of the content tests.
const (
	contentText    = "0123456789"
	rangeHeader    = "Range"
	rangeRequest   = "bytes=2-5"
	rangeBody      = "2345"
	rangeResponse  = "bytes 2-5/10"
	rangeSuffix    = "bytes=-3"
	rangeSuffixOut = "789"
	rangeTooFar    = "bytes=100-"
	rangeUnmet     = "bytes */10"
	headerRange    = "Content-Range"
)

// jailedProvider serves each caller a file service over the folders its jail and
// read-only flag allow, the way the daemon's provider does.
type jailedProvider struct{ set *rootfs.Set }

func (p jailedProvider) For(pr authz.Principal, _ authz.Policy) (api.FileService, error) {
	set := p.set
	if pr.JailRoot != "" {
		var err error
		if set, err = set.Narrow(pr.JailRoot); err != nil {
			return nil, err
		}
	}
	return filesAdapter{files.New(set)}, nil
}

// bareProvider serves a service that cannot read content.
type bareProvider struct{ api.FileService }

func (p bareProvider) For(authz.Principal, authz.Policy) (api.FileService, error) {
	return p.FileService, nil
}

// contentRig is the HTTP stack over one allowed folder with per-caller jails.
type contentRig struct {
	*rig
	root  string
	admin string
}

// newContentRig builds the stack with the given provider (nil: a jailedProvider
// over a fresh root) and an admin device.
func newContentRig(t *testing.T, wrap func(api.FilesProvider) api.FilesProvider) *contentRig {
	t.Helper()
	r := newRig(t, rigOptions{})
	root := t.TempDir()
	set, err := rootfs.New([]string{root}, rootfs.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = set.Close() })
	var prov api.FilesProvider = jailedProvider{set}
	if wrap != nil {
		prov = wrap(prov)
	}
	sys, err := system.New(system.Config{
		Version: testVersion, APIVersion: testAPIVer, Started: startedAt, Fingerprint: testFingerprint,
		Port: "8765", Settings: r.set, Disk: fakeDisk{}, Network: fakeNet{}, Counts: r.db, Host: fakeHost{},
	})
	if err != nil {
		t.Fatal(err)
	}
	h, err := api.New(api.Config{
		Pairing: panicPairing{r.svc}, Devices: r.db, System: sys, Policy: r.set, Logger: r.logs,
		Apps:  apps.New(apps.Config{Lister: fakeLister{}, Launcher: r.launcher}),
		Files: prov,
	})
	if err != nil {
		t.Fatal(err)
	}
	r.h = h
	admin, _ := r.registerAdmin(newPhone("admin-1", 1))
	return &contentRig{rig: r, root: root, admin: admin}
}

// put writes a file inside the root and returns its path.
func (c *contentRig) put(name string, data []byte) string {
	c.t.Helper()
	p := filepath.Join(c.root, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		c.t.Fatal(err)
	}
	if err := os.WriteFile(p, data, 0o644); err != nil {
		c.t.Fatal(err)
	}
	return p
}

// device pairs a non-admin device with the given grants and jail.
func (c *contentRig) device(id string, seed byte, g authz.Grants, jail string) string {
	c.t.Helper()
	tok, devID := c.pairDevice(newPhone(id, seed))
	if err := c.svc.SetFileGrants(pairing.PC, devID, g); err != nil {
		c.t.Fatal(err)
	}
	if jail != "" {
		if err := c.svc.SetJail(pairing.PC, devID, jail); err != nil {
			c.t.Fatal(err)
		}
	}
	return tok
}

func TestContentServesFileAndRanges(t *testing.T) {
	c := newContentRig(t, nil)
	p := c.put("sub/note.txt", []byte(contentText))
	get := func(opts ...opt) result { return c.do(http.MethodGet, "/content"+q("path", p), c.admin, nil, opts...) }

	res := get()
	if res.Code != http.StatusOK || res.Body.String() != contentText {
		t.Fatalf("full = %d %q", res.Code, res.Body.String())
	}
	h := res.Header()
	if !strings.HasPrefix(h.Get("Content-Disposition"), "attachment") || !strings.Contains(h.Get("Content-Disposition"), "note.txt") ||
		h.Get("X-Content-Type-Options") != "nosniff" || h.Get("Cache-Control") != "no-store" ||
		!strings.Contains(h.Get("Content-Security-Policy"), "sandbox") || h.Get("Accept-Ranges") != "bytes" {
		t.Fatalf("download headers = %v", h)
	}
	if strings.Contains(h.Get("Content-Disposition"), c.root) {
		t.Fatalf("the disposition names a host path: %q", h.Get("Content-Disposition"))
	}

	res = get(withHeader(rangeHeader, rangeRequest))
	if res.Code != http.StatusPartialContent || res.Body.String() != rangeBody || res.Header().Get(headerRange) != rangeResponse {
		t.Fatalf("range = %d %q %q", res.Code, res.Body.String(), res.Header().Get(headerRange))
	}
	res = get(withHeader(rangeHeader, rangeSuffix))
	if res.Code != http.StatusPartialContent || res.Body.String() != rangeSuffixOut {
		t.Fatalf("suffix range = %d %q", res.Code, res.Body.String())
	}
	res = get(withHeader(rangeHeader, rangeTooFar))
	if res.Code != http.StatusRequestedRangeNotSatisfiable || res.Header().Get(headerRange) != rangeUnmet {
		t.Fatalf("range past the end = %d, Content-Range %q", res.Code, res.Header().Get(headerRange))
	}
}

func TestContentRefusals(t *testing.T) {
	c := newContentRig(t, nil)
	c.put("a.txt", []byte(contentText))
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(c.root, "escape")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.Mkdir(filepath.Join(c.root, "folder"), 0o755); err != nil {
		t.Fatal(err)
	}
	get := func(path string) result { return c.do(http.MethodGet, "/content"+q("path", path), c.admin, nil) }

	get(filepath.Join(c.root, "none.txt")).wantError(t, http.StatusNotFound, api.CodePathNotFound)
	get(filepath.Join(c.root, "folder")).wantError(t, http.StatusUnprocessableEntity, api.CodeNotAFile)
	get(outside).wantError(t, http.StatusForbidden, authz.CodeForbidden)
	get(filepath.Join(c.root, "..", filepath.Base(filepath.Dir(outside)), "outside.txt")).wantError(t, http.StatusForbidden, authz.CodeForbidden)
	res := get(filepath.Join(c.root, "escape"))
	res.wantError(t, http.StatusForbidden, authz.CodeForbidden)
	if strings.Contains(res.Body.String(), "secret") {
		t.Fatalf("a link out of the root leaked content: %s", res.Body.String())
	}
	c.do(http.MethodGet, "/content", c.admin, nil).wantError(t, http.StatusBadRequest, api.CodeBadRequest)
	c.do(http.MethodGet, "/content"+q("path", filepath.Join(c.root, "a.txt")), "", nil).wantError(t, http.StatusUnauthorized, authz.CodeUnauthorized)
}

func TestContentNeedsTheDownloadGrantAndRespectsTheJail(t *testing.T) {
	c := newContentRig(t, nil)
	c.put("jail/in.txt", []byte(contentText))
	c.put("out.txt", []byte(contentText))
	in, out := filepath.Join(c.root, "jail", "in.txt"), filepath.Join(c.root, "out.txt")
	get := func(tok, path string, opts ...opt) result {
		return c.do(http.MethodGet, "/content"+q("path", path), tok, nil, opts...)
	}

	browseOnly := c.device("phone-1", 2, authz.Grants{Browse: true}, "")
	get(browseOnly, in).wantError(t, http.StatusForbidden, authz.CodeCapabilityDenied)
	get(browseOnly, in, withHeader(rangeHeader, rangeRequest)).wantError(t, http.StatusForbidden, authz.CodeCapabilityDenied)

	downloader := c.device("phone-2", 3, authz.Grants{Download: true}, "")
	if res := get(downloader, out); res.Code != http.StatusOK || res.Body.String() != contentText {
		t.Fatalf("download with the grant = %d %s", res.Code, res.Body.String())
	}

	jailed := c.device("phone-3", 4, authz.Grants{Download: true}, filepath.Join(c.root, "jail"))
	if res := get(jailed, in); res.Code != http.StatusOK || res.Body.String() != contentText {
		t.Fatalf("download inside the jail = %d %s", res.Code, res.Body.String())
	}
	get(jailed, out).wantError(t, http.StatusForbidden, authz.CodeForbidden)
	get(jailed, filepath.Join(c.root, "jail", "..", "out.txt")).wantError(t, http.StatusForbidden, authz.CodeForbidden)
}

// zipOf makes a zip with the given file names and bodies.
func zipOf(t *testing.T, contents map[string]string, order []string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, name := range order {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(contents[name])); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

type archiveListing struct {
	Entries []api.ArchiveEntry `json:"entries"`
}

func TestArchiveListingOfAFolderWithASymlink(t *testing.T) {
	c := newContentRig(t, nil)
	c.put("pack/a.txt", []byte("alpha"))
	c.put("pack/sub/b.txt", []byte("beta!"))
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(c.root, "pack", "ln")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	zipPath := filepath.Join(c.root, "out.zip")
	res := c.do(http.MethodPost, "/fs/compress", c.admin, map[string]any{"sources": []string{filepath.Join(c.root, "pack")}, "dest": zipPath})
	if res.Code != http.StatusCreated {
		t.Fatalf("compress = %d %s", res.Code, res.Body.String())
	}

	res = c.do(http.MethodGet, "/fs/archive"+q("path", zipPath), c.admin, nil)
	if res.Code != http.StatusOK {
		t.Fatalf("archive = %d %s", res.Code, res.Body.String())
	}
	var got archiveListing
	res.json(t, &got)
	// Read the zip back independently and compare name, size and kind.
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	if len(got.Entries) != len(zr.File) || len(got.Entries) == 0 {
		t.Fatalf("listing has %d entries, the zip has %d: %+v", len(got.Entries), len(zr.File), got.Entries)
	}
	for i, zf := range zr.File {
		e := got.Entries[i]
		if e.Path != zf.Name || e.Size != int64(zf.UncompressedSize64) || e.IsDir != zf.FileInfo().IsDir() {
			t.Fatalf("entry %d = %+v, zip has %q size %d dir %v", i, e, zf.Name, zf.UncompressedSize64, zf.FileInfo().IsDir())
		}
		if strings.HasSuffix(e.Path, "/ln") {
			t.Fatalf("the link was archived: %+v", e)
		}
	}
	by := map[string]int64{}
	for _, e := range got.Entries {
		by[e.Path] = e.Size
	}
	if by["pack/a.txt"] != 5 || by["pack/sub/b.txt"] != 5 {
		t.Fatalf("sizes = %v", by)
	}
}

func TestArchiveListingLimitAndErrors(t *testing.T) {
	c := newContentRig(t, nil)
	names := []string{"a", "b", "c", "d"}
	body := map[string]string{"a": "1", "b": "22", "c": "333", "d": "4444"}
	zp := c.put("l.zip", zipOf(t, body, names))
	c.put("plain.txt", []byte("text"))
	c.put("empty.tar", make([]byte, 1024)) // two zero blocks: a valid, empty tar
	c.put("bad.zip", []byte("not a zip"))
	if err := os.Mkdir(filepath.Join(c.root, "d.zip"), 0o755); err != nil {
		t.Fatal(err)
	}
	list := func(path string, kv ...string) result {
		return c.do(http.MethodGet, "/fs/archive"+q(append([]string{"path", path}, kv...)...), c.admin, nil)
	}

	var got archiveListing
	list(zp, "limit", "2").json(t, &got)
	if len(got.Entries) != 2 || got.Entries[1].Path != "b" || got.Entries[1].Size != 2 {
		t.Fatalf("limit 2 = %+v", got.Entries)
	}
	for _, limit := range []string{"0", "-5", "abc", "99999999999999999999"} {
		res := list(zp, "limit", limit)
		got = archiveListing{}
		res.json(t, &got)
		if res.Code != http.StatusOK || len(got.Entries) != len(names) {
			t.Fatalf("limit %q = %d, %d entries", limit, res.Code, len(got.Entries))
		}
	}
	if res := list(filepath.Join(c.root, "empty.tar")); res.Code != http.StatusOK || strings.TrimSpace(res.Body.String()) != `{"entries":[]}` {
		t.Fatalf("empty archive = %d %s", res.Code, res.Body.String())
	}
	list(filepath.Join(c.root, "plain.txt")).wantError(t, http.StatusBadRequest, api.CodeUnsupported)
	list(filepath.Join(c.root, "d.zip")).wantError(t, http.StatusBadRequest, api.CodeUnsupported)
	list(filepath.Join(c.root, "bad.zip")).wantError(t, http.StatusBadRequest, api.CodeBadRequest)
	list(filepath.Join(c.root, "none.zip")).wantError(t, http.StatusNotFound, api.CodePathNotFound)
	c.do(http.MethodGet, "/fs/archive", c.admin, nil).wantError(t, http.StatusBadRequest, api.CodeBadRequest)
}

func TestArchiveListingNeedsBrowseAndStaysInTheJail(t *testing.T) {
	c := newContentRig(t, nil)
	data := zipOf(t, map[string]string{"x": "1"}, []string{"x"})
	in := c.put("jail/in.zip", data)
	out := c.put("out.zip", data)
	outside := filepath.Join(t.TempDir(), "o.zip")
	if err := os.WriteFile(outside, data, 0o644); err != nil {
		t.Fatal(err)
	}
	list := func(tok, path string) result { return c.do(http.MethodGet, "/fs/archive"+q("path", path), tok, nil) }

	noBrowse := c.device("phone-1", 2, authz.Grants{Download: true}, "")
	list(noBrowse, in).wantError(t, http.StatusForbidden, authz.CodeCapabilityDenied)

	jailed := c.device("phone-2", 3, authz.Grants{Browse: true}, filepath.Join(c.root, "jail"))
	if res := list(jailed, in); res.Code != http.StatusOK {
		t.Fatalf("list inside the jail = %d %s", res.Code, res.Body.String())
	}
	list(jailed, out).wantError(t, http.StatusForbidden, authz.CodeForbidden)
	list(c.admin, outside).wantError(t, http.StatusForbidden, authz.CodeForbidden)
}

func TestContentRoutesAnswer501WithoutAReadService(t *testing.T) {
	c := newContentRig(t, func(p api.FilesProvider) api.FilesProvider {
		svc, _ := p.For(authz.Principal{}, authz.Policy{})
		return bareProvider{struct{ api.FileService }{svc}}
	})
	p := c.put("a.zip", zipOf(t, map[string]string{"x": "1"}, []string{"x"}))
	c.do(http.MethodGet, "/content"+q("path", p), c.admin, nil).wantError(t, http.StatusNotImplemented, api.CodeNotImplemented)
	c.do(http.MethodGet, "/fs/archive"+q("path", p), c.admin, nil).wantError(t, http.StatusNotImplemented, api.CodeNotImplemented)
}
