package share

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/next/api"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/authz"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/rootfs"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/store"
)

// testStart is the fake clock's first instant, a whole second so expiry maths
// in the tests is exact.
var testStart = time.Unix(1_800_000_000, 0)

// testIP is the requester address most tests fetch from.
const testIP = "192.0.2.1"

// fakeClock is a settable clock safe for concurrent use.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// env is a Service over a temp folder, a temp database and a fake clock.
type env struct {
	t       *testing.T
	root    string
	db      *store.DB
	svc     *Service
	clock   *fakeClock
	enabled atomic.Bool
}

// newEnv builds an env whose database holds the given accounts and devices.
func newEnv(t *testing.T, users []string, devices ...store.Device) *env {
	t.Helper()
	e := &env{t: t, root: t.TempDir(), clock: &fakeClock{t: testStart}}
	e.enabled.Store(true)
	db, err := store.Open(filepath.Join(t.TempDir(), "agent-v3.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	var data store.ImportData
	for _, u := range users {
		data.Users = append(data.Users, store.User{Username: u, PasswordHash: "x", Created: 1})
	}
	for _, d := range devices {
		if d.JailRoot != "" {
			d.JailRoot = filepath.Join(e.root, d.JailRoot)
		}
		data.Devices = append(data.Devices, d)
	}
	if err := db.ImportAll(data); err != nil {
		t.Fatal(err)
	}
	set, err := rootfs.New([]string{e.root}, rootfs.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = set.Close() })
	e.db = db
	e.svc = New(Config{Set: set, Store: db, Sharing: e.enabled.Load, Now: e.clock.now})
	return e
}

// device returns a code-paired device with the three grants a share needs.
func device(id string) store.Device {
	return store.Device{ID: id, Label: "label-" + id, TokenHash: "token-hash-" + id, Origin: store.OriginCode,
		CanBrowse: true, CanDownload: true, CanShare: true}
}

// jailName is the folder under the env's root that a jailed test device is confined to.
const jailName = "jail"

// caller is the Caller a request from d would carry (a jail must already be an absolute path).
func caller(d store.Device) Caller { return Caller{Principal: principalOf(&d), Label: d.Label} }

// caller is like the package-level caller but resolves a jail named relative to the env's root.
func (e *env) caller(d store.Device) Caller {
	if d.JailRoot != "" && !filepath.IsAbs(d.JailRoot) {
		d.JailRoot = filepath.Join(e.root, d.JailRoot)
	}
	return caller(d)
}

// file writes content to name under the env's folder and returns its path.
func (e *env) file(name, content string) string {
	e.t.Helper()
	p := filepath.Join(e.root, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		e.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		e.t.Fatal(err)
	}
	return p
}

// mint mints a link for d and fails the test on error.
func (e *env) mint(d store.Device, path string, seconds int64) *Minted {
	e.t.Helper()
	m, err := e.svc.Mint(caller(d), MintRequest{Path: path, ExpiresInSeconds: seconds})
	if err != nil {
		e.t.Fatalf("Mint(%s): %v", path, err)
	}
	return m
}

// plant stores a link for a token the test chose, as if minted earlier by owner.
func (e *env) plant(token, path, owner string, ttl time.Duration) string {
	e.t.Helper()
	hash := store.HashToken(token)
	l := store.ShareLink{TokenHash: hash, Path: path, DeviceID: owner, Created: testStart.Unix(), Expires: testStart.Add(ttl).Unix()}
	if err := e.db.InsertShareLink(l, store.ShareEvent{TokenHash: hash, Path: path, MintedAt: l.Created, ExpiresAt: l.Expires}); err != nil {
		e.t.Fatal(err)
	}
	return hash
}

// fetch resolves token and, on success, returns the bytes and closes the download.
func (e *env) fetch(token string) (string, error) {
	e.t.Helper()
	d, err := e.svc.Resolve(token, testIP)
	if err != nil {
		return "", err
	}
	defer d.Close()
	b, err := io.ReadAll(d.Content)
	if err != nil {
		e.t.Fatal(err)
	}
	return string(b), nil
}

// consumed reports whether the stored link for hash has been used.
func (e *env) consumed(hash string) bool {
	e.t.Helper()
	l, err := e.db.ShareLinkByHash(hash)
	if err != nil || l == nil {
		e.t.Fatalf("ShareLinkByHash(%s) = %v, %v", hash, l, err)
	}
	return l.ConsumedAt != nil
}

// wantErr fails unless err matches the domain error kind.
func wantErr(t *testing.T, err, kind error) {
	t.Helper()
	if !errors.Is(err, kind) {
		t.Fatalf("error = %v; want one matching %v", err, kind)
	}
}

func TestMintStoresOnlyTheHashOfAHighEntropyToken(t *testing.T) {
	d := device("d1")
	e := newEnv(t, nil, d)
	p := e.file("a.txt", "hello")
	m1 := e.mint(d, p, 0)
	m2 := e.mint(d, p, 0)

	if len(m1.Token) != 2*TokenBytes || strings.Trim(m1.Token, "0123456789abcdef") != "" {
		t.Fatalf("token %q: want %d lower-case hex characters", m1.Token, 2*TokenBytes)
	}
	if m1.Token == m2.Token {
		t.Fatal("two mints returned the same token")
	}
	sum := sha256.Sum256([]byte(m1.Token))
	if m1.TokenHash != hex.EncodeToString(sum[:]) {
		t.Fatalf("TokenHash = %s; want the SHA-256 hex of the token", m1.TokenHash)
	}
	links, _ := e.db.ShareLinks()
	events, _ := e.db.ShareEvents()
	if len(links) != 2 || len(events) != 2 {
		t.Fatalf("stored %d links and %d events; want 2 and 2", len(links), len(events))
	}
	if dump := fmt.Sprintf("%+v %+v", links, events); strings.Contains(dump, m1.Token) || strings.Contains(dump, m2.Token) {
		t.Fatalf("the raw token is stored: %s", dump)
	}
	if links[0].DeviceID != "d1" && links[1].DeviceID != "d1" {
		t.Fatalf("links %+v do not record the minting device", links)
	}
}

func TestMintFailsWithoutRandomnessAndStoresNothing(t *testing.T) {
	d := device("d1")
	e := newEnv(t, nil, d)
	p := e.file("a.txt", "x")
	e.svc.rand = bytes.NewReader([]byte("short"))
	if _, err := e.svc.Mint(caller(d), MintRequest{Path: p}); err == nil {
		t.Fatal("Mint with a failing random source succeeded")
	}
	if links, _ := e.db.ShareLinks(); len(links) != 0 {
		t.Fatalf("a failed mint left %d links", len(links))
	}
}

func TestMintExpiry(t *testing.T) {
	d := device("d1")
	e := newEnv(t, nil, d)
	p := e.file("a.txt", "x")
	maxSeconds := int64(MaxExpiry / time.Second)

	for _, c := range []struct {
		name    string
		seconds int64
		want    time.Duration
	}{
		{"default", 0, DefaultExpiry},
		{"one hour", 3600, time.Hour},
		{"the maximum", maxSeconds, MaxExpiry},
	} {
		m := e.mint(d, p, c.seconds)
		if want := testStart.Add(c.want).Unix(); m.ExpiresAt != want {
			t.Errorf("%s: ExpiresAt = %d; want %d", c.name, m.ExpiresAt, want)
		}
	}
	if DefaultExpiry != 24*time.Hour {
		t.Errorf("DefaultExpiry = %v; the app's default is one day", DefaultExpiry)
	}
	for _, s := range []int64{maxSeconds + 1, -1} {
		_, err := e.svc.Mint(caller(d), MintRequest{Path: p, ExpiresInSeconds: s})
		wantErr(t, err, api.ErrBadRequest)
	}
}

func TestMintRefusals(t *testing.T) {
	full := device("full")
	noBrowse, noDownload, noShare, revoked := device("nb"), device("nd"), device("ns"), device("rv")
	noBrowse.CanBrowse, noDownload.CanDownload, noShare.CanShare, revoked.Revoked = false, false, false, true
	admin := store.Device{ID: "adm", Label: "admin", TokenHash: "th-adm", Origin: store.OriginLogin, Username: "alice"}
	jailed := device("jl")
	jailed.JailRoot = jailName
	e := newEnv(t, []string{"alice"}, full, noBrowse, noDownload, noShare, revoked, admin, jailed)
	p := e.file("a.txt", "x")
	inJail := e.file(jailName+"/in.txt", "x")
	outside := e.file("out.txt", "x")
	dir := filepath.Join(e.root, "folder")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	big := filepath.Join(e.root, "big.bin")
	f, err := os.Create(big)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(MaxFileSize + 1); err != nil {
		t.Fatal(err)
	}
	f.Close()

	mint := func(d store.Device, path string) error {
		_, err := e.svc.Mint(e.caller(d), MintRequest{Path: path})
		return err
	}
	wantErr(t, mint(noBrowse, p), authz.ErrCapabilityDenied)
	wantErr(t, mint(noDownload, p), authz.ErrCapabilityDenied)
	wantErr(t, mint(noShare, p), authz.ErrCapabilityDenied)
	wantErr(t, mint(revoked, p), authz.ErrUnauthorized)
	wantErr(t, mint(full, ""), api.ErrBadRequest)
	wantErr(t, mint(full, filepath.Join(e.root, "missing.txt")), api.ErrPathNotFound)
	wantErr(t, mint(full, dir), api.ErrNotAFile)
	wantErr(t, mint(full, big), api.ErrPayloadTooLarge)
	wantErr(t, mint(full, filepath.Join(t.TempDir(), "elsewhere.txt")), api.ErrForbidden)
	wantErr(t, mint(jailed, outside), api.ErrForbidden)
	if err := mint(jailed, inJail); err != nil {
		t.Fatalf("a file inside the jail: %v", err)
	}
	if err := mint(admin, p); err != nil {
		t.Fatalf("an admin device needs no file grants: %v", err)
	}
	e.enabled.Store(false)
	wantErr(t, mint(full, p), authz.ErrForbidden)
	if links, _ := e.db.ShareLinks(); len(links) != 2 {
		t.Fatalf("%d links stored; want only the two successful mints", len(links))
	}
}

func TestMintReturnsAuditEventAsValue(t *testing.T) {
	d := device("d1")
	e := newEnv(t, nil, d)
	p := e.file("a.txt", "x")
	m := e.mint(d, p, 3600)
	want := []Event{{Action: AuditShareCreated, Actor: "label-d1", Target: m.Path,
		Detail: "expires=" + time.Unix(m.ExpiresAt, 0).UTC().Format(time.RFC3339)}}
	if !reflect.DeepEqual(m.Events, want) {
		t.Fatalf("Events = %+v; want %+v", m.Events, want)
	}
	if !strings.HasSuffix(m.Path, "a.txt") {
		t.Fatalf("Path = %q; want the resolved file path", m.Path)
	}
}

func TestResolveServesOnceAndRecordsHistory(t *testing.T) {
	d := device("d1")
	e := newEnv(t, nil, d)
	m := e.mint(d, e.file("report final.txt", "the bytes"), 0)

	dl, err := e.svc.Resolve(m.Token, testIP)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(dl.Content)
	if string(got) != "the bytes" || dl.Size != int64(len(got)) || dl.Name != "report final.txt" {
		t.Fatalf("download = %q size %d name %q", got, dl.Size, dl.Name)
	}
	if dl.RecordErr != nil {
		t.Fatalf("RecordErr = %v", dl.RecordErr)
	}
	for k, want := range map[string]string{
		"X-Content-Type-Options": "nosniff", "Cache-Control": "no-store",
	} {
		if dl.Header.Get(k) != want {
			t.Errorf("header %s = %q; want %q", k, dl.Header.Get(k), want)
		}
	}
	if cd := dl.Header.Get("Content-Disposition"); !strings.HasPrefix(cd, "attachment") || !strings.Contains(cd, "report final.txt") {
		t.Errorf("Content-Disposition = %q; want an attachment for the file name", cd)
	}
	if !strings.Contains(dl.Header.Get("Content-Security-Policy"), "sandbox") {
		t.Errorf("Content-Security-Policy = %q; want a sandbox policy", dl.Header.Get("Content-Security-Policy"))
	}
	dl.Close()

	_, err = e.svc.Resolve(m.Token, testIP)
	wantErr(t, err, api.ErrNotFound)
	if !e.consumed(m.TokenHash) {
		t.Fatal("link not marked used after a fetch")
	}
	events, _ := e.db.ShareEvents()
	if len(events) != 1 || events[0].ServedAt == nil || *events[0].ServedAt != testStart.Unix() ||
		events[0].RequesterIP == nil || *events[0].RequesterIP != testIP {
		t.Fatalf("history = %+v; want the mint row stamped served by %s", events, testIP)
	}
}

func TestResolveContentIsBoundToTheCheckedSize(t *testing.T) {
	d := device("d1")
	e := newEnv(t, nil, d)
	p := e.file("grow.txt", "12345")
	m := e.mint(d, p, 0)
	dl, err := e.svc.Resolve(m.Token, testIP)
	if err != nil {
		t.Fatal(err)
	}
	defer dl.Close()
	f, err := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("more bytes appended after the check")
	f.Close()
	if got, _ := io.ReadAll(dl.Content); string(got) != "12345" {
		t.Fatalf("served %q; want only the 5 bytes that were checked", got)
	}
}

func TestResolveExpiryBoundary(t *testing.T) {
	d := device("d1")
	e := newEnv(t, nil, d)
	p := e.file("a.txt", "x")
	atLimit := e.mint(d, p, 3600)
	late := e.mint(d, p, 3600)

	e.clock.advance(time.Hour) // the expiry second itself is still valid
	if _, err := e.fetch(atLimit.Token); err != nil {
		t.Fatalf("fetch at the expiry second: %v", err)
	}
	e.clock.advance(time.Second)
	_, err := e.fetch(late.Token)
	wantErr(t, err, api.ErrNotFound)
	if e.consumed(late.TokenHash) {
		t.Fatal("an expired link was marked used")
	}
}

func TestResolveUnknownAndMalformedTokensAreAllTheSameNotFound(t *testing.T) {
	e := newEnv(t, nil, device("d1"))
	for _, tok := range []string{"", "nope", strings.Repeat("0", 2*TokenBytes)} {
		_, err := e.fetch(tok)
		wantErr(t, err, api.ErrNotFound)
		if err.Error() != ErrLinkNotFound.Error() {
			t.Errorf("token %q: message %q differs from the shared not-found text", tok, err)
		}
	}
}

func TestRevokedLinkNoLongerResolves(t *testing.T) {
	d := device("d1")
	e := newEnv(t, nil, d)
	m := e.mint(d, e.file("a.txt", "x"), 0)
	if _, err := e.svc.Revoke(caller(d), m.TokenHash); err != nil {
		t.Fatal(err)
	}
	_, err := e.fetch(m.Token)
	wantErr(t, err, api.ErrNotFound)
}

func TestLinkWorksOnlyWhileOwnerKeepsAllThreeGrants(t *testing.T) {
	noBrowse, noDownload, noShare, revoked := device("nb"), device("nd"), device("ns"), device("rv")
	noBrowse.CanBrowse, noDownload.CanDownload, noShare.CanShare, revoked.Revoked = false, false, false, true
	orphan := store.Device{ID: "orphan", TokenHash: "th-orphan", Origin: store.OriginLogin}
	ok := device("ok")
	e := newEnv(t, nil, noBrowse, noDownload, noShare, revoked, orphan, ok)
	p := e.file("a.txt", "secret")

	for _, c := range []struct{ name, owner string }{
		{"no browse", "nb"}, {"no download", "nd"}, {"no share", "ns"}, {"revoked", "rv"},
		{"admin without account", "orphan"}, {"device removed", "gone"},
	} {
		hash := e.plant("tok-"+c.owner, p, c.owner, time.Hour)
		_, err := e.fetch("tok-" + c.owner)
		if !errors.Is(err, api.ErrNotFound) {
			t.Errorf("%s: fetch error = %v; want not found", c.name, err)
		}
		if l, _ := e.db.ShareLinkByHash(hash); l == nil || l.ConsumedAt != nil {
			t.Errorf("%s: a refused fetch must not use up the link (link %+v)", c.name, l)
		}
	}
	e.plant("tok-ok", p, "ok", time.Hour)
	if got, err := e.fetch("tok-ok"); err != nil || got != "secret" {
		t.Fatalf("owner with all grants: %q, %v", got, err)
	}
}

func TestAdminOwnerNeedsNoFileGrants(t *testing.T) {
	admin := store.Device{ID: "adm", Label: "admin", TokenHash: "th-adm", Origin: store.OriginLogin, Username: "alice"}
	e := newEnv(t, []string{"alice"}, admin)
	e.plant("tok-adm", e.file("a.txt", "admin data"), "adm", time.Hour)
	if got, err := e.fetch("tok-adm"); err != nil || got != "admin data" {
		t.Fatalf("admin-owned link: %q, %v", got, err)
	}
}

func TestOwnerJailAppliesAtFetchTime(t *testing.T) {
	jailed := device("jl")
	jailed.JailRoot = jailName
	e := newEnv(t, nil, jailed)
	jail := filepath.Join(e.root, jailName)
	e.file(jailName+"/in.txt", "inside")
	outside := e.file("outside.txt", "outside")

	e.plant("tok-in", filepath.Join(jail, "in.txt"), "jl", time.Hour)
	if got, err := e.fetch("tok-in"); err != nil || got != "inside" {
		t.Fatalf("a file inside the jail: %q, %v", got, err)
	}
	hash := e.plant("tok-out", outside, "jl", time.Hour)
	_, err := e.fetch("tok-out")
	wantErr(t, err, api.ErrNotFound)
	if e.consumed(hash) {
		t.Fatal("a link refused by the jail was used up")
	}

	link := filepath.Join(jail, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("cannot create a symlink here: %v", err)
	}
	e.plant("tok-link", link, "jl", time.Hour)
	if got, err := e.fetch("tok-link"); err == nil {
		t.Fatalf("a symlink out of the jail served %q", got)
	}
}

func TestLinksImportedFromTheOldAgentKeepWorking(t *testing.T) {
	owner := device("old-dev")
	e := newEnv(t, nil, owner)
	p := e.file("legacy.txt", "from before")
	// The old agent stored hex(sha256(token)); compute it here, not with the code under test.
	sum := sha256.Sum256([]byte("old-token"))
	oldHash := hex.EncodeToString(sum[:])
	sumB := sha256.Sum256([]byte("older-token"))
	l := store.ShareLink{TokenHash: oldHash, Path: p, DeviceID: "old-dev", Created: testStart.Unix(), Expires: testStart.Add(time.Hour).Unix()}
	ownerless := store.ShareLink{TokenHash: hex.EncodeToString(sumB[:]), Path: p, Created: testStart.Unix(), Expires: testStart.Add(time.Hour).Unix()}
	// The import needs an empty database: build a second one carrying the old rows.
	data := store.ImportData{Devices: []store.Device{owner}, ShareLinks: []store.ShareLink{l, ownerless}}
	db, err := store.Open(filepath.Join(t.TempDir(), "imported.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.ImportAll(data); err != nil {
		t.Fatal(err)
	}
	e.db = db
	e.svc.db = db

	if got, err := e.fetch("old-token"); err != nil || got != "from before" {
		t.Fatalf("imported link with an owner: %q, %v", got, err)
	}
	if got, err := e.fetch("older-token"); err != nil || got != "from before" {
		t.Fatalf("imported owner-less link: %q, %v", got, err)
	}
	list, err := e.svc.List(Caller{Principal: authz.Principal{DeviceID: "adm", Admin: true}})
	if err != nil || len(list) != 0 {
		t.Fatalf("List after both were used = %+v, %v; want none", list, err)
	}
}

func TestSharingSwitchOffRefusesWithoutUsingTheLink(t *testing.T) {
	d := device("d1")
	e := newEnv(t, nil, d)
	m := e.mint(d, e.file("a.txt", "x"), 0)
	e.enabled.Store(false)
	_, err := e.fetch(m.Token)
	wantErr(t, err, api.ErrNotFound)
	e.enabled.Store(true)
	if got, err := e.fetch(m.Token); err != nil || got != "x" {
		t.Fatalf("after switching sharing back on: %q, %v", got, err)
	}
}

func TestFileGoneOrChangedAfterMint(t *testing.T) {
	d := device("d1")
	e := newEnv(t, nil, d)

	gone := e.file("gone.txt", "x")
	mGone := e.mint(d, gone, 0)
	os.Remove(gone)
	_, err := e.fetch(mGone.Token)
	wantErr(t, err, api.ErrNotFound)
	if !e.consumed(mGone.TokenHash) {
		t.Error("a fetch that found the file missing leaves the link used (as the old agent did)")
	}

	swapped := e.file("swap.txt", "x")
	mSwap := e.mint(d, swapped, 0)
	os.Remove(swapped)
	if err := os.Mkdir(swapped, 0o755); err != nil {
		t.Fatal(err)
	}
	_, err = e.fetch(mSwap.Token)
	wantErr(t, err, api.ErrNotFound)

	grown := e.file("grown.bin", "x")
	mGrown := e.mint(d, grown, 0)
	if err := os.Truncate(grown, MaxFileSize+1); err != nil {
		t.Fatal(err)
	}
	_, err = e.fetch(mGrown.Token)
	wantErr(t, err, api.ErrPayloadTooLarge)
}

func TestConcurrentFetchesOfOneLinkHaveOneWinner(t *testing.T) {
	d := device("d1")
	e := newEnv(t, nil, d)
	m := e.mint(d, e.file("a.txt", "once"), 0)
	var wins atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			dl, err := e.svc.Resolve(m.Token, fmt.Sprintf("198.51.100.%d", i))
			switch {
			case err == nil:
				wins.Add(1)
				dl.Close()
			case !errors.Is(err, api.ErrNotFound):
				t.Errorf("unexpected error %v", err)
			}
		}(i)
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatalf("%d fetches succeeded; want exactly 1", wins.Load())
	}
}

