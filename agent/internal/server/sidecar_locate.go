package server

import (
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/ZaidQamhieh/rfe/agent/internal/sidecar"
)

// sidecarEnvVar is the environment variable that turns sidecars on or off.
const sidecarEnvVar = "RFE_SIDECARS"

// sidecarFileName is the executable name of a sidecar given its short name: "indexd" is "rfe-indexd". Every other
// function in this package takes the short name and calls this for the full one.
func sidecarFileName(short string) string {
	return sidecar.FileName(short)
}

// sidecarSettingSet reports whether RFE_SIDECARS is set at all, to any value.
func sidecarSettingSet() bool {
	return os.Getenv(sidecarEnvVar) != ""
}

// locateSidecar is sidecar.Locate; see there.
func locateSidecar(short string) (sidecar.Located, bool, error) { return sidecar.Locate(short) }

// sidecarEnabled decides whether to use the sidecar short. RFE_SIDECARS lists short names (or "all"/"1") to turn on
// and "off"/"none"/"0" to turn everything off. Left unset, a sidecar is used when a release package installed it:
// verified against its manifest. A development build is only used when asked for.
func sidecarEnabled(short string) bool {
	raw := strings.ToLower(strings.TrimSpace(os.Getenv(sidecarEnvVar)))
	if raw == "" {
		loc, ok, _ := locateSidecar(short)
		return ok && loc.Verified
	}
	for _, v := range strings.Split(raw, ",") {
		switch strings.TrimSpace(v) {
		case "off", "none", "0", "false":
			return false
		}
	}
	for _, v := range strings.Split(raw, ",") {
		switch strings.TrimSpace(v) {
		case short, "all", "1", "true":
			return true
		}
	}
	return false
}

// sidecarConfig is the supervisor configuration for the sidecar short when it is installed and, when a manifest
// vouches for it, intact. Without a manifest it is allowed only as a development build (see sidecar.LocateToRun).
// The config carries the verified hash so every restart re-checks the file. Problems are logged once here.
func sidecarConfig(short string) (sidecar.Config, bool) {
	name := sidecarFileName(short)
	loc, ok, err := sidecar.LocateToRun(short, sidecarSettingSet())
	if err != nil {
		log.Printf("sidecar %s refused: %v", name, err)
		sidecar.Note(name + ".refused")
		return sidecar.Config{}, false
	}
	if !ok {
		return sidecar.Config{}, false
	}
	cfg := sidecar.Config{Name: name, Path: loc.Path, Logf: log.Printf}
	if loc.Verified {
		cfg.ExpectVersion = loc.Version
		cfg.SHA256 = loc.SHA256
	}
	return cfg, true
}

// SidecarReport describes the sidecars next to this agent, for `rfe status`.
func SidecarReport() []string {
	var out []string
	for _, short := range []string{"indexd", "thumbd"} {
		name := sidecarFileName(short)
		loc, ok, err := locateSidecar(short)
		switch {
		case err != nil:
			out = append(out, fmt.Sprintf("%s: REFUSED (%v)", name, err))
		case !ok:
			out = append(out, name+": not installed (this build runs it in-process; install the packaged release)")
		case loc.Verified:
			out = append(out, fmt.Sprintf("%s: verified, version %s", name, loc.Version))
		case !loc.DevDir:
			out = append(out, name+": found beside the agent without "+sidecar.ManifestName+": NOT used (install the whole packaged release)")
		default:
			out = append(out, name+": found, unverified (no "+sidecar.ManifestName+"); used only with RFE_SIDECARS")
		}
	}
	return out
}
