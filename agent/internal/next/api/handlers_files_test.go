package api_test

import (
	"crypto/sha256"
	"encoding/hex"
	"net/url"
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

// filesAdapter lets a files.Service serve the api.FileService interface: the
// api package cannot name the files types, so the few methods that return them
// are converted here (the structs have identical fields).
type filesAdapter struct{ *files.Service }

func entryOf(e *files.Entry, err error) (*api.FileEntry, error) {
	if err != nil {
		return nil, err
	}
	out := api.FileEntry(*e)
	return &out, nil
}

func (a filesAdapter) List(path, cursor string, limit int) (*api.FileListing, error) {
	l, err := a.Service.List(path, cursor, limit)
	if err != nil {
		return nil, err
	}
	out := &api.FileListing{Path: l.Path, NextCursor: l.NextCursor}
	for _, e := range l.Entries {
		out.Entries = append(out.Entries, api.FileEntry(e))
	}
	return out, nil
}
func (a filesAdapter) Meta(p string) (*api.FileEntry, error) { return entryOf(a.Service.Meta(p)) }
func (a filesAdapter) CreateFolder(p string) (*api.FileEntry, error) {
	return entryOf(a.Service.CreateFolder(p))
}
func (a filesAdapter) CreateFile(p string) (*api.FileEntry, error) {
	return entryOf(a.Service.CreateFile(p))
}
func (a filesAdapter) WriteContent(p string, d []byte, b *time.Time) (*api.FileEntry, error) {
	return entryOf(a.Service.WriteContent(p, d, b))
}
func (a filesAdapter) Rename(s, d string) (*api.FileEntry, error) {
	return entryOf(a.Service.Rename(s, d))
}
func (a filesAdapter) Compress(s []string, d string) (*api.FileEntry, error) {
	return entryOf(a.Service.Compress(s, d))
}
func (a filesAdapter) Extract(arc, d string, o bool) (*api.FileEntry, error) {
	return entryOf(a.Service.Extract(arc, d, o))
}
func (a filesAdapter) Drives() ([]api.FileDrive, error) {
	ds, err := a.Service.Drives()
	var out []api.FileDrive
	for _, d := range ds {
		out = append(out, api.FileDrive(d))
	}
	return out, err
}
func (a filesAdapter) ListTrash() ([]api.TrashItem, error) {
	ts, err := a.Service.ListTrash()
	var out []api.TrashItem
	for _, t := range ts {
		out = append(out, api.TrashItem(t))
	}
	return out, err
}
func (a filesAdapter) ValidTrashID(id string) bool { return files.ValidTrashID(id) }

// recordingProvider serves one service and notes who asked.
type recordingProvider struct {
	svc  api.FileService
	ro   api.FileService // the view of the same folders for a read-only caller
	mu   sync.Mutex
	seen []string
}

// For returns the read-only view when the device or the whole agent is
// read-only, like the composition root does.
func (p *recordingProvider) For(pr authz.Principal, pol authz.Policy) (api.FileService, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.seen = append(p.seen, pr.DeviceID)
	if (pr.ReadOnly || pol.ReadOnly) && p.ro != nil {
		return p.ro, nil
	}
	return p.svc, nil
}

// fileRig is the HTTP stack with a real files.Service over temp folders.
type fileRig struct {
	*rig
	root     string
	trash    string
	provider *recordingProvider
	admin    string
}

// newFileRig builds the stack with a file service over a fresh root folder and
// an admin device; maxBody 0 keeps the default body limit.
func newFileRig(t *testing.T, maxBody int64) *fileRig {
	t.Helper()
	r := newRig(t, rigOptions{})
	root := t.TempDir()
	set, err := rootfs.New([]string{root}, rootfs.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = set.Close() })
	svc := files.New(set)
	trash := filepath.Join(t.TempDir(), "Trash")
	if err := svc.WithTrash(trash); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	roSvc := files.New(set.ReadOnly())
	if err := roSvc.WithTrash(trash); err != nil {
		t.Fatal(err)
	}
	prov := &recordingProvider{svc: filesAdapter{svc}, ro: filesAdapter{roSvc}}
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
		Files: prov, MaxBodyBytes: maxBody,
	})
	if err != nil {
		t.Fatal(err)
	}
	r.h = h
	admin, _ := r.registerAdmin(newPhone("admin-1", 1))
	return &fileRig{rig: r, root: root, trash: trash, provider: prov, admin: admin}
}