func TestFetchRateLimitPerSourceAddress(t *testing.T) {
	e := newEnv(t, nil, device("d1"))
	for i := 0; i < RateLimitAttempts; i++ {
		_, err := e.svc.Resolve("guess", "203.0.113.5")
		wantErr(t, err, api.ErrNotFound)
	}
	_, err := e.svc.Resolve("guess", "203.0.113.5")
	wantErr(t, err, api.ErrRateLimited)
	if _, err := e.svc.Resolve("guess", "203.0.113.6"); !errors.Is(err, api.ErrNotFound) {
		t.Fatalf("a different address shares the budget: %v", err)
	}
	e.clock.advance(RateLimitWindow)
	if _, err := e.svc.Resolve("guess", "203.0.113.5"); !errors.Is(err, api.ErrNotFound) {
		t.Fatalf("after the window the budget should reset: %v", err)
	}
}

func TestLimiterTableIsBounded(t *testing.T) {
	l := newIPLimiter(1, time.Minute, 2)
	now := testStart
	if !l.allow("a", now) || !l.allow("b", now) {
		t.Fatal("the first two addresses must be admitted")
	}
	if l.allow("c", now) {
		t.Fatal("a third address was admitted into a full table")
	}
	if l.allow("a", now) {
		t.Fatal("address a exceeded its budget of one")
	}
	if !l.allow("c", now.Add(time.Minute)) {
		t.Fatal("once windows expire a new address must be admitted")
	}
	if !l.allow("", now.Add(2*time.Minute)) || l.allow("", now.Add(2*time.Minute)) {
		t.Fatal("an empty address must share one bucket")
	}
}

