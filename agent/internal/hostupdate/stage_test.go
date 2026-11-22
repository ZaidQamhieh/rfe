package hostupdate

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/bundle"
	"github.com/ZaidQamhieh/rfe/agent/internal/launcher"
)

const (
	testInstalled = "1.0.0"
	testVersion   = "1.1.0"
	hugeFree      = uint64(1) << 50
)

var testPlatform = Platform{GOOS: "linux", GOARCH: "amd64"}

// fixture is a signed release served by an httptest server on 127.0.0.1, with every served byte replaceable.
type fixture struct {
	t                 *testing.T
	primaryPub        ed25519.PublicKey
	primaryPriv       ed25519.PrivateKey
	sparePub          ed25519.PublicKey
	sparePriv         ed25519.PrivateKey
	srv               *httptest.Server
	root              string
	mu                sync.Mutex
	manifest, sig, ar []byte
	bundleHandler     func(w http.ResponseWriter, r *http.Request) // overrides serving ar when set
	bundleRequests    int
	rangeHeaders      []string
}

// newFixture builds a bundle of the given version for testPlatform, packs it (zip when useZip), signs a
// manifest for it and starts the server.
func newFixture(t *testing.T, version string, useZip bool) *fixture {
	t.Helper()
	f := &fixture{t: t, root: filepath.Join(t.TempDir(), "root")}
	f.primaryPub, f.primaryPriv, _ = ed25519.GenerateKey(rand.Reader)
	f.sparePub, f.sparePriv, _ = ed25519.GenerateKey(rand.Reader)
	files := map[string]string{"rfe": "agent binary " + version, "rfe-indexd": "indexer", "data/readme.txt": "hello"}
	f.ar = buildArchive(t, version, testPlatform, files, useZip)
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	f.sign(f.release(version, f.ar), f.primaryPriv)
	return f
}

// release returns a manifest for archive ar served at /bundle.
func (f *fixture) release(version string, ar []byte) Release {
	sum := sha256.Sum256(ar)
	return Release{
		Version:  version,
		Released: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		Bundles: []Bundle{{GOOS: testPlatform.GOOS, GOARCH: testPlatform.GOARCH, URL: f.srv.URL + "/bundle",
			Size: int64(len(ar)), SHA256: hex.EncodeToString(sum[:])}},
	}
}

// sign serialises r and signs the exact bytes with key.
func (f *fixture) sign(r Release, key ed25519.PrivateKey) {
	f.t.Helper()
	data, err := json.Marshal(r)
	if err != nil {
		f.t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.manifest = data
	f.sig = []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(key, data)) + "\n")
}

func (f *fixture) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	manifest, sig, ar, h := f.manifest, f.sig, f.ar, f.bundleHandler
	f.mu.Unlock()
	switch r.URL.Path {
	case "/manifest.json":
		w.Write(manifest)
	case "/manifest.json.sig":
		w.Write(sig)
	case "/bundle":
		f.mu.Lock()
		f.bundleRequests++
		f.rangeHeaders = append(f.rangeHeaders, r.Header.Get("Range"))
		f.mu.Unlock()
		if h != nil {
			h(w, r)
			return
		}
		http.ServeContent(w, r, "bundle", time.Time{}, bytes.NewReader(ar))
	default:
		http.NotFound(w, r)
	}
}

func (f *fixture) requests() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.bundleRequests
}

// cfg is a Config that trusts the primary key and talks to the fixture.
func (f *fixture) cfg() Config {
	plat := testPlatform
	return Config{
		Root:        f.root,
		ManifestURL: f.srv.URL + "/manifest.json",
		Installed:   testInstalled,
		Keys:        &Keys{Primary: f.primaryPub, Spare: f.sparePub},
		HTTPClient:  f.srv.Client(),
		Platform:    &plat,
		FreeSpace:   func(string) (uint64, error) { return hugeFree, nil },
		Sleep:       func(time.Duration) {},
	}
}

