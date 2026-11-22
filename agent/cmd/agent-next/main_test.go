package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/next/api"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/daemon"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/legacyimport"
	"github.com/ZaidQamhieh/rfe/agent/internal/security"
	"github.com/ZaidQamhieh/rfe/agent/internal/store/legacyfixtures"
	_ "modernc.org/sqlite" // the driver used to edit the old database fixture
)

// Names and words the tests rely on.
const (
	envSidecars     = "RFE_SIDECARS" // read by the daemon; "off" keeps the tests from starting helper programs
	sidecarsOff     = "off"
	loopbackAny     = "127.0.0.1:0"
	pairedDeviceID  = "dev-new-pair"
	pairedLabel     = "pair test phone"
	checkHeaderText = "Check only"
	noOldDBText     = "no old agent database"
	needsImportText = "not imported yet"
	requestTimeout  = 10 * time.Second
	shutdownWait    = 5 * time.Second
)

// pairCodeLine matches the line `agent-next pair` prints: kind and code.
var pairCodeLine = regexp.MustCompile(`pairing code \((\w+)\): (\S+)`)

// newTableNames maps the fixture's old table names to the names the import report uses.
var newTableNames = map[string]string{"share_tokens": "share_links", "share_log": "share_events"}

// setRootsSQL points the old agent's allowed folders setting at an existing folder.
const setRootsSQL = `UPDATE config SET value = ? WHERE key = 'roots'`

// chunkTable is the table whose rows an import may drop.
const chunkTable = "upload_chunks"

// droppedChunksLine matches the report line counting dropped chunk rows.
var droppedChunksLine = regexp.MustCompile(`Chunk rows dropped[^:]*: (\d+)`)

// cli runs the command line in-process and returns its exit status, stdout and stderr.
func cli(args ...string) (code int, stdout, stderr string) {
	var out, errOut bytes.Buffer
	code = run(args, &out, &errOut)
	return code, out.String(), errOut.String()
}

// dirState maps every path under dir (relative) to "dir" or the file's SHA-256.
func dirState(t *testing.T, dir string) map[string]string {
	t.Helper()
	state := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		if d.IsDir() {
			state[rel] = "dir"
			return nil
		}
		state[rel] = hashFile(t, p)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return state
}

// hashFile is the hex SHA-256 of a file's bytes.
func hashFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// oldAgentDir builds a data directory as the old agent leaves it: its database
// (latest schema, with devices and tokens) and its TLS certificate. It returns
// the fixture and the certificate fingerprint.
func oldAgentDir(t *testing.T) (legacyfixtures.Fixture, string) {
	t.Helper()
	dir := t.TempDir()
	// The old agent wrote its identity before its database, and LoadOrCreateCert refuses to
	// create one next to an existing agent.db, so the certificate comes first.
	cert, err := security.LoadOrCreateCert(dir)
	if err != nil {
		t.Fatal(err)
	}
	f := legacyfixtures.Build(t, dir, legacyfixtures.LatestVersion)
	// The fixture's allowed folder is a path that exists only on the machine it
	// imitates, and the daemon refuses to start when an allowed folder is missing.
	oldDB, err := sql.Open("sqlite", f.DBPath)
	if err != nil {
		t.Fatal(err)
	}
	defer oldDB.Close()
	if _, err := oldDB.Exec(setRootsSQL, t.TempDir()); err != nil {
		t.Fatalf("%s: %v", setRootsSQL, err)
	}
	return f, security.Fingerprint(cert)
}

// served is a daemon listening on an ephemeral loopback port, and a client that
// trusts only the certificate fingerprint it was given (trust on first use).
type served struct {
	t      *testing.T
	d      *daemon.Daemon
	addr   string
	client *http.Client
}

// serve starts the daemon over dataDir on a free loopback port. The client it
// returns fails the TLS handshake unless the server's certificate has the pinned fingerprint.
func serve(t *testing.T, dataDir, pinned string) *served {
	t.Helper()
	t.Setenv(envSidecars, sidecarsOff)
	d, err := daemon.New(daemon.Options{
		Addr: loopbackAny, Name: "main-test", DataDir: dataDir, Version: "test",
		TrashDir: filepath.Join(t.TempDir(), "trash"), Logf: t.Logf,
	})
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", loopbackAny)
	if err != nil {
		d.Close()
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- d.Serve(ln) }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), shutdownWait)
		defer cancel()
		if err := d.Shutdown(ctx); err != nil {
			t.Errorf("shutdown: %v", err)
		}
		if err := <-done; err != nil {
			t.Errorf("serve: %v", err)
		}
		d.Close()
	})
	// The certificate is self-signed, so the chain check is replaced by the pin.
	tlsCfg := &tls.Config{
		InsecureSkipVerify: true, //nolint:gosec // pinned by VerifyPeerCertificate below
		MinVersion:         tls.VersionTLS12,
		VerifyPeerCertificate: func(raw [][]byte, _ [][]*x509.Certificate) error {
			if len(raw) == 0 {
				return errors.New("server sent no certificate")
			}
			sum := sha256.Sum256(raw[0])
			if got := hex.EncodeToString(sum[:]); got != pinned {
				return fmt.Errorf("certificate fingerprint %s does not match the pinned %s", got, pinned)
			}
			return nil
		},
	}
	client := &http.Client{Timeout: requestTimeout, Transport: &http.Transport{TLSClientConfig: tlsCfg}}
	t.Cleanup(client.CloseIdleConnections)
	return &served{t: t, d: d, addr: ln.Addr().String(), client: client}
}