func TestListScopesByCallerAndHidesExpiredAndUsed(t *testing.T) {
	a, b := device("a"), device("b")
	admin := store.Device{ID: "adm", Label: "admin", TokenHash: "th-adm", Origin: store.OriginLogin, Username: "alice"}
	e := newEnv(t, []string{"alice"}, a, b, admin)
	p := e.file("f.txt", "x")
	ma := e.mint(a, p, 3600)
	e.clock.advance(time.Second)
	mb := e.mint(b, p, 3600)
	ownerless := e.plant("tok-ownerless", p, "", time.Hour)
	short := e.mint(a, p, 1)
	used := e.mint(a, p, 3600)
	if _, err := e.fetch(used.Token); err != nil {
		t.Fatal(err)
	}
	e.clock.advance(2 * time.Second) // `short` has expired

	hashes := func(ss []Summary) []string {
		var out []string
		for _, s := range ss {
			out = append(out, s.TokenHash)
		}
		return out
	}
	mine, err := e.svc.List(caller(a))
	if err != nil || !reflect.DeepEqual(hashes(mine), []string{ma.TokenHash}) {
		t.Fatalf("device a sees %v, %v; want only its live link", hashes(mine), err)
	}
	if mine[0].Path != ma.Path || mine[0].ExpiresAt != ma.ExpiresAt {
		t.Fatalf("summary = %+v; want path and expiry of the mint", mine[0])
	}
	all, err := e.svc.List(caller(admin))
	got := map[string]bool{}
	for _, h := range hashes(all) {
		got[h] = true
	}
	if err != nil || len(all) != 3 || !got[ma.TokenHash] || !got[mb.TokenHash] || !got[ownerless] || got[short.TokenHash] || got[used.TokenHash] {
		t.Fatalf("admin sees %v, %v; want a, b and the owner-less link only", hashes(all), err)
	}
	_, err = e.svc.List(Caller{})
	wantErr(t, err, authz.ErrForbidden)
}