// buildArchive makes a bundle directory with bundle.Build and packs it as tar.gz or zip.
func buildArchive(t *testing.T, version string, p Platform, files map[string]string, useZip bool) []byte {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		full := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := bundle.Build(dir, version, p.GOOS, p.GOARCH); err != nil {
		t.Fatal(err)
	}
	entries := map[string][]byte{}
	for name := range files {
		entries[name], _ = os.ReadFile(filepath.Join(dir, filepath.FromSlash(name)))
	}
	entries[bundle.ManifestName], _ = os.ReadFile(filepath.Join(dir, bundle.ManifestName))
	return pack(t, entries, useZip)
}

// pack writes name->content entries as tar.gz or zip, in sorted order.
func pack(t *testing.T, entries map[string][]byte, useZip bool) []byte {
	t.Helper()
	names := make([]string, 0, len(entries))
	for n := range entries {
		names = append(names, n)
	}
	sort.Strings(names)
	var buf bytes.Buffer
	if useZip {
		zw := zip.NewWriter(&buf)
		for _, n := range names {
			w, err := zw.Create(n)
			if err != nil {
				t.Fatal(err)
			}
			w.Write(entries[n])
		}
		zw.Close()
		return buf.Bytes()
	}
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, n := range names {
		tw.WriteHeader(&tar.Header{Name: n, Mode: 0o644, Size: int64(len(entries[n])), Typeflag: tar.TypeReg})
		tw.Write(entries[n])
	}
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

// assertClean fails if staging holds anything for the version, or the version dir exists.
func assertClean(t *testing.T, root, version string) {
	t.Helper()
	entries, _ := os.ReadDir(filepath.Join(root, launcher.StagingDir))
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), version) {
			t.Errorf("staging leftover %s", e.Name())
		}
	}
	if _, err := os.Stat(filepath.Join(root, launcher.VersionsDir, version)); err == nil {
		t.Errorf("versions/%s exists after a failed stage", version)
	}
}

func mustFail(t *testing.T, f *fixture, cfg Config, want error) {
	t.Helper()
	res, err := Stage(context.Background(), cfg)
	if err == nil {
		t.Fatalf("Stage succeeded (%+v), want %v", res, want)
	}
	if !errors.Is(err, want) {
		t.Fatalf("Stage error = %v, want %v", err, want)
	}
	var te *Error
	if !errors.As(err, &te) {
		t.Fatalf("error %T is not *Error", err)
	}
	assertClean(t, f.root, testVersionOf(f))
}

func testVersionOf(f *fixture) string {
	var r Release
	_ = json.Unmarshal(f.manifest, &r)
	return r.Version
}

