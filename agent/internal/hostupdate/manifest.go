package hostupdate

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/launcher"
)

// Bundle is one downloadable archive of a release for one OS and CPU.
type Bundle struct {
	GOOS   string `json:"goos"`
	GOARCH string `json:"goarch"`
	URL    string `json:"url"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// Release is the signed release manifest. MinInstalled, when set, is the oldest installed version that may
// update straight to this release.
type Release struct {
	Version      string    `json:"version"`
	Released     time.Time `json:"released"`
	MinInstalled string    `json:"min_installed,omitempty"`
	Bundles      []Bundle  `json:"bundles"`
}

// ParseRelease decodes and validates manifest JSON: a version usable as a directory name and valid as semver,
// a release time, and for every bundle a platform, a URL, a positive size within the hard cap and a 64-digit
// SHA-256. Two bundles for the same platform are refused as ambiguous. It does not check any signature.
func ParseRelease(data []byte) (*Release, error) {
	var r Release
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrBadManifest, err)
	}
	if !launcher.ValidVersion(r.Version) {
		return nil, fmt.Errorf("%w: version %q is not usable as a directory name", ErrBadManifest, r.Version)
	}
	if _, err := parseSemver(r.Version); err != nil {
		return nil, err
	}
	if r.MinInstalled != "" {
		if _, err := parseSemver(r.MinInstalled); err != nil {
			return nil, err
		}
	}
	if r.Released.IsZero() {
		return nil, fmt.Errorf("%w: released time is missing", ErrBadManifest)
	}
	if len(r.Bundles) == 0 {
		return nil, fmt.Errorf("%w: no bundles", ErrBadManifest)
	}
	seen := map[string]bool{}
	for i, b := range r.Bundles {
		if b.GOOS == "" || b.GOARCH == "" || b.URL == "" {
			return nil, fmt.Errorf("%w: bundle %d lacks goos, goarch or url", ErrBadManifest, i)
		}
		if b.Size <= 0 || b.Size > maxBundleBytes {
			return nil, fmt.Errorf("%w: bundle %d has size %d", ErrBadManifest, i, b.Size)
		}
		if raw, err := hex.DecodeString(b.SHA256); err != nil || len(raw) != 32 {
			return nil, fmt.Errorf("%w: bundle %d has a bad sha256", ErrBadManifest, i)
		}
		key := b.GOOS + "/" + b.GOARCH
		if seen[key] {
			return nil, fmt.Errorf("%w: two bundles for %s", ErrBadManifest, key)
		}
		seen[key] = true
	}
	return &r, nil
}

// Verify checks that sigB64 (base64 of a 64-byte Ed25519 signature, surrounding whitespace allowed) is a valid
// signature of exactly msg under the primary or the spare key. With no key configured it always fails
// (ErrNoTrustedKeys), so an unconfigured build can never accept an update.
func (k Keys) Verify(msg, sigB64 []byte) error {
	var keys []ed25519.PublicKey
	for _, key := range []ed25519.PublicKey{k.Primary, k.Spare} {
		if len(key) == ed25519.PublicKeySize {
			keys = append(keys, key)
		}
	}
	if len(keys) == 0 {
		return ErrNoTrustedKeys
	}
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sigB64)))
	if err != nil || len(sig) != ed25519.SignatureSize {
		return fmt.Errorf("%w: signature is not base64 of %d bytes", ErrBadSignature, ed25519.SignatureSize)
	}
	for _, key := range keys {
		if ed25519.Verify(key, msg, sig) {
			return nil
		}
	}
	return ErrBadSignature
}

// PickBundle chooses the bundle for a machine: the one for goos/goarch of the running binary. The single
// exception is goos "windows" with goarch "amd64" running under emulation on an arm64 machine
// (emulatedHostArch "arm64"): the arm64 bundle is preferred, and the amd64 one is the fallback. A release with
// neither returns ErrWrongArch.
func PickBundle(r Release, goos, goarch, emulatedHostArch string) (Bundle, error) {
	find := func(arch string) (Bundle, bool) {
		for _, b := range r.Bundles {
			if b.GOOS == goos && b.GOARCH == arch {
				return b, true
			}
		}
		return Bundle{}, false
	}
	if goos == "windows" && goarch == "amd64" && emulatedHostArch == "arm64" {
		if b, ok := find("arm64"); ok {
			return b, nil
		}
	}
	if b, ok := find(goarch); ok {
		return b, nil
	}
	return Bundle{}, fmt.Errorf("%w: %s/%s (release %s offers %s)", ErrWrongArch, goos, goarch, r.Version, offered(r))
}

// offered lists the platforms of a release for an error message.
func offered(r Release) string {
	var s []string
	for _, b := range r.Bundles {
		s = append(s, b.GOOS+"/"+b.GOARCH)
	}
	return strings.Join(s, ", ")
}
