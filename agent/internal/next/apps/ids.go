package apps

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os/exec"
	"regexp"
	"sort"
	"strings"
)

// Domain separators of the app id hash. They are the old agent's, so an id a
// phone remembered keeps meaning the same app.
const (
	linuxIDDomain   = "rfe-host-app-v1"
	windowsIDDomain = "rfe-host-app-windows-v1"
	darwinIDDomain  = "rfe-host-app-darwin-v1"
	idDomainSep     = "\x00"
)

// safeIconNamePattern accepts a plain theme icon name (no path, no dot first);
// iconName adds the length cap.
var safeIconNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// makeID returns the opaque id for an identity under a domain: the prefix and
// the hex SHA-256 of "domain NUL identity".
func makeID(domain, identity string) string {
	sum := sha256.Sum256([]byte(domain + idDomainSep + identity))
	return idPrefix + hex.EncodeToString(sum[:])
}

// displayName cleans a registered name for display: control characters are
// dropped, slashes become spaces, the result is cut to MaxDisplayBytes and
// trimmed. It keeps a hostile name from forging log lines or looking like a path.
func displayName(value string) string {
	var b strings.Builder
	for _, r := range value {
		if r < 0x20 || r == 0x7f || r == '/' || r == '\\' {
			if r == '/' || r == '\\' {
				b.WriteByte(' ')
			}
			continue
		}
		if b.Len() >= MaxDisplayBytes {
			break
		}
		b.WriteRune(r)
	}
	return strings.TrimSpace(b.String())
}

// iconName returns icon when it is a safe theme icon name, else "".
func iconName(icon string) string {
	if len(icon) > maxIconNameLen || !safeIconNamePattern.MatchString(icon) {
		return ""
	}
	return icon
}

// sortApps orders entries by name (case-insensitive), then by id.
func sortApps(apps []Listing) {
	sort.Slice(apps, func(i, j int) bool {
		left, right := strings.ToLower(apps[i].Name), strings.ToLower(apps[j].Name)
		if left == right {
			return apps[i].ID < apps[j].ID
		}
		return left < right
	})
}

// normalize applies the service's own rules to whatever a Lister returned, so a
// careless provider cannot leak data: an entry with a malformed id or an empty
// name is dropped, a repeated id keeps its first entry, the name is cleaned, the
// icon must be a plain theme name, and a non-launchable entry loses its Ref.
func normalize(in []Listing) []Listing {
	out := make([]Listing, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for _, l := range in {
		if _, ok := canonicalID(l.ID); !ok {
			continue
		}
		if _, dup := seen[l.ID]; dup {
			continue
		}
		l.Name = displayName(l.Name)
		if l.Name == "" {
			continue
		}
		seen[l.ID] = struct{}{}
		l.Icon = iconName(l.Icon)
		l.Description = displayName(l.Description)
		if !l.Launchable {
			l.Ref = ""
		}
		out = append(out, l)
	}
	return out
}

// runFunc runs a native program with fixed arguments and no shell, discarding
// its output. It is the seam tests replace.
type runFunc func(ctx context.Context, name string, args ...string) error

// execRun is the production runFunc.
func execRun(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	return cmd.Run()
}