func TestStageEndToEnd(t *testing.T) {
	for _, useZip := range []bool{false, true} {
		t.Run(fmt.Sprintf("zip=%v", useZip), func(t *testing.T) {
			f := newFixture(t, testVersion, useZip)
			// A running install: versions/1.0.0 is current and must come through untouched.
			store := launcher.NewStore(f.root)
			curDir := filepath.Join(f.root, launcher.VersionsDir, testInstalled)
			if err := os.MkdirAll(curDir, 0o755); err != nil {
				t.Fatal(err)
			}
			marker := filepath.Join(curDir, "marker")
			os.WriteFile(marker, []byte("current"), 0o644)
			if _, err := store.Apply(launcher.EvSetCurrent, testInstalled); err != nil {
				t.Fatal(err)
			}
			before, _ := os.ReadFile(filepath.Join(f.root, launcher.StateFile))

			trialDir := ""
			cfg := f.cfg()
			cfg.Trial = func(dir string) error { trialDir = dir; _, err := bundle.Verify(dir); return err }
			res, err := Stage(context.Background(), cfg)
			if err != nil {
				t.Fatalf("Stage: %v", err)
			}
			want := filepath.Join(f.root, launcher.VersionsDir, testVersion)
			if res.Dir != want || res.Version != testVersion || !res.TrialRan || res.AlreadyStaged {
				t.Fatalf("result = %+v", res)
			}
			if trialDir == "" || trialDir == want {
				t.Errorf("trial ran in %q, want the staging dir", trialDir)
			}
			if _, err := bundle.Verify(want); err != nil {
				t.Errorf("staged bundle does not verify: %v", err)
			}
			if runtime.GOOS != "windows" {
				for _, n := range []string{"rfe", "rfe-indexd"} {
					st, err := os.Stat(filepath.Join(want, n))
					if err != nil || st.Mode().Perm()&0o111 == 0 {
						t.Errorf("%s is not executable: %v %v", n, st, err)
					}
				}
			}
			after, _ := os.ReadFile(filepath.Join(f.root, launcher.StateFile))
			if !bytes.Equal(before, after) {
				t.Errorf("state.json changed by Stage:\n%s\n--\n%s", before, after)
			}
			if got, _ := os.ReadFile(marker); string(got) != "current" {
				t.Errorf("current version dir was modified")
			}
			assertNoStagingFiles(t, f.root)

			// A second run finds the version staged and downloads nothing.
			n := f.requests()
			again, err := Stage(context.Background(), cfg)
			if err != nil || !again.AlreadyStaged || f.requests() != n {
				t.Errorf("second Stage = %+v, %v (bundle requests %d -> %d)", again, err, n, f.requests())
			}
		})
	}
}

// assertNoStagingFiles fails if staging holds anything but the lock file.
func assertNoStagingFiles(t *testing.T, root string) {
	t.Helper()
	entries, _ := os.ReadDir(filepath.Join(root, launcher.StagingDir))
	for _, e := range entries {
		if e.Name() != "stage.lock" {
			t.Errorf("staging leftover %s", e.Name())
		}
	}
}

