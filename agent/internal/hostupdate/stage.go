package hostupdate

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/bundle"
	"github.com/ZaidQamhieh/rfe/agent/internal/launcher"
)

// stager is one Check or Stage run: the config with defaults filled in and the shared HTTP client.
type stager struct {
	cfg      Config
	client   *http.Client
	keys     Keys
	platform Platform
	store    *launcher.Store
}

// newStager validates cfg and fills in the production defaults of every seam.
func newStager(cfg Config, needRoot bool) (*stager, error) {
	if cfg.ManifestURL == "" {
		cfg.ManifestURL = ManifestURL
	}
	if cfg.ManifestURL == "" {
		return nil, &Error{Op: "config", Err: fmt.Errorf("%w: no manifest url", ErrConfig)}
	}
	if cfg.SigURL == "" {
		cfg.SigURL = cfg.ManifestURL + ".sig"
	}
	if _, err := parseSemver(cfg.Installed); err != nil {
		return nil, &Error{Op: "config", Err: fmt.Errorf("installed version: %w", err)}
	}
	if needRoot && cfg.Root == "" {
		return nil, &Error{Op: "config", Err: fmt.Errorf("%w: no install root", ErrConfig)}
	}
	if cfg.FreeSpace == nil {
		cfg.FreeSpace = FreeSpace
	}
	if cfg.Rename == nil {
		cfg.Rename = os.Rename
	}
	if cfg.RenameRetryable == nil {
		cfg.RenameRetryable = defaultRenameRetryable
	}
	if cfg.Sleep == nil {
		cfg.Sleep = time.Sleep
	}
	if cfg.RenameAttempts <= 0 {
		cfg.RenameAttempts = DefaultRenameAttempts
	}
	if cfg.RenameBackoff <= 0 {
		cfg.RenameBackoff = DefaultRenameBackoff
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = defaultStageTimeout
	}
	s := &stager{cfg: cfg}
	s.keys = CompiledKeys()
	if cfg.Keys != nil {
		s.keys = *cfg.Keys
	}
	s.platform = NativePlatform()
	if cfg.Platform != nil {
		s.platform = *cfg.Platform
	}
	if cfg.Root != "" {
		s.store = launcher.NewStore(cfg.Root)
	}
	s.client = s.newClient()
	return s, nil
}

// Check downloads the manifest and its signature, verifies the signature, and decides whether this release may
// be installed here: not older than the installed version (equal only with Force), the installed version at least
// min_installed, a bundle for this platform, and not marked bad in Root's state.json when Root is set. It writes
// nothing.
func Check(ctx context.Context, cfg Config) (*Plan, error) {
	s, err := newStager(cfg, false)
	if err != nil {
		return nil, err
	}
	return s.plan(ctx)
}

// plan is Check on an already-built stager.
func (s *stager) plan(ctx context.Context) (*Plan, error) {
	data, err := s.fetchSmall(ctx, s.cfg.ManifestURL, maxManifestBytes)
	if err != nil {
		return nil, opErr("manifest", err)
	}
	sig, err := s.fetchSmall(ctx, s.cfg.SigURL, maxSigBytes)
	if err != nil {
		return nil, opErr("signature", err)
	}
	if err := s.keys.Verify(data, sig); err != nil {
		return nil, opErr("signature", err)
	}
	rel, err := ParseRelease(data)
	if err != nil {
		return nil, opErr("manifest", err)
	}
	cmp, err := CompareVersions(rel.Version, s.cfg.Installed)
	if err != nil {
		return nil, opErr("version", err)
	}
	switch {
	case cmp < 0:
		return nil, opErr("version", fmt.Errorf("%w: release %s, installed %s", ErrDowngrade, rel.Version, s.cfg.Installed))
	case cmp == 0 && !s.cfg.Force:
		return nil, opErr("version", fmt.Errorf("%w: %s", ErrAlreadyCurrent, rel.Version))
	}
	if rel.MinInstalled != "" {
		if c, err := CompareVersions(s.cfg.Installed, rel.MinInstalled); err != nil {
			return nil, opErr("version", err)
		} else if c < 0 {
			return nil, opErr("version", fmt.Errorf("%w: installed %s, needs %s", ErrInstalledTooOld, s.cfg.Installed, rel.MinInstalled))
		}
	}
	b, err := PickBundle(*rel, s.platform.GOOS, s.platform.GOARCH, s.platform.EmulatedHostArch)
	if err != nil {
		return nil, opErr("platform", err)
	}
	if s.store != nil {
		st, err := s.store.Read()
		if err != nil {
			return nil, opErr("version", err)
		}
		if st.IsBad(rel.Version) {
			return nil, opErr("version", fmt.Errorf("%w: %s", ErrVersionBad, rel.Version))
		}
	}
	return &Plan{Release: *rel, Bundle: b, Newer: cmp > 0}, nil
}