// call sends one JSON request with an optional bearer token and returns the status and body.
func (s *served) call(method, path, token string, body any) (int, []byte) {
	s.t.Helper()
	var rd io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			s.t.Fatal(err)
		}
		rd = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, "https://"+s.addr+api.PathPrefix+path, rd)
	if err != nil {
		s.t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		s.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		s.t.Fatal(err)
	}
	return resp.StatusCode, raw
}

// devices lists GET /devices for a token and returns the status and the ids it shows.
func (s *served) devices(token string) (int, []string) {
	s.t.Helper()
	status, raw := s.call(http.MethodGet, "/devices", token, nil)
	var list []struct {
		ID string `json:"id"`
	}
	if status == http.StatusOK {
		if err := json.Unmarshal(raw, &list); err != nil {
			s.t.Fatalf("GET /devices body %s: %v", raw, err)
		}
	}
	ids := make([]string, 0, len(list))
	for _, d := range list {
		ids = append(ids, d.ID)
	}
	return status, ids
}

// TestImportCheckLeavesTheDataDirByteIdentical runs `import --check` on an old
// agent's data directory: it reports the rows, exits 0 and changes nothing.
func TestImportCheckLeavesTheDataDirByteIdentical(t *testing.T) {
	f, _ := oldAgentDir(t)
	before := dirState(t, f.DataDir)

	code, out, errOut := cli(cmdImport, "--"+flagCheck, "-"+flagData, f.DataDir)
	if code != exitOK {
		t.Fatalf("exit %d, stderr %q, stdout %q", code, errOut, out)
	}
	if !strings.Contains(out, checkHeaderText) {
		t.Errorf("output does not say it was a check:\n%s", out)
	}
	// The report names the new tables; chunk rows whose temp file is too short are
	// dropped and counted on their own line, so printed + dropped = fixture rows.
	droppedChunks := 0
	if m := droppedChunksLine.FindStringSubmatch(out); m != nil {
		n, err := strconv.Atoi(m[1])
		if err != nil {
			t.Fatal(err)
		}
		droppedChunks = n
	}
	for oldName, n := range f.Expected.Counts {
		name := oldName
		if renamed, ok := newTableNames[oldName]; ok {
			name = renamed
		}
		if name == chunkTable {
			n -= droppedChunks
		}
		if want := fmt.Sprintf("  %s: %d\n", name, n); n > 0 && !strings.Contains(out, want) {
			t.Errorf("output lacks %q (fixture table %s):\n%s", strings.TrimSpace(want), oldName, out)
		}
	}
	after := dirState(t, f.DataDir)
	if !reflect.DeepEqual(before, after) {
		t.Errorf("--check changed the data directory:\n before %v\n after  %v", before, after)
	}
	if _, ok := after[legacyimport.NewDBName]; ok {
		t.Errorf("--check created %s", legacyimport.NewDBName)
	}
	for _, sidecar := range []string{"-wal", "-shm"} {
		if _, ok := after[legacyimport.NewDBName+sidecar]; ok {
			t.Errorf("--check left %s%s behind", legacyimport.NewDBName, sidecar)
		}
	}
}

// TestImportCheckWithoutAnOldDatabaseFailsAndWritesNothing covers an empty
// directory and a directory that does not exist yet.
func TestImportCheckWithoutAnOldDatabaseFailsAndWritesNothing(t *testing.T) {
	empty := t.TempDir()
	missing := filepath.Join(t.TempDir(), "never-created")
	for name, dir := range map[string]string{"empty": empty, "missing": missing} {
		t.Run(name, func(t *testing.T) {
			code, out, errOut := cli(cmdImport, "--"+flagCheck, "-"+flagData, dir)
			if code != exitFail {
				t.Errorf("exit %d, want %d (stdout %q)", code, exitFail, out)
			}
			if !strings.Contains(errOut, noOldDBText) {
				t.Errorf("stderr %q does not explain that there is no old database", errOut)
			}
		})
	}
	if entries, err := os.ReadDir(empty); err != nil || len(entries) != 0 {
		t.Errorf("the empty data dir now holds %v (err %v)", entries, err)
	}
	if _, err := os.Stat(missing); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the missing data dir was created (stat err %v)", err)
	}
}

