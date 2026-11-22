package daemon

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/next/api"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/authz"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/legacyimport"
	"github.com/ZaidQamhieh/rfe/agent/internal/security"
	"github.com/ZaidQamhieh/rfe/agent/internal/sidecar"
)

const testPassword = "a long test password for the owner 4242"

// stubRestarter never touches the host: the sweep calls POST /agent/restart.
type stubRestarter struct{}

// Supported says the host can restart.
func (stubRestarter) Supported() bool { return true }

// Restart does nothing.
func (stubRestarter) Restart() error { return nil }

// newTestDaemon builds a daemon over temp folders with sidecars off.
func newTestDaemon(t *testing.T) (d *Daemon, root string) {
	t.Helper()
	t.Setenv(envSidecars, sidecarOffWords[0])
	root = t.TempDir()
	d, err := New(Options{
		Addr: "127.0.0.1:0", Name: "test-agent", DataDir: t.TempDir(), Roots: []string{root}, RootsExplicit: true,
		Version: "test", TrashDir: filepath.Join(t.TempDir(), "trash"), Restarter: stubRestarter{}, Logf: t.Logf,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.Close)
	return d, root
}

// do sends one request to the handler from a distinct private address.
func do(d *Daemon, n int, method, path, token string, body any) *httptest.ResponseRecorder {
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(method, path, bytes.NewReader(raw))
	req.RemoteAddr = fmt.Sprintf("127.0.%d.%d:4000", n/250, n%250+1)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	d.Handler().ServeHTTP(rec, req)
	return rec
}

// registerOwner creates the owner account over HTTP and returns its admin token.
func registerOwner(t *testing.T, d *Daemon) string {
	t.Helper()
	code, err := d.pairing.MintOwner(0)
	if err != nil {
		t.Fatal(err)
	}
	rec := do(d, 1, http.MethodPost, "/v1/auth/challenge", "", nil)
	var ch api.ChallengeResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &ch); err != nil || ch.Nonce == "" {
		t.Fatalf("challenge = %d %s", rec.Code, rec.Body)
	}
	seed := make([]byte, ed25519.SeedSize)
	priv := ed25519.NewKeyFromSeed(seed)
	sig := ed25519.Sign(priv, []byte(security.DeviceProofMessageV2(ch.CertFingerprint, ch.Nonce)))
	rec = do(d, 2, http.MethodPost, "/v1/register", "", map[string]any{
		"pairingCode": code.Code, "deviceLabel": "test phone", "deviceId": "dev-test-1",
		"devicePublicKey": base64.StdEncoding.EncodeToString(priv.Public().(ed25519.PublicKey)),
		"nonce":           ch.Nonce, "signature": base64.StdEncoding.EncodeToString(sig),
		"username": "owner", "password": testPassword,
	})
	var pr api.PairResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &pr); err != nil || pr.DeviceToken == "" {
		t.Fatalf("register = %d %s", rec.Code, rec.Body)
	}
	return pr.DeviceToken
}

// accountServerPending lists the optional account-server routes (rfe-7np). The
// current agent serves them; the rewrite answers 501 until it ports them. Remove
// an entry here when its handler arrives (api.NotYetImplemented says the same).
var accountServerPending = map[string]bool{
	"GET /accounts/key":               true,
	"POST /accounts/assertion":        true,
	"GET /settings/account-server":    true,
	"PUT /settings/account-server":    true,
	"DELETE /settings/account-server": true,
	// ffmpeg HLS video (rfe-iyr): the current agent serves it; the rewrite answers 501.
	"GET /stream": true,
}

// TestNoRouteAnswers501 calls every route of the table with the real wiring and
// fails for any that answers 501 NOT_IMPLEMENTED.
func TestNoRouteAnswers501(t *testing.T) {
	d, _ := newTestDaemon(t)
	token := registerOwner(t, d)
	param := regexp.MustCompile(`\{[^}]+\}`)
	var bad []string
	for i, rt := range api.Routes {
		if rt.Method == "GET" && rt.Pattern == "/events" {
			continue // a stream that ends only with the client or Shutdown: TestShutdownEndsEventStreams
		}
		path := api.PathPrefix + param.ReplaceAllString(rt.Pattern, "x")
		rec := do(d, 10+i, rt.Method, path, token, map[string]any{})
		if rec.Code == http.StatusNotImplemented && !accountServerPending[rt.Method+" "+rt.Pattern] {
			bad = append(bad, rt.Method+" "+path)
		}
	}
	if len(api.Routes) == 0 {
		t.Fatal("no routes")
	}
	if len(bad) > 0 {
		t.Fatalf("routes that answer 501: %v", bad)
	}
}

// TestConfigHasEveryService fails when a service field of the API config is
// left empty, so a newly added service cannot be forgotten.
func TestConfigHasEveryService(t *testing.T) {
	d, _ := newTestDaemon(t)
	optional := map[string]bool{"Clock": true, "MaxBodyBytes": true, "ChunkBufferBytes": true, "ChunkWait": true}
	for _, v := range []any{d.apiCfg, d.apiCfg.Misc, d.apiCfg.Host} {
		rv := reflect.ValueOf(v)
		for i := 0; i < rv.NumField(); i++ {
			name := rv.Type().Field(i).Name
			if rv.Field(i).IsZero() && !optional[name] {
				t.Errorf("%T.%s is not wired", v, name)
			}
		}
	}
	svc, err := d.apiCfg.Files.For(authz.Principal{}, authz.Policy{})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := svc.(api.FsExtraService); !ok {
		t.Error("file service lacks FsExtraService")
	}
	if _, ok := svc.(api.ContentService); !ok {
		t.Error("file service lacks ContentService")
	}
}

