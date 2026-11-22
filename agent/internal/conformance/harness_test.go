package conformance

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"
)

const (
	// binEnv names the environment variable that points at the agent binary to test.
	binEnv = "RFE_CONF_BIN"
	// oldBinEnv names the variable that points at the current (oracle) agent
	// binary the differential test compares binEnv against.
	oldBinEnv = "RFE_CONF_OLD_BIN"
	// reportEnv, when set, is the file the differential test writes its full
	// per-case report to (explained and unexplained differences).
	reportEnv = "RFE_CONF_REPORT"
	// ignoreHeadersEnv, when set, lists response headers (comma separated) the
	// differential test drops from both sides, so one known header difference
	// does not hide every other difference while it is being triaged.
	ignoreHeadersEnv = "RFE_CONF_IGNORE_HEADERS"
	// agentName is the display name the test agent is started with.
	agentName = "conformance-agent"
	// ownerUser and ownerPassword are throwaway credentials for the owner account
	// the harness registers on its own empty agent; they protect nothing real.
	ownerUser     = "conf-owner"
	ownerPassword = "conformance-fake-pw"

	buildTimeout   = 5 * time.Minute
	startTimeout   = 20 * time.Second
	adminTimeout   = 30 * time.Second
	requestTimeout = 30 * time.Second
	stopTimeout    = 5 * time.Second
	pairingTTL     = "5m"
	logTailBytes   = 4096
	// fixtureImageSide is the width and height of the generated PNG fixture.
	fixtureImageSide = 64
)

// pairingCodeRe pulls the one-time code out of `rfe pair` output.
var pairingCodeRe = regexp.MustCompile(`(?i)pairing code(?: \(\w+\))?:\s+(\S+)`)

// agentUnderTest is one running agent process plus the directories and HTTP
// client the harness uses to talk to it.
type agentUnderTest struct {
	bin, addr, baseURL        string
	dataDir, root, home, logs string
	outside                   string // sibling of root holding a file the jail must hide
	client                    *http.Client
}

// testDevice is a client identity: an Ed25519 key the agent pins at pairing,
// plus the device id and bearer token the agent returned.
type testDevice struct {
	clientID string
	label    string
	pub      ed25519.PublicKey
	priv     ed25519.PrivateKey
	id       string
	token    string
}

// rawResponse is an HTTP response read fully into memory.
type rawResponse struct {
	status int
	header http.Header
	body   []byte
}

// absBinary makes the path an environment variable named absolute and checks
// that it is a file; env is only used in the failure message.
func absBinary(t *testing.T, env, p string) string {
	t.Helper()
	abs, err := filepath.Abs(p)
	if err != nil {
		t.Fatalf("%s=%q: %v", env, p, err)
	}
	if st, err := os.Stat(abs); err != nil || st.IsDir() {
		t.Fatalf("%s=%q is not a usable file (%v)", env, p, err)
	}
	return abs
}

// agentBinary returns the agent executable to test: the file named by
// RFE_CONF_BIN, or a fresh build of ./cmd/agent into a temp directory.
func agentBinary(t *testing.T) string {
	t.Helper()
	if p := os.Getenv(binEnv); p != "" {
		return absBinary(t, binEnv, p)
	}
	out := filepath.Join(t.TempDir(), "agent")
	if runtime.GOOS == "windows" {
		out += ".exe"
	}
	ctx, cancel := context.WithTimeout(context.Background(), buildTimeout)
	defer cancel()
	// The test runs with the package directory as its working directory.
	if b, err := exec.CommandContext(ctx, "go", "build", "-o", out, "../../cmd/agent").CombinedOutput(); err != nil {
		t.Fatalf("go build ./cmd/agent: %v\n%s", err, b)
	}
	return out
}

// freeLoopbackAddr asks the OS for an unused loopback port and returns it as
// host:port. The port is released before return, so a clash is possible but rare.
func freeLoopbackAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("pick a port: %v", err)
	}
	defer ln.Close()
	return ln.Addr().String()
}

// writeFixtureTree creates the directory tree every request in the table
// operates on and returns nothing; file modes are set explicitly so results do
// not depend on the umask.
func writeFixtureTree(t *testing.T, root, outside string) {
	t.Helper()
	files := map[string]string{
		"docs/hello.txt":      "hello world\n",
		"docs/notes.md":       "# notes\n",
		"docs/sub/nested.txt": "nested\n",
		"data/range.txt":      strings.Repeat("0123456789", 10),
		"delete-me.txt":       "to trash\n",
		"delete-perm.txt":     "to delete for good\n",
		// Search, chmod and jail fixtures. They sit under data/ so the root
		// listing the older cases take is unchanged.
		"data/needle-alpha.txt": "findme-content-token alpha\n",
		"data/beta.log":         "beta log line\n",
		"data/run.sh":           "#!/bin/sh\n",
	}
	for rel, content := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("fixture dir: %v", err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatalf("fixture file: %v", err)
		}
	}
	for _, dir := range []string{"", "docs", "docs/sub", "data"} {
		if err := os.Chmod(filepath.Join(root, filepath.FromSlash(dir)), 0o755); err != nil {
			t.Fatalf("fixture chmod: %v", err)
		}
	}
	writeFixturePNG(t, filepath.Join(root, "data", "pic.png"))
	// A file next to the root that the jail must hide, reachable through a
	// symlink inside the root.
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatalf("fixture outside dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("outside the root\n"), 0o644); err != nil {
		t.Fatalf("fixture outside file: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "data", "escape-link")); err != nil {
		t.Logf("symlink fixture skipped: %v", err)
	}
}