func TestRevokeIsScopedToOwnerOrAdmin(t *testing.T) {
	a, b := device("a"), device("b")
	admin := store.Device{ID: "adm", Label: "admin", TokenHash: "th-adm", Origin: store.OriginLogin, Username: "alice"}
	e := newEnv(t, []string{"alice"}, a, b, admin)
	p := e.file("f.txt", "x")
	m := e.mint(a, p, 0)
	ownerless := e.plant("tok-ownerless", p, "", time.Hour)

	_, err := e.svc.Revoke(caller(b), m.TokenHash)
	wantErr(t, err, api.ErrNotFound)
	_, err = e.svc.Revoke(caller(a), ownerless)
	wantErr(t, err, api.ErrNotFound)
	_, err = e.svc.Revoke(caller(a), "unknown-hash")
	wantErr(t, err, api.ErrNotFound)
	if err.Error() != ErrNoSuchShare.Error() {
		t.Fatalf("an unknown hash says %q; a foreign one must say the same", err)
	}
	_, err = e.svc.Revoke(caller(a), "")
	wantErr(t, err, api.ErrBadRequest)
	if l, _ := e.db.ShareLinkByHash(m.TokenHash); l == nil {
		t.Fatal("a refused revoke deleted the link")
	}

	evs, err := e.svc.Revoke(caller(a), m.TokenHash)
	want := []Event{{Action: AuditShareRevoked, Actor: "label-a", Target: m.Path}}
	if err != nil || !reflect.DeepEqual(evs, want) {
		t.Fatalf("owner revoke = %+v, %v; want %+v", evs, err, want)
	}
	_, err = e.svc.Revoke(caller(a), m.TokenHash)
	wantErr(t, err, api.ErrNotFound)
	if _, err := e.svc.Revoke(caller(admin), ownerless); err != nil {
		t.Fatalf("admin revoke of an owner-less link: %v", err)
	}
	if events, _ := e.db.ShareEvents(); len(events) != 2 {
		t.Fatalf("%d history rows; want both mint rows kept", len(events))
	}
}

func TestSweepRemovesExpiredLinks(t *testing.T) {
	d := device("d1")
	e := newEnv(t, nil, d)
	p := e.file("a.txt", "x")
	e.mint(d, p, 10)
	keep := e.mint(d, p, 3600)
	e.clock.advance(11 * time.Second)
	n, err := e.svc.Sweep()
	if err != nil || n != 1 {
		t.Fatalf("Sweep = %d, %v; want 1", n, err)
	}
	if l, _ := e.db.ShareLinkByHash(keep.TokenHash); l == nil {
		t.Fatal("Sweep removed a live link")
	}
}

func TestSameHashIsExactComparison(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{{"abc", "abc", true}, {"abc", "abd", false}, {"abc", "abcd", false}, {"", "", true}, {"", "a", false}} {
		if got := sameHash(c.a, c.b); got != c.want {
			t.Errorf("sameHash(%q, %q) = %v; want %v", c.a, c.b, got, c.want)
		}
	}
}
