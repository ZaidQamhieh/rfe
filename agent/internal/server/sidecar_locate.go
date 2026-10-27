package server

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/ZaidQamhieh/rfe/agent/internal/sidecar"
)

// sidecarManifestName is written next to the sidecars by the release packaging script:
//
//	rfe-sidecars 1
//	version 1.4.0
//	sha256 <hex> rfe-indexd
//	sha256 <hex> rfe-thumbd
//
// A sidecar listed there is only started when its bytes hash to the listed value and it announces the listed
// version, so a half-updated install (new agent, old sidecar) or a corrupted file is refused, not run.
const sidecarManifestName = "rfe-sidecars.txt"

type sidecarManifest struct {
	version string
	sums    map[string]string
}

func readSidecarManifest(dir string) (*sidecarManifest, error) {
	f, err := os.Open(filepath.Join(dir, sidecarManifestName))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	m := &sidecarManifest{sums: map[string]string{}}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		switch {
		case len(fields) == 2 && fields[0] == "version":
			m.version = fields[1]
		case len(fields) == 3 && fields[0] == "sha256":
			m.sums[fields[2]] = strings.ToLower(fields[1])
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if m.version == "" {
		return nil, fmt.Errorf("%s has no version line", sidecarManifestName)
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

// located is a sidecar executable found on disk.
type located struct {
	path string
	// verified: a manifest vouches for it (hash and version checked).
	verified bool
	version  string
}

// locateSidecar finds name in $RFE_SIDECAR_DIR, else next to the agent executable, and checks it against the
// manifest there. A listed sidecar that does not match is refused (ok is false and err says why); a directory with
// no manifest (a development build) is accepted unverified.
func locateSidecar(name string) (loc located, ok bool, err error) {
	exeName := name
	if runtime.GOOS == "windows" {
		exeName += ".exe"
	}
	dirs := []string{os.Getenv("RFE_SIDECAR_DIR")}
	if exe, e := os.Executable(); e == nil {
		if resolved, e := filepath.EvalSymlinks(exe); e == nil {
			exe = resolved
		}
		dirs = append(dirs, filepath.Dir(exe))
	}
	for _, d := range dirs {
		if d == "" {
			continue
		}
		p := filepath.Join(d, exeName)
		if st, e := os.Stat(p); e != nil || !st.Mode().IsRegular() {
			continue
		}
		m, e := readSidecarManifest(d)
		if e != nil {
			if os.IsNotExist(e) {
				return located{path: p}, true, nil
			}
			return located{}, false, fmt.Errorf("%s: %w", filepath.Join(d, sidecarManifestName), e)
		}
		want, listed := m.sums[exeName]
		if !listed {
			want, listed = m.sums[name]
		}
		if !listed {
			return located{}, false, fmt.Errorf("%s is not listed in %s", exeName, sidecarManifestName)
		}
		got, e := fileSHA256(p)
		if e != nil {
			return located{}, false, e
		}
		if got != want {
			return located{}, false, fmt.Errorf("%s does not match %s (sha256 %s, expected %s)", exeName, sidecarManifestName, got, want)
		}
		return located{path: p, verified: true, version: m.version}, true, nil
	}
	return located{}, false, nil
}

// sidecarEnabled decides whether to use sidecar name. RFE_SIDECARS lists names (or "all"/"1") to turn on and
// "off"/"none"/"0" to turn everything off. Left unset, a sidecar is used when a release package installed it:
// verified against its manifest. A development build is only used when asked for.
func sidecarEnabled(name string) bool {
	raw := strings.ToLower(strings.TrimSpace(os.Getenv("RFE_SIDECARS")))
	if raw == "" {
		loc, ok, _ := locateSidecar("rfe-" + name)
		return ok && loc.verified
	}
	for _, v := range strings.Split(raw, ",") {
		switch strings.TrimSpace(v) {
		case "off", "none", "0", "false":
			return false
		}
	}
	for _, v := range strings.Split(raw, ",") {
		switch strings.TrimSpace(v) {
		case name, "all", "1", "true":
			return true
		}
	}
	return false
}

// sidecarConfig is the supervisor configuration for a sidecar that is installed and, when a manifest vouches for
// it, intact. Problems are logged once here.
func sidecarConfig(name string) (sidecar.Config, bool) {
	loc, ok, err := locateSidecar(name)
	if err != nil {
		log.Printf("sidecar %s refused: %v", name, err)
		sidecar.Note(name + ".refused")
		return sidecar.Config{}, false
	}
	if !ok {
		return sidecar.Config{}, false
	}
	cfg := sidecar.Config{Name: name, Path: loc.path, Logf: log.Printf}
	if loc.verified {
		cfg.ExpectVersion = loc.version
	}
	return cfg, true
}

// SidecarReport describes the sidecars next to this agent, for `rfe status`.
func SidecarReport() []string {
	var out []string
	for _, name := range []string{"rfe-indexd", "rfe-thumbd"} {
		loc, ok, err := locateSidecar(name)
		switch {
		case err != nil:
			out = append(out, fmt.Sprintf("%s: REFUSED (%v)", name, err))
		case !ok:
			out = append(out, name+": not installed (this build runs it in-process; install the packaged release)")
		case loc.verified:
			out = append(out, fmt.Sprintf("%s: verified, version %s", name, loc.version))
		default:
			out = append(out, name+": found, unverified (no "+sidecarManifestName+"); used only with RFE_SIDECARS")
		}
	}
	return out
}