// TestServeAndPairRefuseAnUnimportedOldDatabase checks that neither command
// starts a fresh empty database beside an old one that was not imported.
func TestServeAndPairRefuseAnUnimportedOldDatabase(t *testing.T) {
	f, _ := oldAgentDir(t)
	before := dirState(t, f.DataDir)
	for _, args := range [][]string{
		{cmdServe, "-" + flagAddr, loopbackAny, "-" + flagData, f.DataDir},
		{cmdPair, "-" + flagData, f.DataDir},
	} {
		code, _, errOut := cli(args...)
		if code != exitFail || !strings.Contains(errOut, needsImportText) {
			t.Errorf("%v: exit %d, stderr %q; want exit %d and %q", args, code, errOut, exitFail, needsImportText)
		}
	}
	if after := dirState(t, f.DataDir); !reflect.DeepEqual(before, after) {
		t.Errorf("a refused command changed the data directory:\n before %v\n after  %v", before, after)
	}
}

// importOld imports an old agent's data directory through the command line.
func importOld(t *testing.T, dir string) {
	t.Helper()
	code, out, errOut := cli(cmdImport, "-"+flagData, dir)
	if code != exitOK {
		t.Fatalf("import: exit %d, stderr %q, stdout %q", code, errOut, out)
	}
	if _, err := os.Stat(filepath.Join(dir, legacyimport.NewDBName)); err != nil {
		t.Fatalf("import wrote no new database: %v", err)
	}
}

// TestImportThenServeKeepsDevicesTokensAndCertificate imports, serves, and
// checks that the old bearer tokens still work (and revoked or orphaned ones
// do not) and that the served certificate is the old one, so pinning holds.
func TestImportThenServeKeepsDevicesTokensAndCertificate(t *testing.T) {
	f, oldFingerprint := oldAgentDir(t)
	oldDBHash := hashFile(t, f.DBPath)
	certHashes := map[string]string{}
	for _, name := range []string{"agent-cert.pem", "agent-key.pem"} {
		certHashes[name] = hashFile(t, filepath.Join(f.DataDir, name))
	}

	importOld(t, f.DataDir)
	if got := hashFile(t, f.DBPath); got != oldDBHash {
		t.Errorf("import changed the old database (%s -> %s)", oldDBHash, got)
	}

	s := serve(t, f.DataDir, oldFingerprint) // the client fails unless the certificate is the old one
	if got := s.d.Fingerprint(); got != oldFingerprint {
		t.Errorf("daemon fingerprint %s, old data dir's certificate %s", got, oldFingerprint)
	}

	var allIDs []string
	for _, d := range f.Expected.Devices {
		allIDs = append(allIDs, d.ID)
	}
	for _, d := range f.Expected.Devices {
		token := f.Tokens[d.ID]
		status, ids := s.devices(token)
		if want := d.Authenticates && !d.Revoked; want != (status == http.StatusOK) {
			t.Errorf("device %s (authenticates %v, revoked %v): GET /devices answered %d", d.ID, d.Authenticates, d.Revoked, status)
			continue
		}
		if status != http.StatusOK {
			continue
		}
		if d.ViaLogin { // a login device is an administrator and sees every device
			if !sameSet(ids, allIDs) {
				t.Errorf("the owner device sees %v, want every imported device %v", ids, allIDs)
			}
		} else if !reflect.DeepEqual(ids, []string{d.ID}) {
			t.Errorf("device %s sees %v, want only itself", d.ID, ids)
		}
	}
	if status, _ := s.devices("not-a-token-the-old-agent-issued"); status != http.StatusUnauthorized {
		t.Errorf("an unknown token answered %d, want %d", status, http.StatusUnauthorized)
	}
	if status, _ := s.devices(""); status != http.StatusUnauthorized {
		t.Errorf("no token answered %d, want %d", status, http.StatusUnauthorized)
	}
	for name, want := range certHashes {
		if got := hashFile(t, filepath.Join(f.DataDir, name)); got != want {
			t.Errorf("%s was rewritten by import or serve", name)
		}
	}
}

// sameSet reports whether a and b hold the same strings, ignoring order.
func sameSet(a, b []string) bool {
	seen := map[string]int{}
	for _, s := range a {
		seen[s]++
	}
	for _, s := range b {
		seen[s]--
	}
	for _, n := range seen {
		if n != 0 {
			return false
		}
	}
	return true
}