func TestSignatures(t *testing.T) {
	t.Run("spare key accepted", func(t *testing.T) {
		f := newFixture(t, testVersion, false)
		f.sign(f.release(testVersion, f.ar), f.sparePriv)
		if _, err := Stage(context.Background(), f.cfg()); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("signed by neither key", func(t *testing.T) {
		f := newFixture(t, testVersion, false)
		_, other, _ := ed25519.GenerateKey(rand.Reader)
		f.sign(f.release(testVersion, f.ar), other)
		mustFail(t, f, f.cfg(), ErrBadSignature)
		if f.requests() != 0 {
			t.Error("bundle was requested despite a bad signature")
		}
	})
	t.Run("tampered manifest", func(t *testing.T) {
		f := newFixture(t, testVersion, false)
		f.manifest = bytes.Replace(f.manifest, []byte(testVersion), []byte("9.9.9"), 1)
		mustFail(t, f, f.cfg(), ErrBadSignature)
	})
	t.Run("garbage signature", func(t *testing.T) {
		f := newFixture(t, testVersion, false)
		f.sig = []byte("not base64 !!")
		mustFail(t, f, f.cfg(), ErrBadSignature)
	})
	t.Run("no keys fails closed", func(t *testing.T) {
		f := newFixture(t, testVersion, false)
		cfg := f.cfg()
		cfg.Keys = &Keys{}
		mustFail(t, f, cfg, ErrNoTrustedKeys)
		cfg.Keys = nil // the compiled-in keys: empty in this build
		if CompiledKeys().Primary != nil || CompiledKeys().Spare != nil {
			t.Fatal("test binary has compiled-in keys")
		}
		mustFail(t, f, cfg, ErrNoTrustedKeys)
	})
	t.Run("malformed injected key is ignored", func(t *testing.T) {
		old := PrimaryPublicKey
		t.Cleanup(func() { PrimaryPublicKey = old })
		PrimaryPublicKey = "AAAA"
		if CompiledKeys().Primary != nil {
			t.Error("short key accepted")
		}
	})
}

func TestDownloadIntegrity(t *testing.T) {
	t.Run("tampered bundle", func(t *testing.T) {
		f := newFixture(t, testVersion, false)
		bad := append([]byte(nil), f.ar...)
		bad[len(bad)/2] ^= 0xff // same size, different bytes
		f.ar = bad
		mustFail(t, f, f.cfg(), ErrHashMismatch)
	})
	t.Run("truncated with a lying content-length", func(t *testing.T) {
		f := newFixture(t, testVersion, false)
		f.bundleHandler = func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Length", fmt.Sprint(len(f.ar)))
			w.Write(f.ar[:len(f.ar)/2])
			w.(http.Flusher).Flush() // headers and half the body are on the wire before the connection dies
			panic(http.ErrAbortHandler)
		}
		mustFail(t, f, f.cfg(), ErrTruncated)
	})
	t.Run("truncated clean end", func(t *testing.T) {
		f := newFixture(t, testVersion, false)
		f.bundleHandler = func(w http.ResponseWriter, r *http.Request) { w.Write(f.ar[:len(f.ar)-10]) }
		mustFail(t, f, f.cfg(), ErrTruncated)
	})
	t.Run("server sends too much (streamed)", func(t *testing.T) {
		f := newFixture(t, testVersion, false)
		f.bundleHandler = func(w http.ResponseWriter, r *http.Request) {
			w.Write(f.ar)
			w.(http.Flusher).Flush()
			w.Write(bytes.Repeat([]byte("x"), 4096))
		}
		mustFail(t, f, f.cfg(), ErrTooLarge)
	})
	t.Run("server announces too much", func(t *testing.T) {
		f := newFixture(t, testVersion, false)
		f.bundleHandler = func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Length", fmt.Sprint(len(f.ar)+100))
			w.Write(f.ar)
		}
		mustFail(t, f, f.cfg(), ErrTooLarge)
	})
	t.Run("resumes a leftover part with a range request", func(t *testing.T) {
		f := newFixture(t, testVersion, false)
		staging := filepath.Join(f.root, launcher.StagingDir)
		os.MkdirAll(staging, 0o700)
		half := len(f.ar) / 2
		os.WriteFile(filepath.Join(staging, testVersion+".part"), f.ar[:half], 0o600)
		if _, err := Stage(context.Background(), f.cfg()); err != nil {
			t.Fatal(err)
		}
		if want := fmt.Sprintf("bytes=%d-", half); len(f.rangeHeaders) != 1 || f.rangeHeaders[0] != want {
			t.Errorf("range headers = %q, want %q", f.rangeHeaders, want)
		}
	})
	t.Run("damaged leftover part restarts from scratch", func(t *testing.T) {
		f := newFixture(t, testVersion, false)
		staging := filepath.Join(f.root, launcher.StagingDir)
		os.MkdirAll(staging, 0o700)
		junk := bytes.Repeat([]byte("z"), len(f.ar)/2)
		os.WriteFile(filepath.Join(staging, testVersion+".part"), junk, 0o600)
		if _, err := Stage(context.Background(), f.cfg()); err != nil {
			t.Fatal(err)
		}
		if f.requests() != 2 {
			t.Errorf("bundle requests = %d, want 2 (resume, then restart)", f.requests())
		}
	})
}