// TestSearchRootsAreTheJail checks a jailed device searches from its jail only.
func TestSearchRootsAreTheJail(t *testing.T) {
	d, root := newTestDaemon(t)
	jail := filepath.Join(root, "jail")
	if err := os.MkdirAll(jail, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{filepath.Join(jail, "inside-needle.txt"), filepath.Join(root, "outside-needle.txt")} {
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if got := d.search.rootsFor(jail); len(got) != 1 || got[0] != jail {
		t.Fatalf("rootsFor(jail) = %v", got)
	}
	q := url.Values{"q": {"needle"}}
	page, err := d.search.Search(context.Background(), jail, q)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(page.Entries)
	if !strings.Contains(string(raw), "inside-needle") || strings.Contains(string(raw), "outside-needle") {
		t.Fatalf("jailed search = %s", raw)
	}
	page, err = d.search.Search(context.Background(), "", q)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(page.Entries)
	if !strings.Contains(string(raw), "outside-needle") {
		t.Fatalf("unjailed search = %s", raw)
	}
}

// pinnedClient trusts only the certificate with the given fingerprint.
func pinnedClient(fingerprint string) *http.Client {
	return &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{TLSClientConfig: &tls.Config{
		InsecureSkipVerify: true, // verification is replaced by the pin below
		VerifyPeerCertificate: func(raw [][]byte, _ [][]*x509.Certificate) error {
			sum := sha256.Sum256(raw[0])
			if hex.EncodeToString(sum[:]) != fingerprint {
				return errors.New("certificate does not match the pinned fingerprint")
			}
			return nil
		},
	}}}
}

// TestSmokeServe starts the daemon on 127.0.0.1:0 and calls /v1/health,
// /v1/status and the web companion over pinned TLS.
func TestSmokeServe(t *testing.T) {
	d, _ := newTestDaemon(t)
	token := registerOwner(t, d)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- d.Serve(ln) }()
	base := "https://" + ln.Addr().String()
	c := pinnedClient(d.Fingerprint())

	get := func(path, tok string) (int, string) {
		req, _ := http.NewRequest(http.MethodGet, base+path, nil)
		if tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
		}
		resp, err := c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	}
	if code, body := get("/v1/health", ""); code != 200 || !strings.Contains(body, "ok") {
		t.Fatalf("health = %d %s", code, body)
	}
	if code, body := get("/v1/status", token); code != 200 || !strings.Contains(body, "test-agent") {
		t.Fatalf("status = %d %s", code, body)
	}
	if code, _ := get("/v1/status", ""); code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d", code)
	}
	if code, body := get("/", ""); code != 200 || !strings.Contains(strings.ToLower(body), "<html") {
		t.Fatalf("web companion = %d %.80s", code, body)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := d.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatalf("Serve: %v", err)
	}
}

// TestNewRefusesUnimportedOldDatabase checks the daemon will not start an empty
// database beside an old one.
func TestNewRefusesUnimportedOldDatabase(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, legacyimport.OldDBName), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := New(Options{Addr: "127.0.0.1:0", DataDir: dir, Logf: t.Logf})
	if !errors.Is(err, ErrNeedsImport) {
		t.Fatalf("err = %v", err)
	}
}

// TestSidecarEnabled checks RFE_SIDECARS parsing.
func TestSidecarEnabled(t *testing.T) {
	t.Setenv(sidecar.DirEnv, t.TempDir())
	cases := map[string]bool{"off": false, "indexd": true, "all": true, "thumbd": false, "indexd,thumbd": true, "off,all": false}
	for env, want := range cases {
		t.Setenv(envSidecars, env)
		if got := sidecarEnabled(sidecarIndexd); got != want {
			t.Errorf("RFE_SIDECARS=%q indexd = %v, want %v", env, got, want)
		}
	}
	t.Setenv(envSidecars, "")
	if sidecarEnabled(sidecarIndexd) {
		t.Error("unset RFE_SIDECARS enabled an uninstalled sidecar")
	}
}

// TestShutdownEndsEventStreams checks an open GET /v1/events does not hold
// Shutdown up: http.Server.Shutdown waits for in-flight requests, and a stream
// never finishes by itself.
func TestShutdownEndsEventStreams(t *testing.T) {
	d, _ := newTestDaemon(t)
	token := registerOwner(t, d)
	srv := httptest.NewServer(d.handler)
	defer srv.Close()
	req, err := http.NewRequest(http.MethodGet, srv.URL+"/v1/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	ended := make(chan struct{})
	go func() { _, _ = io.Copy(io.Discard, resp.Body); close(ended) }()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := d.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ended:
	case <-time.After(3 * time.Second):
		t.Fatal("the stream outlived Shutdown")
	}
}