// path is the absolute path of name inside the root.
func (f *fileRig) path(name string) string { return filepath.Join(f.root, name) }

// put writes a file inside the root and returns its path.
func (f *fileRig) put(name, content string) string {
	f.t.Helper()
	p := f.path(name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		f.t.Fatal(err)
	}
	return p
}

// q builds a query string of key/value pairs.
func q(kv ...string) string {
	v := url.Values{}
	for i := 0; i+1 < len(kv); i += 2 {
		v.Set(kv[i], kv[i+1])
	}
	return "?" + v.Encode()
}

// batch decodes a {"results":[...]} answer.
func batch(t *testing.T, res result) []api.ItemResult {
	t.Helper()
	var b struct {
		Results []api.ItemResult `json:"results"`
	}
	res.json(t, &b)
	return b.Results
}

func TestFilesBrowseAndMeta(t *testing.T) {
	f := newFileRig(t, 0)
	f.put("a.txt", "hello")
	f.put("b.txt", "world")
	if err := os.Mkdir(f.path("empty"), 0o755); err != nil {
		t.Fatal(err)
	}

	res := f.do("GET", "/fs"+q("path", f.root), f.admin, nil)
	var l struct {
		Path       string `json:"path"`
		Entries    []struct{ Name string }
		NextCursor *string `json:"nextCursor"`
	}
	res.json(t, &l)
	if res.Code != 200 || len(l.Entries) != 3 || l.NextCursor != nil || l.Path != f.root {
		t.Fatalf("list = %d %s", res.Code, res.Body.String())
	}

	// Paging: one entry at a time, resuming from the cursor.
	res = f.do("GET", "/fs"+q("path", f.root, "limit", "1"), f.admin, nil)
	res.json(t, &l)
	if len(l.Entries) != 1 || l.NextCursor == nil || l.Entries[0].Name != "a.txt" {
		t.Fatalf("page 1 = %s", res.Body.String())
	}
	res = f.do("GET", "/fs"+q("path", f.root, "limit", "1", "cursor", *l.NextCursor), f.admin, nil)
	res.json(t, &l)
	if len(l.Entries) != 1 || l.Entries[0].Name != "b.txt" {
		t.Fatalf("page 2 = %s", res.Body.String())
	}

	// An empty folder lists as [] and not null.
	res = f.do("GET", "/fs"+q("path", f.path("empty")), f.admin, nil)
	if !strings.Contains(res.Body.String(), `"entries":[]`) {
		t.Fatalf("empty folder = %s", res.Body.String())
	}

	var e struct {
		Name  string `json:"name"`
		Size  int64  `json:"size"`
		IsDir bool   `json:"isDir"`
	}
	res = f.do("GET", "/fs/meta"+q("path", f.path("a.txt")), f.admin, nil)
	res.json(t, &e)
	if res.Code != 200 || e.Name != "a.txt" || e.Size != 5 || e.IsDir {
		t.Fatalf("meta = %d %s", res.Code, res.Body.String())
	}

	f.do("GET", "/fs", f.admin, nil).wantError(t, 400, api.CodeBadRequest)
	f.do("GET", "/fs"+q("path", f.path("a.txt")), f.admin, nil).wantError(t, 400, api.CodeBadRequest) // a file, not a folder
	f.do("GET", "/fs"+q("path", f.path("nope")), f.admin, nil).wantError(t, 404, api.CodePathNotFound)
	f.do("GET", "/fs/meta", f.admin, nil).wantError(t, 400, api.CodeBadRequest)

	// Outside the allowed folder: refused, and the answer leaks no host path.
	outside := t.TempDir()
	res = f.do("GET", "/fs"+q("path", outside), f.admin, nil)
	res.wantError(t, 403, authz.CodeForbidden)
	if strings.Contains(res.Body.String(), outside) || strings.Contains(res.Body.String(), f.root) {
		t.Fatalf("error leaks a host path: %s", res.Body.String())
	}
	if len(f.provider.seen) == 0 {
		t.Fatal("the provider was never asked for the caller's service")
	}
}

