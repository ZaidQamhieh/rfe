package api_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	evhub "github.com/ZaidQamhieh/rfe/agent/internal/events"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/api"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/apps"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/pairing"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/settings"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/store"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/system"
	"github.com/ZaidQamhieh/rfe/agent/internal/security"
)

// Fixed values of the test rig. The fingerprint is all zeros: a valid-looking
// SHA-256 that belongs to no real certificate.
var (
	testFingerprint = hex.EncodeToString(make([]byte, sha256.Size))
	startedAt       = time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
	appID           = "app_" + strings.Repeat("ab", sha256.Size)
	viewOnlyAppID   = "app_" + strings.Repeat("cd", sha256.Size)
	goneAppID       = "app_" + strings.Repeat("ef", sha256.Size)
)

const (
	testPassword = "correct horse battery"
	testVersion  = "9.9.9"
	testAPIVer   = 3
	testName     = "test-pc"
	testAddress  = "192.0.2.1:8765"
	// hostPath is a folder name that must never appear in a client answer.
	hostPath = "/srv/secret-host-folder"
)

// fakeSettings is a settable settings source.
type fakeSettings struct {
	mu sync.Mutex
	s  settings.Settings
}

// Get returns the current settings.
func (f *fakeSettings) Get() settings.Settings {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.s
}

// setReadOnly turns global read-only mode on or off.
func (f *fakeSettings) setReadOnly(v bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.s.ReadOnly = v
}

type fakeDisk struct{}

// Usage reports a fixed disk.
func (fakeDisk) Usage() (uint64, uint64, error) { return 40, 100, nil }

type fakeNet struct{}

// Addresses reports fixed addresses.
func (fakeNet) Addresses() system.Addresses {
	return system.Addresses{LAN: "192.0.2.1", MAC: "aa:bb:cc:dd:ee:ff"}
}

type fakeHost struct{}

// CPU is unsupported on the fake host.
func (fakeHost) CPU() (system.CPUTimes, error) { return system.CPUTimes{}, system.ErrUnsupported }

// Memory is unsupported on the fake host.
func (fakeHost) Memory() (system.MemInfo, error) { return system.MemInfo{}, system.ErrUnsupported }

type fakeLister struct{}

// List returns one launchable and one view-only app.
func (fakeLister) List(_ context.Context) (string, []apps.Listing, error) {
	return apps.PlatformLinux, []apps.Listing{
		{App: apps.App{ID: appID, Name: "Editor", Launchable: true}, Ref: "editor.desktop"},
		{App: apps.App{ID: viewOnlyAppID, Name: "Viewer", Launchable: false}, Ref: "viewer.desktop"},
	}, nil
}

type fakeLauncher struct {
	mu  sync.Mutex
	got []string
}

// Launch records the launched app's name.
func (f *fakeLauncher) Launch(_ context.Context, app apps.Listing) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.got = append(f.got, app.Name)
	return nil
}

// launched returns the names launched so far.
func (f *fakeLauncher) launched() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.got...)
}

// panicPairing makes Authenticate panic for one token, to test recovery.
type panicPairing struct {
	*pairing.Service
}

const panicToken = "panic-token"

// Authenticate panics for panicToken and otherwise defers to the service.
func (p panicPairing) Authenticate(token string) (*store.Device, error) {
	if token == panicToken {
		panic("boom " + hostPath)
	}
	return p.Service.Authenticate(token)
}

// rig is the whole HTTP stack over a real store and real services.
type rig struct {
	t        *testing.T
	h        http.Handler
	svc      *pairing.Service
	db       *store.DB
	set      *fakeSettings
	launcher *fakeLauncher
	prompts  []pairing.Prompt
	logs     *lines
	remote   int
}

// lines is a Logger that keeps what it is given.
type lines struct {
	mu sync.Mutex
	b  bytes.Buffer
}

// Printf appends one line.
func (l *lines) Printf(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.b.WriteString(strings.TrimSpace(fmt.Sprintf(format, args...)) + "\n")
}

// String returns everything logged.
func (l *lines) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// rigOptions tune a rig.
type rigOptions struct {
	maxBody      int64
	validateJail func(string) (string, error)
	events       *evhub.Hub
}