func TestVersionRules(t *testing.T) {
	t.Run("downgrade rejected even when forced", func(t *testing.T) {
		f := newFixture(t, "0.9.0", false)
		cfg := f.cfg()
		mustFail(t, f, cfg, ErrDowngrade)
		cfg.Force = true
		mustFail(t, f, cfg, ErrDowngrade)
	})
	t.Run("equal rejected unless forced", func(t *testing.T) {
		f := newFixture(t, testInstalled, false)
		cfg := f.cfg()
		mustFail(t, f, cfg, ErrAlreadyCurrent)
		cfg.Force = true
		if _, err := Stage(context.Background(), cfg); err != nil {
			t.Fatalf("forced equal: %v", err)
		}
	})
	t.Run("rc is lower than its release", func(t *testing.T) {
		f := newFixture(t, "1.0.0-rc.1", false)
		mustFail(t, f, f.cfg(), ErrDowngrade)
	})
	t.Run("min_installed", func(t *testing.T) {
		f := newFixture(t, testVersion, false)
		r := f.release(testVersion, f.ar)
		r.MinInstalled = "1.0.5"
		f.sign(r, f.primaryPriv)
		mustFail(t, f, f.cfg(), ErrInstalledTooOld)
	})
	t.Run("installed version must be semver", func(t *testing.T) {
		f := newFixture(t, testVersion, false)
		cfg := f.cfg()
		cfg.Installed = "dev"
		if _, err := Stage(context.Background(), cfg); !errors.Is(err, ErrBadVersion) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("version marked bad is refused", func(t *testing.T) {
		f := newFixture(t, testVersion, false)
		store := launcher.NewStore(f.root)
		if _, err := store.Apply(launcher.EvMarkBad, testVersion); err != nil {
			t.Fatal(err)
		}
		mustFail(t, f, f.cfg(), ErrVersionBad)
		if f.requests() != 0 {
			t.Error("bad version was downloaded")
		}
	})
}

func TestWrongArch(t *testing.T) {
	f := newFixture(t, testVersion, false)
	cfg := f.cfg()
	cfg.Platform = &Platform{GOOS: "linux", GOARCH: "riscv64"}
	mustFail(t, f, cfg, ErrWrongArch)
	if f.requests() != 0 {
		t.Error("bundle requested for a platform the release lacks")
	}
}

func TestFullDisk(t *testing.T) {
	f := newFixture(t, testVersion, false)
	size := int64(len(f.ar))
	const db = 1000
	need := uint64(2*size + 3*db)
	cfg := f.cfg()
	cfg.DatabaseBytes = db
	cfg.FreeSpace = func(string) (uint64, error) { return need - 1, nil }
	mustFail(t, f, cfg, ErrInsufficientSpace)
	if f.requests() != 0 {
		t.Error("downloaded despite a full disk")
	}
	cfg.FreeSpace = func(string) (uint64, error) { return need, nil }
	if _, err := Stage(context.Background(), cfg); err != nil {
		t.Fatalf("exactly enough space: %v", err)
	}
	cfg.FreeSpace = func(string) (uint64, error) { return 0, errors.New("statfs failed") }
	cfg.Force = true
	os.RemoveAll(filepath.Join(f.root, launcher.VersionsDir))
	if _, err := Stage(context.Background(), cfg); err == nil {
		t.Error("an unreadable free-space figure must not allow the download")
	}
}

func TestRequiredSpaceOverflow(t *testing.T) {
	if _, ok := requiredSpace(math.MaxInt64, math.MaxInt64); ok {
		t.Error("3x the largest database size overflows uint64 and must be reported")
	}
	if need, ok := requiredSpace(10, 5); !ok || need != 35 {
		t.Errorf("requiredSpace(10,5) = %d,%v want 35", need, ok)
	}
	if _, ok := requiredSpace(-1, 0); ok {
		t.Error("negative size accepted")
	}
}

func TestFreeSpaceReal(t *testing.T) {
	n, err := FreeSpace(t.TempDir())
	if err != nil || n == 0 {
		t.Fatalf("FreeSpace = %d, %v", n, err)
	}
}

func TestRenameRetry(t *testing.T) {
	lock := errors.New("sharing violation")
	retryable := func(err error) bool { return errors.Is(err, lock) }
	t.Run("succeeds after N lock errors", func(t *testing.T) {
		f := newFixture(t, testVersion, false)
		cfg := f.cfg()
		cfg.RenameRetryable = retryable
		calls := 0
		var sleeps []time.Duration
		cfg.Rename = func(a, b string) error {
			calls++
			if calls <= 3 {
				return &os.LinkError{Op: "rename", Old: a, New: b, Err: lock}
			}
			return os.Rename(a, b)
		}
		cfg.Sleep = func(d time.Duration) { sleeps = append(sleeps, d) }
		cfg.RenameBackoff = 10 * time.Millisecond
		if _, err := Stage(context.Background(), cfg); err != nil {
			t.Fatal(err)
		}
		if calls != 4 {
			t.Errorf("rename calls = %d, want 4", calls)
		}
		want := []time.Duration{10 * time.Millisecond, 20 * time.Millisecond, 40 * time.Millisecond}
		if fmt.Sprint(sleeps) != fmt.Sprint(want) {
			t.Errorf("backoff = %v, want %v", sleeps, want)
		}
	})
	t.Run("gives up cleanly after the limit", func(t *testing.T) {
		f := newFixture(t, testVersion, false)
		cfg := f.cfg()
		cfg.RenameRetryable = retryable
		cfg.RenameAttempts = 3
		calls := 0
		cfg.Rename = func(a, b string) error { calls++; return lock }
		mustFail(t, f, cfg, ErrRenameFailed)
		if calls != 3 {
			t.Errorf("rename calls = %d, want 3", calls)
		}
	})
	t.Run("other errors are not retried", func(t *testing.T) {
		f := newFixture(t, testVersion, false)
		cfg := f.cfg()
		cfg.RenameRetryable = retryable
		calls := 0
		cfg.Rename = func(a, b string) error { calls++; return errors.New("disk on fire") }
		mustFail(t, f, cfg, ErrRenameFailed)
		if calls != 1 {
			t.Errorf("rename calls = %d, want 1", calls)
		}
	})
}

func TestTrialFailureMarksBad(t *testing.T) {
	f := newFixture(t, testVersion, false)
	cfg := f.cfg()
	var dir string
	cfg.Trial = func(d string) error { dir = d; return errors.New("selfcheck exploded") }
	mustFail(t, f, cfg, ErrTrialFailed)
	st, err := launcher.NewStore(f.root).Read()
	if err != nil || !st.IsBad(testVersion) {
		t.Fatalf("state = %+v, %v; want %s marked bad", st, err, testVersion)
	}
	if _, err := os.Stat(dir); err == nil {
		t.Error("trial directory was not discarded")
	}
	// A later attempt is refused without downloading.
	n := f.requests()
	if _, err := Stage(context.Background(), f.cfg()); !errors.Is(err, ErrVersionBad) || f.requests() != n {
		t.Errorf("retry err = %v", err)
	}
}

func TestBundleContentChecks(t *testing.T) {
	cases := map[string]struct {
		build func(t *testing.T) []byte
		want  error
	}{
		"inner manifest names another version": {func(t *testing.T) []byte {
			return buildArchive(t, "1.1.1", testPlatform, map[string]string{"rfe": "x"}, false)
		}, ErrBundleMismatch},
		"inner manifest for another platform": {func(t *testing.T) []byte {
			return buildArchive(t, testVersion, Platform{GOOS: "linux", GOARCH: "arm64"}, map[string]string{"rfe": "x"}, false)
		}, ErrBundleMismatch},
		"no agent executable": {func(t *testing.T) []byte {
			return buildArchive(t, testVersion, testPlatform, map[string]string{"other": "x"}, false)
		}, ErrBundleMismatch},
		"file the manifest does not list": {func(t *testing.T) []byte {
			m := unpackEntries(t, buildArchive(t, testVersion, testPlatform, map[string]string{"rfe": "x"}, false))
			m["extra.bin"] = []byte("surprise")
			return pack(t, m, false)
		}, ErrBundleMismatch},
		"file differs from the inner manifest": {func(t *testing.T) []byte {
			m := unpackEntries(t, buildArchive(t, testVersion, testPlatform, map[string]string{"rfe": "x"}, false))
			m["rfe"] = []byte("y")
			return pack(t, m, false)
		}, bundle.ErrMismatch},
		"no manifest.json": {func(t *testing.T) []byte {
			m := unpackEntries(t, buildArchive(t, testVersion, testPlatform, map[string]string{"rfe": "x"}, false))
			delete(m, bundle.ManifestName)
			return pack(t, m, false)
		}, bundle.ErrNoManifest},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, testVersion, false)
			ar := tc.build(t)
			f.ar = ar
			f.sign(f.release(testVersion, ar), f.primaryPriv)
			mustFail(t, f, f.cfg(), tc.want)
		})
	}
}