func TestFilesCreateRenameCopyMove(t *testing.T) {
	f := newFileRig(t, 0)
	f.put("src/one.txt", "1")

	res := f.do("POST", "/fs/folder", f.admin, map[string]string{"path": f.path("made")})
	if res.Code != 201 || !strings.Contains(res.Body.String(), `"isDir":true`) {
		t.Fatalf("folder = %d %s", res.Code, res.Body.String())
	}
	f.do("POST", "/fs/folder", f.admin, map[string]string{"path": f.path("made")}).wantError(t, 409, api.CodeConflict)
	f.do("POST", "/fs/folder", f.admin, map[string]string{"path": ""}).wantError(t, 400, api.CodeBadRequest)
	f.do("POST", "/fs/folder", f.admin, "not json").wantError(t, 400, api.CodeBadRequest)
	if res := f.do("POST", "/fs/file", f.admin, map[string]string{"path": f.path("made/new.txt")}); res.Code != 201 {
		t.Fatalf("file = %d %s", res.Code, res.Body.String())
	}

	res = f.do("PATCH", "/fs/rename", f.admin, map[string]string{"src": f.path("made/new.txt"), "dst": f.path("made/renamed.txt")})
	if res.Code != 200 || !strings.Contains(res.Body.String(), `"name":"renamed.txt"`) {
		t.Fatalf("rename = %d %s", res.Code, res.Body.String())
	}
	f.do("PATCH", "/fs/rename", f.admin, map[string]string{"src": f.path("made/gone"), "dst": f.path("made/x")}).wantError(t, 404, api.CodePathNotFound)
	f.do("PATCH", "/fs/rename", f.admin, map[string]string{"src": f.path("made")}).wantError(t, 400, api.CodeBadRequest)

	// Copy: one result per source; a missing source fails alone.
	res = f.do("POST", "/fs/copy", f.admin, map[string]any{
		"sources": []string{f.path("src/one.txt"), f.path("src/missing.txt")}, "destDir": f.path("made"),
	})
	rs := batch(t, res)
	if res.Code != 202 || len(rs) != 2 || !rs[0].OK || rs[1].OK || rs[1].Error == nil || rs[1].Error.Code != api.CodePathNotFound {
		t.Fatalf("copy = %d %s", res.Code, res.Body.String())
	}
	if _, err := os.Stat(f.path("made/one.txt")); err != nil {
		t.Fatalf("copied file missing: %v", err)
	}
	// Copying again without duplicate/overwrite conflicts on that item.
	rs = batch(t, f.do("POST", "/fs/copy", f.admin, map[string]any{"sources": []string{f.path("src/one.txt")}, "destDir": f.path("made")}))
	if len(rs) != 1 || rs[0].OK || rs[0].Error.Code != api.CodeConflict {
		t.Fatalf("second copy = %+v", rs)
	}

	res = f.do("POST", "/fs/move", f.admin, map[string]any{"sources": []string{f.path("src/one.txt")}, "destDir": f.path("made"), "duplicate": true})
	rs = batch(t, res)
	if res.Code != 202 || len(rs) != 1 || !rs[0].OK {
		t.Fatalf("move = %d %s", res.Code, res.Body.String())
	}
	if _, err := os.Stat(f.path("src/one.txt")); !os.IsNotExist(err) {
		t.Fatalf("source still there after move: %v", err)
	}
	f.do("POST", "/fs/move", f.admin, map[string]any{"sources": []string{}, "destDir": f.root}).wantError(t, 400, api.CodeBadRequest)
}

func TestFilesCopyOverwriteNeedsDeleteGrant(t *testing.T) {
	f := newFileRig(t, 0)
	f.put("a.txt", "new")
	f.put("dst/a.txt", "old")
	tok, id := f.pairDevice(newPhone("phone-1", 2))
	if res := f.do("PATCH", "/devices/"+id, f.admin, map[string]any{"modify": true}); res.Code != 200 {
		t.Fatalf("grant = %d %s", res.Code, res.Body.String())
	}
	body := map[string]any{"sources": []string{f.path("a.txt")}, "destDir": f.path("dst"), "overwrite": true}
	f.do("POST", "/fs/copy", tok, body).wantError(t, 403, authz.CodeCapabilityDenied)
	if b, _ := os.ReadFile(f.path("dst/a.txt")); string(b) != "old" {
		t.Fatalf("a refused overwrite changed the file: %q", b)
	}
	// Moving also removes the source name, so it needs delete (route table).
	f.do("POST", "/fs/move", tok, map[string]any{"sources": []string{f.path("a.txt")}, "destDir": f.path("dst")}).wantError(t, 403, authz.CodeCapabilityDenied)
	if res := f.do("PATCH", "/devices/"+id, f.admin, map[string]any{"delete": true}); res.Code != 200 {
		t.Fatalf("grant = %d %s", res.Code, res.Body.String())
	}
	rs := batch(t, f.do("POST", "/fs/copy", tok, body))
	if len(rs) != 1 || !rs[0].OK {
		t.Fatalf("overwrite with the delete grant = %+v", rs)
	}
	if b, _ := os.ReadFile(f.path("dst/a.txt")); string(b) != "new" {
		t.Fatalf("overwrite did not replace: %q", b)
	}
}