// TestPairCodeIsAcceptedByTheServer mints a code with the pair subcommand
// after an import and pairs a new device with it over HTTPS.
func TestPairCodeIsAcceptedByTheServer(t *testing.T) {
	f, oldFingerprint := oldAgentDir(t)
	importOld(t, f.DataDir)

	code, out, errOut := cli(cmdPair, "-"+flagData, f.DataDir)
	if code != exitOK {
		t.Fatalf("pair: exit %d, stderr %q", code, errOut)
	}
	m := pairCodeLine.FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("pair printed no code:\n%s", out)
	}
	kind, pairingCode := m[1], m[2]
	if kind != "device" {
		t.Errorf("the imported database has accounts, so the code should be an ordinary device code, got %q", kind)
	}

	s := serve(t, f.DataDir, oldFingerprint)
	status, raw := s.call(http.MethodPost, "/auth/challenge", "", nil)
	var ch api.ChallengeResponse
	if err := json.Unmarshal(raw, &ch); status != http.StatusOK || err != nil || ch.Nonce == "" {
		t.Fatalf("challenge = %d %s (%v)", status, raw, err)
	}
	if ch.CertFingerprint != oldFingerprint {
		t.Errorf("challenge advertises fingerprint %s, want the old one %s", ch.CertFingerprint, oldFingerprint)
	}
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	sig := ed25519.Sign(priv, []byte(security.DeviceProofMessageV2(ch.CertFingerprint, ch.Nonce)))
	body := map[string]any{
		"pairingCode": pairingCode, "deviceLabel": pairedLabel, "deviceId": pairedDeviceID,
		"devicePublicKey": base64.StdEncoding.EncodeToString(pub),
		"nonce":           ch.Nonce, "signature": base64.StdEncoding.EncodeToString(sig),
	}
	status, raw = s.call(http.MethodPost, "/pair", "", body)
	var pr api.PairResponse
	if err := json.Unmarshal(raw, &pr); status != http.StatusOK || err != nil || pr.DeviceToken == "" {
		t.Fatalf("pair = %d %s (%v)", status, raw, err)
	}
	if pr.CertFingerprint != oldFingerprint {
		t.Errorf("pair response fingerprint %s, want %s", pr.CertFingerprint, oldFingerprint)
	}
	if status, ids := s.devices(pr.DeviceToken); status != http.StatusOK || !reflect.DeepEqual(ids, []string{pr.DeviceID}) {
		t.Errorf("the new token: GET /devices = %d %v, want 200 and only the new device", status, ids)
	}
	// A code is one-time: a second challenge and pairing with it must fail.
	status, raw = s.call(http.MethodPost, "/auth/challenge", "", nil)
	if err := json.Unmarshal(raw, &ch); status != http.StatusOK || err != nil {
		t.Fatalf("second challenge = %d %s (%v)", status, raw, err)
	}
	sig = ed25519.Sign(priv, []byte(security.DeviceProofMessageV2(ch.CertFingerprint, ch.Nonce)))
	body["nonce"], body["signature"] = ch.Nonce, base64.StdEncoding.EncodeToString(sig)
	body["deviceId"] = pairedDeviceID + "-again"
	if status, raw = s.call(http.MethodPost, "/pair", "", body); status == http.StatusOK {
		t.Errorf("a used pairing code was accepted again: %s", raw)
	}
}

// TestPairOnAFreshDataDirMintsAnOwnerCode checks the no-account case: the code
// creates the owner account, and the directory gets its own certificate.
func TestPairOnAFreshDataDirMintsAnOwnerCode(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "fresh")
	code, out, errOut := cli(cmdPair, "-"+flagData, dir)
	if code != exitOK {
		t.Fatalf("pair: exit %d, stderr %q", code, errOut)
	}
	if m := pairCodeLine.FindStringSubmatch(out); m == nil || m[1] != "owner" {
		t.Errorf("want an owner code, got:\n%s", out)
	}
}

// TestUnknownCommandAndStrayArgumentsAreUsageErrors checks the exit status 2 paths.
func TestUnknownCommandAndStrayArgumentsAreUsageErrors(t *testing.T) {
	dir := t.TempDir()
	for _, args := range [][]string{
		{"frobnicate"},
		{cmdImport, "-" + flagData, dir, "stray"},
		{cmdPair, "-" + flagData, dir, "stray"},
		{cmdImport, "--no-such-flag"},
	} {
		if code, _, _ := cli(args...); code != exitUsage {
			t.Errorf("%v: exit %d, want %d", args, code, exitUsage)
		}
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("a usage error wrote %v", entries)
	}
}
