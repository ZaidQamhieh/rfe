package sidecar

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/ZaidQamhieh/rfe/agent/internal/bundle"
)

// The files a release package puts next to the executables, and the environment variable that points at them.
const (
	// ManifestName is written next to the sidecars by the release packaging script:
	//
	//	rfe-sidecars 1
	//	version 1.4.0
	//	sha256 <hex> rfe-indexd
	//	sha256 <hex> rfe-thumbd
	//
	// A sidecar listed there is only started when its bytes hash to the listed value and it announces the listed
	// version, so a half-updated install (new agent, old sidecar) or a corrupted file is refused, not run.
	ManifestName = "rfe-sidecars.txt"
	// DirEnv names a directory searched for sidecars before the one next to the agent executable.
	DirEnv = "RFE_SIDECAR_DIR"

	filePrefix       = "rfe-"
	windowsExeSuffix = ".exe"
	manifestVersion  = "version"
	manifestSum      = "sha256"
)

// FileName is the executable name of a sidecar given its short name: "indexd" is "rfe-indexd".
func FileName(short string) string { return filePrefix + short }

// Located is a sidecar executable found on disk.
type Located struct {
	Path string
	// Verified: a manifest vouches for it (hash and version checked).
	Verified bool
	Version  string
	// SHA256 is the lowercase hex digest the manifest lists and the file matched; empty when not Verified. A
	// Config carries it so the file is hashed again before every start.
	SHA256 string
	// DevDir: found in $RFE_SIDECAR_DIR, the explicit development location, not beside the agent executable.
	DevDir bool
}

// envDirIndex is the position of $RFE_SIDECAR_DIR in the directories Locate searches.
const envDirIndex = 0

// Locate-time and start-time refusals of an unverified or swapped sidecar.
var (
	// ErrUnverifiedNotAllowed: a sidecar has no manifest to vouch for it and the owner did not ask for a
	// development build (RFE_SIDECARS naming it with RFE_SIDECAR_DIR pointing at it).
	ErrUnverifiedNotAllowed = errors.New("no " + ManifestName + " beside it; an unverified sidecar runs only from $" + DirEnv + " when RFE_SIDECARS names it")
	// ErrHashChanged: the file no longer hashes to the digest it was verified with.
	ErrHashChanged = errors.New("sidecar file changed since it was verified")
)

type manifest struct {
	version string
	sums    map[string]string
}

func readManifest(dir string) (*manifest, error) {
	f, err := os.Open(filepath.Join(dir, ManifestName))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	m := &manifest{sums: map[string]string{}}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		switch {
		case len(fields) == 2 && fields[0] == manifestVersion:
			m.version = fields[1]
		case len(fields) == 3 && fields[0] == manifestSum:
			m.sums[fields[2]] = strings.ToLower(fields[1])
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if m.version == "" {
		return nil, fmt.Errorf("%s has no version line", ManifestName)
	}
	return m, nil
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// Locate finds the sidecar short (its file is FileName(short)) in $RFE_SIDECAR_DIR, else next to the agent
// executable, and checks it against the manifest there. A listed sidecar that does not match is refused (ok is
// false and err says why); a directory with no manifest (a development build) is accepted unverified.
// The returned path is always absolute, so it is never looked up on PATH when started.
func Locate(short string) (loc Located, ok bool, err error) {
	name := FileName(short)
	exeName := name
	if runtime.GOOS == "windows" {
		exeName += windowsExeSuffix
	}
	dirs := []string{os.Getenv(DirEnv)}
	if exe, e := os.Executable(); e == nil {
		if resolved, e := filepath.EvalSymlinks(exe); e == nil {
			exe = resolved
		}
		dirs = append(dirs, filepath.Dir(exe))
	}
	for i, d := range dirs {
		if d == "" {
			continue
		}
		fromEnvDir := i == envDirIndex
		if abs, e := filepath.Abs(d); e == nil {
			d = abs
		}
		p := filepath.Join(d, exeName)
		if st, e := os.Stat(p); e != nil || !st.Mode().IsRegular() {
			continue
		}
		m, e := readManifest(d)
		if e != nil {
			if os.IsNotExist(e) {
				if _, be := os.Stat(filepath.Join(d, bundle.ManifestName)); be == nil {
					return locateInBundle(d, p, exeName, fromEnvDir)
				}
				return Located{Path: p, DevDir: fromEnvDir}, true, nil
			}
			return Located{}, false, fmt.Errorf("%s: %w", filepath.Join(d, ManifestName), e)
		}
		want, listed := m.sums[exeName]
		if !listed {
			want, listed = m.sums[name]
		}
		if !listed {
			return Located{}, false, fmt.Errorf("%s is not listed in %s", exeName, ManifestName)
		}
		got, e := fileSHA256(p)
		if e != nil {
			return Located{}, false, e
		}
		if got != want {
			return Located{}, false, fmt.Errorf("%s does not match %s (sha256 %s, expected %s)", exeName, ManifestName, got, want)
		}
		return Located{Path: p, Verified: true, Version: m.version, SHA256: got, DevDir: fromEnvDir}, true, nil
	}
	return Located{}, false, nil
}

// locateInBundle vouches for the sidecar file exeName in d through the bundle manifest.json there (an installed
// launcher bundle) instead of rfe-sidecars.txt. A file that no longer matches is refused with an error, so the
// caller runs without that helper rather than starting a changed binary.
func locateInBundle(d, p, exeName string, fromEnvDir bool) (Located, bool, error) {
	info, err := bundle.VerifyFileInfo(d, exeName)
	if err != nil {
		return Located{}, false, fmt.Errorf("%s: %w", filepath.Join(d, bundle.ManifestName), err)
	}
	return Located{Path: p, Verified: true, Version: info.Version, SHA256: info.SHA256, DevDir: fromEnvDir}, true, nil
}

// LocateToRun is Locate plus the rule for starting what it found. A sidecar a manifest vouches for is always
// allowed. One without a manifest is a development build and is allowed only when the owner asked for it
// (explicit: RFE_SIDECARS is set) and it sits in $RFE_SIDECAR_DIR; beside the agent executable, a missing manifest
// means a damaged or tampered install, so it is refused with ErrUnverifiedNotAllowed.
func LocateToRun(short string, explicit bool) (loc Located, ok bool, err error) {
	loc, ok, err = Locate(short)
	if err != nil || !ok || loc.Verified {
		return loc, ok, err
	}
	if explicit && loc.DevDir {
		return loc, true, nil
	}
	return Located{}, false, fmt.Errorf("%s: %w", loc.Path, ErrUnverifiedNotAllowed)
}

// VerifyFile checks that the file at path still hashes to want (lowercase hex). It is called before every start of
// a verified sidecar, so a file swapped after the first start is refused.
func VerifyFile(path, want string) error {
	got, err := fileSHA256(path)
	if err != nil {
		return err
	}
	if got != want {
		return fmt.Errorf("%w: %s (sha256 %s, expected %s)", ErrHashChanged, path, got, want)
	}
	return nil
}