func TestFilesDeleteAndTrash(t *testing.T) {
	f := newFileRig(t, 0)
	keep := f.put("keep.txt", "k")
	gone := f.put("gone.txt", "g")
	hard := f.put("hard.txt", "h")

	// Default delete goes to the trash, per item.
	res := f.do("DELETE", "/fs"+q("path", gone), f.admin, map[string]any{"paths": []string{f.path("nope.txt")}})
	rs := batch(t, res)
	if res.Code != 200 || len(rs) != 2 || !rs[0].OK || rs[1].OK || rs[1].Error.Code != api.CodePathNotFound {
		t.Fatalf("delete = %d %s", res.Code, res.Body.String())
	}
	f.do("DELETE", "/fs", f.admin, nil).wantError(t, 400, api.CodeBadRequest)

	var tl struct {
		Items []struct {
			ID           string `json:"id"`
			OriginalPath string `json:"originalPath"`
		} `json:"items"`
	}
	res = f.do("GET", "/trash", f.admin, nil)
	res.json(t, &tl)
	if res.Code != 200 || len(tl.Items) != 1 || tl.Items[0].OriginalPath != gone {
		t.Fatalf("trash = %d %s", res.Code, res.Body.String())
	}
	id := tl.Items[0].ID

	// Restore: a bad id is its own failed item, the good one still restores.
	rs = batch(t, f.do("POST", "/trash/restore", f.admin, map[string][]string{"ids": {"../x", id}}))
	if len(rs) != 2 || rs[0].OK || rs[0].Error.Code != api.CodeBadRequest || !rs[1].OK || rs[1].Path != gone {
		t.Fatalf("restore = %+v", rs)
	}
	f.do("POST", "/trash/restore", f.admin, map[string][]string{"ids": {}}).wantError(t, 400, api.CodeBadRequest)

	// Permanent delete leaves nothing in the trash.
	rs = batch(t, f.do("DELETE", "/fs"+q("path", hard, "permanent", "true"), f.admin, nil))
	if len(rs) != 1 || !rs[0].OK {
		t.Fatalf("permanent delete = %+v", rs)
	}
	if _, err := os.Stat(hard); !os.IsNotExist(err) {
		t.Fatalf("permanently deleted file remains: %v", err)
	}

	// Emptying the trash: bad bodies delete nothing, no body empties all.
	f.do("DELETE", "/fs"+q("path", keep), f.admin, nil)
	f.do("DELETE", "/trash", f.admin, `{}`).wantError(t, 400, api.CodeBadRequest)
	f.do("DELETE", "/trash", f.admin, `{"ids":[]}`).wantError(t, 400, api.CodeBadRequest)
	f.do("DELETE", "/trash", f.admin, `nonsense`).wantError(t, 400, api.CodeBadRequest)
	f.do("DELETE", "/trash", f.admin, map[string][]string{"ids": {"a/b"}}).wantError(t, 400, api.CodeBadRequest)
	res = f.do("GET", "/trash", f.admin, nil)
	res.json(t, &tl)
	if len(tl.Items) != 1 {
		t.Fatalf("a refused empty changed the trash: %s", res.Body.String())
	}
	if res := f.do("DELETE", "/trash", f.admin, nil); res.Code != 200 || res.Body.String() != "{\"ok\":true}\n" {
		t.Fatalf("empty = %d %s", res.Code, res.Body.String())
	}
	res = f.do("GET", "/trash", f.admin, nil)
	if res.Body.String() != "{\"items\":[]}\n" {
		t.Fatalf("trash after empty = %s", res.Body.String())
	}
	// An id the trash does not know is reported with its own code.
	f.do("DELETE", "/trash", f.admin, map[string][]string{"ids": {"unknown-id"}}).wantError(t, 404, api.CodePathNotFound)
}