// newRig builds the stack: real store, pairing, system and apps services behind
// the real handler.
func newRig(t *testing.T, opt rigOptions) *rig {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "agent-v3.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	r := &rig{t: t, db: db, set: &fakeSettings{s: settings.Settings{AgentName: testName}}, launcher: &fakeLauncher{}, logs: &lines{}}
	svc, err := pairing.New(pairing.Options{
		Store: db, Fingerprint: testFingerprint, AgentName: testName, Address: testAddress,
		OnPairRequest: func(p pairing.Prompt) { r.prompts = append(r.prompts, p) },
		// A fast stand-in for the real password hash, which has its own tests.
		Passwords: pairing.Passwords{
			Hash:   func(p string) (string, error) { return "hash:" + p, nil },
			Verify: func(h, p string) bool { return h == "hash:"+p },
			Burn:   func(string) {},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	r.svc = svc
	sys, err := system.New(system.Config{
		Version: testVersion, APIVersion: testAPIVer, Started: startedAt, Fingerprint: testFingerprint,
		Port: "8765", Settings: r.set, Disk: fakeDisk{}, Network: fakeNet{}, Counts: db, Host: fakeHost{},
	})
	if err != nil {
		t.Fatal(err)
	}
	h, err := api.New(api.Config{
		Pairing: panicPairing{svc}, Devices: db, System: sys, Policy: r.set, Logger: r.logs,
		Apps:         apps.New(apps.Config{Lister: fakeLister{}, Launcher: r.launcher}),
		ValidateJail: opt.validateJail, MaxBodyBytes: opt.maxBody, Events: opt.events,
	})
	if err != nil {
		t.Fatal(err)
	}
	r.h = h
	return r
}

// result is a recorded answer.
type result struct {
	*httptest.ResponseRecorder
}

// json decodes the body into v, failing the test when it is not JSON.
func (res result) json(t *testing.T, v any) {
	t.Helper()
	if err := json.Unmarshal(res.Body.Bytes(), v); err != nil {
		t.Fatalf("body %q is not the expected JSON: %v", res.Body.String(), err)
	}
}

// errBody decodes an error answer strictly: exactly code and message.
func (res result) errBody(t *testing.T) api.ErrorBody {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(res.Body.Bytes()))
	dec.DisallowUnknownFields()
	var e api.ErrorBody
	if err := dec.Decode(&e); err != nil {
		t.Fatalf("error body %q has the wrong shape: %v", res.Body.String(), err)
	}
	return e
}

// wantError fails unless the answer is the given status with the given code.
func (res result) wantError(t *testing.T, status int, code string) {
	t.Helper()
	if res.Code != status {
		t.Fatalf("status = %d (%s), want %d %s", res.Code, res.Body.String(), status, code)
	}
	if e := res.errBody(t); e.Code != code || e.Message == "" {
		t.Fatalf("error = %+v, want code %s with a message", e, code)
	}
}

// opt is one request tweak.
type opt func(*http.Request)

// withHeader sets a request header.
func withHeader(k, v string) opt { return func(r *http.Request) { r.Header.Set(k, v) } }

// do sends one request. body is encoded as JSON unless it is a string or nil.
// Each call comes from its own source address so rate limits are not what a
// test exercises, unless it sets RemoteAddr itself.
func (r *rig) do(method, path, token string, body any, opts ...opt) result {
	r.t.Helper()
	var rd *bytes.Reader
	switch b := body.(type) {
	case nil:
		rd = bytes.NewReader(nil)
	case string:
		rd = bytes.NewReader([]byte(b))
	default:
		raw, err := json.Marshal(b)
		if err != nil {
			r.t.Fatal(err)
		}
		rd = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, "/v1"+path, rd)
	r.remote++
	req.RemoteAddr = "10.9." + strconv.Itoa(r.remote/250) + "." + strconv.Itoa(r.remote%250+1) + ":4000"
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for _, o := range opts {
		o(req)
	}
	rec := httptest.NewRecorder()
	r.h.ServeHTTP(rec, req)
	return result{rec}
}

// phone is a device with its own Ed25519 identity.
type phone struct {
	id   string
	pub  string
	priv ed25519.PrivateKey
}

// newPhone makes a phone whose key derives from seedByte.
func newPhone(id string, seedByte byte) phone {
	seed := make([]byte, ed25519.SeedSize)
	seed[0] = seedByte
	priv := ed25519.NewKeyFromSeed(seed)
	return phone{id: id, pub: base64.StdEncoding.EncodeToString(priv.Public().(ed25519.PublicKey)), priv: priv}
}

// proof gets a challenge over HTTP and signs it as the phone would.
func (r *rig) proof(p phone) (nonce, sig string) {
	r.t.Helper()
	res := r.do(http.MethodPost, "/auth/challenge", "", nil)
	if res.Code != http.StatusOK {
		r.t.Fatalf("challenge = %d %s", res.Code, res.Body.String())
	}
	var ch api.ChallengeResponse
	res.json(r.t, &ch)
	if ch.Proof != "v2" || ch.CertFingerprint != testFingerprint {
		r.t.Fatalf("challenge = %+v", ch)
	}
	raw := ed25519.Sign(p.priv, []byte(security.DeviceProofMessageV2(ch.CertFingerprint, ch.Nonce)))
	return ch.Nonce, base64.StdEncoding.EncodeToString(raw)
}

// pairBody is the JSON of POST /pair for the phone with a fresh proof.
func (r *rig) pairBody(p phone, code string) map[string]any {
	nonce, sig := r.proof(p)
	return map[string]any{
		"pairingCode": code, "deviceLabel": "my phone", "deviceId": p.id,
		"devicePublicKey": p.pub, "nonce": nonce, "signature": sig,
	}
}

// pairDevice pairs the phone with an ordinary code over HTTP and returns its
// token and device id. The device starts browse-only.
func (r *rig) pairDevice(p phone) (token, deviceID string) {
	r.t.Helper()
	m, err := r.svc.Mint(0)
	if err != nil {
		r.t.Fatal(err)
	}
	res := r.do(http.MethodPost, "/pair", "", r.pairBody(p, m.Code))
	if res.Code != http.StatusOK {
		r.t.Fatalf("pair = %d %s", res.Code, res.Body.String())
	}
	var pr api.PairResponse
	res.json(r.t, &pr)
	if pr.DeviceToken == "" || pr.DeviceID == "" {
		r.t.Fatalf("pair response = %+v", pr)
	}
	return pr.DeviceToken, pr.DeviceID
}

// registerAdmin creates the account with an owner code over HTTP and returns
// the admin device's token and id.
func (r *rig) registerAdmin(p phone) (token, deviceID string) {
	r.t.Helper()
	m, err := r.svc.MintOwner(0)
	if err != nil {
		r.t.Fatal(err)
	}
	body := r.pairBody(p, m.Code)
	body["username"], body["password"] = "owner", testPassword
	res := r.do(http.MethodPost, "/register", "", body)
	if res.Code != http.StatusOK {
		r.t.Fatalf("register = %d %s", res.Code, res.Body.String())
	}
	var pr api.PairResponse
	res.json(r.t, &pr)
	return pr.DeviceToken, pr.DeviceID
}