// writeFixturePNG writes a small gradient PNG, the image the thumbnail and
// media-info cases read.
func writeFixturePNG(t *testing.T, path string) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, fixtureImageSide, fixtureImageSide))
	for y := 0; y < fixtureImageSide; y++ {
		for x := 0; x < fixtureImageSide; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 4), G: uint8(y * 4), B: 128, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode fixture png: %v", err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("fixture png: %v", err)
	}
}

// startAgent launches the binary on a free loopback port with a temp data dir
// and the fixture tree as its only root, waits until /v1/health answers, and
// registers a cleanup that stops it (dumping its log if the test failed).
func startAgent(t *testing.T, bin string) *agentUnderTest {
	t.Helper()
	base := t.TempDir()
	a := &agentUnderTest{bin: bin, home: filepath.Join(base, "home")}
	for _, d := range []string{a.home, filepath.Join(base, "data"), filepath.Join(base, "root")} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}
	// Resolve symlinks (a temp dir may sit behind one) so the path the agent
	// reports for the root is the path the harness masks.
	var err error
	if a.dataDir, err = filepath.EvalSymlinks(filepath.Join(base, "data")); err != nil {
		t.Fatal(err)
	}
	if a.root, err = filepath.EvalSymlinks(filepath.Join(base, "root")); err != nil {
		t.Fatal(err)
	}
	a.outside = filepath.Join(filepath.Dir(a.root), "outside")
	writeFixtureTree(t, a.root, a.outside)

	a.addr = freeLoopbackAddr(t)
	a.baseURL = "https://" + a.addr
	a.logs = filepath.Join(base, "agent.log")
	logf, err := os.Create(a.logs)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(bin, "-addr", a.addr, "-name", agentName, "-data", a.dataDir, "-roots", a.root)
	cmd.Env = a.env()
	cmd.Stdout, cmd.Stderr = logf, logf
	if err := cmd.Start(); err != nil {
		t.Fatalf("start agent: %v", err)
	}
	done := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(done)
	}()
	t.Cleanup(func() {
		_ = cmd.Process.Signal(os.Interrupt)
		select {
		case <-done:
		case <-time.After(stopTimeout):
			_ = cmd.Process.Kill()
			<-done
		}
		_ = logf.Close()
		if t.Failed() {
			if b, err := os.ReadFile(a.logs); err == nil {
				if len(b) > logTailBytes {
					b = b[len(b)-logTailBytes:]
				}
				t.Logf("agent log tail:\n%s", b)
			}
		}
	})

	// The agent is a throwaway process this test started on loopback and it
	// presents a self-signed certificate, so verification is skipped.
	a.client = &http.Client{
		Timeout: requestTimeout,
		Transport: &http.Transport{
			TLSClientConfig:    &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // loopback test agent, self-signed
			ForceAttemptHTTP2:  true,
			DisableCompression: true,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	t.Cleanup(a.client.CloseIdleConnections)

	deadline := time.Now().Add(startTimeout)
	for {
		if r, err := a.do("", http.MethodGet, "/v1/health", nil, nil); err == nil && r.status == http.StatusOK {
			return a
		}
		select {
		case <-done:
			t.Fatalf("agent exited before answering /v1/health; see %s", a.logs)
		default:
		}
		if time.Now().After(deadline) {
			t.Fatalf("agent did not answer /v1/health within %s; see %s", startTimeout, a.logs)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// env is the minimal environment for the agent and its admin CLI: an isolated
// HOME and data dir, and sidecars off so results do not depend on what is installed.
func (a *agentUnderTest) env() []string {
	return []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + a.home,
		"RFE_DATA_DIR=" + a.dataDir,
		"RFE_SIDECARS=off",
		// Keep /v1/health the same on hosts with and without ffmpeg.
		"RFE_FFMPEG=/nonexistent/ffmpeg",
	}
}

// admin runs one admin CLI subcommand against the agent's data dir and returns its stdout.
func (a *agentUnderTest) admin(t *testing.T, args ...string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), adminTimeout)
	defer cancel()
	full := append(append([]string{}, args...), "-data", a.dataDir)
	cmd := exec.CommandContext(ctx, a.bin, full...)
	cmd.Env = a.env()
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("%s %v: %v\n%s", filepath.Base(a.bin), args, err, stderr.String())
	}
	return stdout.String()
}