func TestFilesChecksums(t *testing.T) {
	f := newFileRig(t, 0)
	p := f.put("a.txt", "hello")
	sum := sha256.Sum256([]byte("hello"))
	want := hex.EncodeToString(sum[:])

	res := f.do("GET", "/fs/checksum"+q("path", p), f.admin, nil)
	if res.Code != 200 || !strings.Contains(res.Body.String(), want) || !strings.Contains(res.Body.String(), `"algorithm":"sha256"`) {
		t.Fatalf("checksum = %d %s", res.Code, res.Body.String())
	}
	f.do("GET", "/fs/checksum"+q("path", p, "algo", "crc32"), f.admin, nil).wantError(t, 400, api.CodeBadRequest)
	f.do("GET", "/fs/checksum"+q("path", f.root), f.admin, nil).wantError(t, 400, api.CodeBadRequest) // a folder
	f.do("GET", "/fs/checksum", f.admin, nil).wantError(t, 400, api.CodeBadRequest)

	res = f.do("POST", "/fs/checksums", f.admin, map[string]any{"paths": []string{p, f.path("missing"), f.root, p}, "algo": "sha256"})
	var out struct {
		Checksums []struct{ Path, Hash, Error string } `json:"checksums"`
	}
	res.json(t, &out)
	if res.Code != 200 || len(out.Checksums) != 4 {
		t.Fatalf("checksums = %d %s", res.Code, res.Body.String())
	}
	c := out.Checksums
	if c[0].Hash != want || c[3].Hash != want || c[1].Error == "" || c[1].Hash != "" || c[2].Error == "" || c[1].Path != f.path("missing") {
		t.Fatalf("checksums = %s", res.Body.String())
	}
	if strings.Contains(c[1].Error, f.root) || strings.Contains(c[2].Error, f.root) {
		t.Fatalf("per-item error leaks a host path: %q", c[1].Error)
	}
	f.do("POST", "/fs/checksums", f.admin, map[string]any{"paths": []string{}}).wantError(t, 400, api.CodeBadRequest)
	f.do("POST", "/fs/checksums", f.admin, map[string]any{"paths": []string{p}, "algo": "x"}).wantError(t, 400, api.CodeBadRequest)
	many := make([]string, 1001)
	for i := range many {
		many[i] = p
	}
	f.do("POST", "/fs/checksums", f.admin, map[string]any{"paths": many}).wantError(t, 400, api.CodeBadRequest)

	// Hashing needs the download grant: a browse-only device is refused.
	tok, _ := f.pairDevice(newPhone("phone-1", 2))
	f.do("GET", "/fs/checksum"+q("path", p), tok, nil).wantError(t, 403, authz.CodeCapabilityDenied)
}

func TestFilesCompressExtractRoundTrip(t *testing.T) {
	f := newFileRig(t, 0)
	f.put("pack/a.txt", "alpha")
	f.put("pack/sub/b.txt", "beta")
	zip := f.path("out.zip")

	res := f.do("POST", "/fs/compress", f.admin, map[string]any{"sources": []string{f.path("pack")}, "dest": zip})
	if res.Code != 201 || !strings.Contains(res.Body.String(), `"name":"out.zip"`) {
		t.Fatalf("compress = %d %s", res.Code, res.Body.String())
	}
	f.do("POST", "/fs/compress", f.admin, map[string]any{"sources": []string{}, "dest": zip}).wantError(t, 400, api.CodeBadRequest)

	res = f.do("POST", "/fs/extract", f.admin, map[string]any{"archive": zip, "destDir": f.path("unpacked")})
	if res.Code != 200 {
		t.Fatalf("extract = %d %s", res.Code, res.Body.String())
	}
	if b, err := os.ReadFile(f.path("unpacked/pack/sub/b.txt")); err != nil || string(b) != "beta" {
		t.Fatalf("extracted content = %q, %v", b, err)
	}
	f.do("POST", "/fs/extract", f.admin, map[string]any{"archive": f.path("pack/a.txt"), "destDir": f.path("u2")}).wantError(t, 400, api.CodeUnsupported)
	f.do("POST", "/fs/extract", f.admin, map[string]any{"archive": zip}).wantError(t, 400, api.CodeBadRequest)
}