// unpackEntries reads a tar.gz made by pack back into a map.
func unpackEntries(t *testing.T, ar []byte) map[string][]byte {
	t.Helper()
	gz, err := gzip.NewReader(bytes.NewReader(ar))
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)
	out := map[string][]byte{}
	for {
		h, err := tr.Next()
		if err != nil {
			return out
		}
		var b bytes.Buffer
		b.ReadFrom(tr)
		out[h.Name] = b.Bytes()
	}
}

func TestURLRules(t *testing.T) {
	f := newFixture(t, testVersion, false)
	cfg := f.cfg()
	cfg.HTTPClient = nil // production client: plain http, even to loopback, is refused
	mustFail(t, f, cfg, ErrInsecureURL)
	cfg = f.cfg()
	cfg.ManifestURL = "http://example.invalid/manifest.json"
	mustFail(t, f, cfg, ErrInsecureURL)
	cfg.ManifestURL = "https://user:pw@example.invalid/manifest.json"
	mustFail(t, f, cfg, ErrInsecureURL)
	// A bundle URL on plain http to a non-loopback host in an otherwise valid signed manifest is refused too.
	r := f.release(testVersion, f.ar)
	r.Bundles[0].URL = "http://example.invalid/bundle"
	f.sign(r, f.primaryPriv)
	mustFail(t, f, f.cfg(), ErrInsecureURL)
}