// Stage runs Check, then downloads the bundle into <Root>/staging/<version>.part, verifies its size and
// SHA-256 against the signed manifest, extracts it into <Root>/staging/<version>.dir, verifies the tree against its
// own manifest.json, runs Config.Trial if set, and renames the directory to <Root>/versions/<version>. It never
// writes state.json except to mark a version bad after a failed trial, never touches current or pending, and never
// deletes anything under versions/. Whatever it created in staging is removed again on every failure.
func Stage(ctx context.Context, cfg Config) (res *Result, err error) {
	s, err := newStager(cfg, true)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, s.cfg.Timeout)
	defer cancel()
	plan, err := s.plan(ctx)
	if err != nil {
		return nil, err
	}
	v := plan.Release.Version
	target, err := s.store.VersionDir(v)
	if err != nil {
		return nil, opErr("version", err)
	}
	staging := s.store.StagingPath()
	if err := os.MkdirAll(staging, 0o700); err != nil {
		return nil, opErr("install", err)
	}
	lock, err := launcher.AcquireLock(filepath.Join(staging, "stage.lock"))
	if err != nil {
		return nil, opErr("install", err)
	}
	defer lock.Release()

	if st, lerr := os.Lstat(target); lerr == nil {
		if !st.IsDir() {
			return nil, opErr("install", fmt.Errorf("%w: %s is not a directory", ErrVersionExists, target))
		}
		if _, verr := bundle.Verify(target); verr != nil {
			return nil, opErr("install", fmt.Errorf("%w: %v", ErrVersionExists, verr))
		}
		s.cfg.logf("version %s is already staged", v)
		return &Result{Version: v, Dir: target, Plan: *plan, AlreadyStaged: true}, nil
	} else if !errors.Is(lerr, fs.ErrNotExist) {
		return nil, opErr("install", lerr)
	}

	part := filepath.Join(staging, v+".part")
	dir := filepath.Join(staging, v+".dir")
	defer func() {
		if err != nil {
			os.Remove(part)
			os.RemoveAll(dir)
		}
	}()
	if err := os.RemoveAll(dir); err != nil {
		return nil, opErr("install", err)
	}

	if err := checkDisk(s.cfg.FreeSpace, staging, plan.Bundle.Size, s.cfg.DatabaseBytes); err != nil {
		return nil, opErr("disk", err)
	}
	s.cfg.logf("downloading %s (%s)", v, plan.Bundle.describe())
	if err := s.download(ctx, plan.Bundle, part); err != nil {
		return nil, opErr("download", err)
	}
	files, err := extractArchive(part, dir, s.cfg.Limits)
	if err != nil {
		return nil, opErr("extract", err)
	}
	if err := s.verifyTree(dir, plan, files); err != nil {
		return nil, opErr("verify", err)
	}
	trialRan := false
	if s.cfg.Trial != nil {
		if terr := s.cfg.Trial(dir); terr != nil {
			err = fmt.Errorf("%w: %v", ErrTrialFailed, terr)
			if _, merr := s.store.Apply(launcher.EvMarkBad, v); merr != nil {
				err = errors.Join(err, fmt.Errorf("could not mark %s bad: %w", v, merr))
			}
			return nil, opErr("trial", err)
		}
		trialRan = true
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return nil, opErr("install", err)
	}
	if err := s.moveIntoPlace(dir, target); err != nil {
		return nil, opErr("install", err)
	}
	os.Remove(part)
	s.cfg.logf("staged %s in %s", v, target)
	return &Result{Version: v, Dir: target, Plan: *plan, TrialRan: trialRan}, nil
}

// verifyTree checks an extracted directory against its own manifest.json (every listed file re-hashed), that the
// manifest names the release version and the chosen platform, that it lists the agent executable, and that the
// archive held no file the manifest does not list. It then sets the exec bits on unix.
func (s *stager) verifyTree(dir string, plan *Plan, extracted []string) error {
	m, err := bundle.Verify(dir)
	if err != nil {
		return err
	}
	if m.Version != plan.Release.Version || m.GOOS != plan.Bundle.GOOS || m.GOARCH != plan.Bundle.GOARCH {
		return fmt.Errorf("%w: bundle is %s %s/%s, release says %s %s/%s", ErrBundleMismatch,
			m.Version, m.GOOS, m.GOARCH, plan.Release.Version, plan.Bundle.GOOS, plan.Bundle.GOARCH)
	}
	listed := map[string]uint32{}
	for _, f := range m.Files {
		listed[f.Path] = f.Mode
	}
	agent := "rfe"
	if m.GOOS == "windows" {
		agent = "rfe.exe"
	}
	if _, ok := listed[agent]; !ok {
		return fmt.Errorf("%w: manifest does not list %s", ErrBundleMismatch, agent)
	}
	for _, p := range extracted {
		if _, ok := listed[p]; !ok && p != bundle.ManifestName {
			return fmt.Errorf("%w: archive holds %s, which the manifest does not list", ErrBundleMismatch, p)
		}
	}
	if runtime.GOOS != "windows" {
		return setExecBits(dir, listed)
	}
	return nil
}

// moveIntoPlace renames from to to, retrying with exponential backoff while the error is a transient file lock
// (Windows holds files open during scans). It gives up after Config.RenameAttempts tries with ErrRenameFailed, and
// fails at once on any other error.
func (s *stager) moveIntoPlace(from, to string) error {
	delay := s.cfg.RenameBackoff
	var last error
	for attempt := 1; attempt <= s.cfg.RenameAttempts; attempt++ {
		if last = s.cfg.Rename(from, to); last == nil {
			return nil
		}
		if !s.cfg.RenameRetryable(last) {
			return fmt.Errorf("%w: %v", ErrRenameFailed, last)
		}
		if attempt == s.cfg.RenameAttempts {
			break
		}
		s.cfg.logf("rename of %s locked (attempt %d): %v", path.Base(filepath.ToSlash(to)), attempt, last)
		s.cfg.Sleep(delay)
		if delay *= 2; delay > maxRenameBackoff {
			delay = maxRenameBackoff
		}
	}
	return fmt.Errorf("%w: gave up after %d attempts: %v", ErrRenameFailed, s.cfg.RenameAttempts, last)
}