func TestFilesContentPut(t *testing.T) {
	f := newFileRig(t, 6<<20)
	p := f.put("note.txt", "old")
	info, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	when := info.ModTime().UTC().Format(time.RFC3339)

	res := f.do("PUT", "/content"+q("path", p, "baseModified", when), f.admin, "new text")
	if res.Code != 200 || !strings.Contains(res.Body.String(), `"size":8`) {
		t.Fatalf("put = %d %s", res.Code, res.Body.String())
	}
	if b, _ := os.ReadFile(p); string(b) != "new text" {
		t.Fatalf("content = %q", b)
	}
	// The file changed since the read time: a stale write is refused.
	stale := info.ModTime().Add(-time.Hour).UTC().Format(time.RFC3339)
	f.do("PUT", "/content"+q("path", p, "baseModified", stale), f.admin, "lost").wantError(t, 409, api.CodeStaleWrite)
	if b, _ := os.ReadFile(p); string(b) != "new text" {
		t.Fatalf("a stale write changed the file: %q", b)
	}
	f.do("PUT", "/content"+q("path", p, "baseModified", "yesterday"), f.admin, "x").wantError(t, 400, api.CodeBadRequest)
	f.do("PUT", "/content", f.admin, "x").wantError(t, 400, api.CodeBadRequest)
	f.do("PUT", "/content"+q("path", f.path("fresh.txt")), f.admin, "x").wantStatus(t, 200) // unconditional write creates new content (the service also creates missing parents)
	// Over 5 MiB (the route's own cap, below the server limit set here).
	f.do("PUT", "/content"+q("path", p), f.admin, strings.Repeat("x", 5<<20+1)).wantError(t, 413, api.CodePayloadTooLarge)
	if b, _ := os.ReadFile(p); string(b) != "new text" {
		t.Fatalf("an oversized write changed the file: %d bytes", len(b))
	}
}

func TestFilesDrivesAndWriteGuards(t *testing.T) {
	f := newFileRig(t, 0)
	res := f.do("GET", "/system/drives", f.admin, nil)
	if res.Code != 200 || !strings.HasPrefix(res.Body.String(), "[") {
		t.Fatalf("drives = %d %s", res.Code, res.Body.String())
	}

	// A browse-only device may list but not create; read-only mode refuses writes.
	tok, _ := f.pairDevice(newPhone("phone-1", 2))
	f.do("GET", "/fs"+q("path", f.root), tok, nil).wantStatus(t, 200)
	f.do("POST", "/fs/folder", tok, map[string]string{"path": f.path("x")}).wantError(t, 403, authz.CodeCapabilityDenied)
	f.set.setReadOnly(true)
	f.do("POST", "/fs/folder", f.admin, map[string]string{"path": f.path("x")}).wantError(t, 403, authz.CodeReadOnly)
	f.do("DELETE", "/fs"+q("path", f.path("x")), f.admin, nil).wantError(t, 403, authz.CodeReadOnly)
	f.do("GET", "/fs"+q("path", f.root), f.admin, nil).wantStatus(t, 200)
	if _, err := os.Stat(f.path("x")); !os.IsNotExist(err) {
		t.Fatalf("a refused write created something: %v", err)
	}
}

func TestFileRoutesWithoutAServiceAnswer501(t *testing.T) {
	r := newRig(t, rigOptions{})
	tok, _ := r.registerAdmin(newPhone("admin-1", 1))
	r.do("GET", "/fs"+q("path", "/"), tok, nil).wantError(t, 501, api.CodeNotImplemented)
	r.do("GET", "/trash", tok, nil).wantError(t, 501, api.CodeNotImplemented)
	r.do("GET", "/system/drives", tok, nil).wantError(t, 501, api.CodeNotImplemented)
}

// wantStatus fails unless the answer has the given status.
func (res result) wantStatus(t *testing.T, status int) {
	t.Helper()
	if res.Code != status {
		t.Fatalf("status = %d (%s), want %d", res.Code, res.Body.String(), status)
	}
}