// do sends one request and reads the whole response. A non-empty token is sent
// as a bearer credential; pathAndQuery is appended to the agent's base URL.
func (a *agentUnderTest) do(token, method, pathAndQuery string, header map[string]string, body []byte) (*rawResponse, error) {
	req, err := http.NewRequest(method, a.baseURL+pathAndQuery, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for k, v := range header {
		req.Header.Set(k, v)
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return &rawResponse{status: resp.StatusCode, header: resp.Header, body: b}, nil
}

// postJSON sends v as a JSON body and decodes a JSON object reply (if any) into a map.
func (a *agentUnderTest) postJSON(t *testing.T, token, path string, v any) (int, map[string]any) {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	r, err := a.do(token, http.MethodPost, path, map[string]string{"Content-Type": "application/json"}, b)
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	var out map[string]any
	_ = json.Unmarshal(r.body, &out)
	return r.status, out
}

// newDevice makes a client identity with a fresh Ed25519 key.
func newDevice(t *testing.T, clientID, label string) *testDevice {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return &testDevice{clientID: clientID, label: label, pub: pub, priv: priv}
}

// proof fetches a challenge nonce and signs it the way the agent's v2 device
// proof expects: Ed25519 over "rfe-device-proof-v2\n<cert fingerprint>\n<nonce>".
func (a *agentUnderTest) proof(t *testing.T, d *testDevice) map[string]any {
	t.Helper()
	status, ch := a.postJSON(t, "", "/v1/auth/challenge", map[string]any{})
	nonce, _ := ch["nonce"].(string)
	fp, _ := ch["certFingerprint"].(string)
	if status != http.StatusOK || nonce == "" || fp == "" {
		t.Fatalf("challenge: status %d body %v", status, ch)
	}
	msg := "rfe-device-proof-v2\n" + strings.ToLower(fp) + "\n" + nonce
	return map[string]any{
		"deviceId":        d.clientID,
		"deviceLabel":     d.label,
		"devicePublicKey": base64.StdEncoding.EncodeToString(d.pub),
		"nonce":           nonce,
		"signature":       base64.StdEncoding.EncodeToString(ed25519.Sign(d.priv, []byte(msg))),
	}
}

// pairSupports reports whether the binary's `pair` usage text lists the flag.
func (a *agentUnderTest) pairSupports(t *testing.T, flagName string) bool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), adminTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, a.bin, "pair", "-h", "-data", a.dataDir)
	cmd.Env = a.env()
	// -h exits non-zero after printing the usage, so the error is not a failure here.
	out, _ := cmd.CombinedOutput()
	// A flag line reads "  -name type" (value flags) or just "  -name" (booleans).
	return regexp.MustCompile(`(?m)^\s+` + regexp.QuoteMeta(flagName) + `(\s|$)`).Match(out)
}

// mintCode runs `pair` (with -owner when asked) and returns the printed one-time code.
func (a *agentUnderTest) mintCode(t *testing.T, owner bool) string {
	t.Helper()
	// The current agent's pair command takes -ttl, -addr and -owner; a binary
	// whose usage does not list a flag (the rewrite picks owner or ordinary
	// code by itself) is not given it.
	args := []string{"pair"}
	for _, f := range []struct {
		name, value string
		wanted      bool
	}{{"-ttl", pairingTTL, true}, {"-addr", a.addr, true}, {"-owner", "", owner}} {
		if f.wanted && a.pairSupports(t, f.name) {
			args = append(args, f.name)
			if f.value != "" {
				args = append(args, f.value)
			}
		}
	}
	m := pairingCodeRe.FindStringSubmatch(a.admin(t, args...))
	if m == nil {
		t.Fatal("pair printed no pairing code")
	}
	return m[1]
}

// finishPairing posts the body to a pairing endpoint and stores the returned id and token.
func (a *agentUnderTest) finishPairing(t *testing.T, d *testDevice, path string, body map[string]any) {
	t.Helper()
	status, out := a.postJSON(t, "", path, body)
	d.id, _ = out["deviceId"].(string)
	d.token, _ = out["deviceToken"].(string)
	if status != http.StatusOK || d.id == "" || d.token == "" {
		t.Fatalf("%s for %s: status %d body %v", path, d.label, status, out)
	}
}

// registerOwner creates the owner account (using an owner pairing code) and
// leaves d holding the owner device's token, which carries every file grant.
func (a *agentUnderTest) registerOwner(t *testing.T, d *testDevice) {
	t.Helper()
	body := a.proof(t, d)
	body["pairingCode"] = a.mintCode(t, true)
	body["username"] = ownerUser
	body["password"] = ownerPassword
	a.finishPairing(t, d, "/v1/register", body)
}

// pairGuest pairs d with an ordinary one-time code, which grants browse only.
func (a *agentUnderTest) pairGuest(t *testing.T, d *testDevice) {
	t.Helper()
	body := a.proof(t, d)
	body["pairingCode"] = a.mintCode(t, false)
	a.finishPairing(t, d, "/v1/pair", body)
}

// describe is a one-line summary used in test logs.
func (a *agentUnderTest) describe() string {
	return fmt.Sprintf("agent %s at %s root=%s", filepath.Base(a.bin), a.addr, a.root)
}