func TestCheckWritesNothing(t *testing.T) {
	f := newFixture(t, testVersion, false)
	plan, err := Check(context.Background(), f.cfg())
	if err != nil || !plan.Newer || plan.Release.Version != testVersion {
		t.Fatalf("Check = %+v, %v", plan, err)
	}
	if _, err := os.Stat(f.root); err == nil {
		t.Error("Check created the install root")
	}
	if f.requests() != 0 {
		t.Error("Check downloaded the bundle")
	}
}

func TestParseReleaseRejects(t *testing.T) {
	good := `{"version":"1.2.3","released":"2026-01-02T03:04:05Z","bundles":[{"goos":"linux","goarch":"amd64","url":"https://x/y","size":10,"sha256":"` + strings.Repeat("ab", 32) + `"}]}`
	if _, err := ParseRelease([]byte(good)); err != nil {
		t.Fatalf("good manifest: %v", err)
	}
	bad := map[string]string{
		"not json":           `{`,
		"path-like version":  strings.Replace(good, "1.2.3", "../1.2.3", 1),
		"non semver":         strings.Replace(good, "1.2.3", "1.2", 1),
		"bad sha":            strings.Replace(good, strings.Repeat("ab", 32), "zz", 1),
		"zero size":          strings.Replace(good, `"size":10`, `"size":0`, 1),
		"no released":        strings.Replace(good, `"released":"2026-01-02T03:04:05Z",`, "", 1),
		"no bundles":         `{"version":"1.2.3","released":"2026-01-02T03:04:05Z","bundles":[]}`,
		"duplicate platform": strings.Replace(good, `}]}`, `},`+good[strings.Index(good, `{"goos"`):], 1),
		"bad min_installed":  strings.Replace(good, `"bundles"`, `"min_installed":"x","bundles"`, 1),
	}
	for name, doc := range bad {
		if _, err := ParseRelease([]byte(doc)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
