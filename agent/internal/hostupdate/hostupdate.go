// Package hostupdate fetches, authenticates and stages a new version of the host agent. It never switches
// versions: it only leaves a verified bundle in <root>/versions/<version>, ready for the launcher's state machine
// (internal/launcher) to try later. It uses the standard library and golang.org/x/sys only: no cgo.
//
// Trust chain. A release is described by a small JSON manifest (version, release time, bundles per OS and CPU with
// URL, size and SHA-256) and a detached Ed25519 signature over the exact manifest bytes (a separate ".sig" file,
// base64). The signature must verify against one of two public keys compiled into the binary: a primary and a
// spare (so the primary can be rotated out without bricking installs). The bundle archive is then checked against the
// size and SHA-256 in the signed manifest, extracted with every unsafe entry refused, and the extracted tree is
// checked once more against the bundle's own manifest.json (internal/bundle).
//
// Signing keys. This repository holds NO production key. With empty keys, verification always fails closed
// (ErrNoTrustedKeys). Before the first release that ships this updater the owner must generate two Ed25519 key
// pairs OFFLINE, keep the private halves off every build machine, and inject the public halves (base64 of the
// 32-byte keys) at release build time:
//
//	go build -ldflags "-X github.com/ZaidQamhieh/rfe/agent/internal/hostupdate.PrimaryPublicKey=<base64> \
//	                   -X github.com/ZaidQamhieh/rfe/agent/internal/hostupdate.SparePublicKey=<base64>"
//
// The manifest URL can be injected the same way (ManifestURL). Tests inject their own keys through Config.Keys.
//
// Native architecture. The bundle matches the running binary (runtime.GOOS and GOARCH). The one exception is an
// x64 binary emulated on a Windows ARM64 machine (detected with IsWow64Process2 in platform_windows.go): there the
// arm64 bundle is preferred when the manifest has one, otherwise the amd64 bundle is used.
package hostupdate

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"time"
)

// Release-build injection points, set with -ldflags -X (see the package doc). Empty means "none".
var (
	// PrimaryPublicKey is the base64 of the primary Ed25519 public key (32 bytes).
	PrimaryPublicKey string
	// SparePublicKey is the base64 of the spare Ed25519 public key (32 bytes).
	SparePublicKey string
	// ManifestURL is the static https URL of the signed release manifest; the signature is at ManifestURL + ".sig".
	ManifestURL string
)

// Errors a caller can test with errors.Is. Every failure of Check and Stage is also wrapped in *Error.
var (
	ErrNoTrustedKeys     = errors.New("no trusted update signing key is configured")
	ErrBadSignature      = errors.New("release manifest signature does not verify")
	ErrBadManifest       = errors.New("release manifest is invalid")
	ErrBadVersion        = errors.New("version is not a valid semantic version")
	ErrDowngrade         = errors.New("release is older than the installed version")
	ErrAlreadyCurrent    = errors.New("release equals the installed version")
	ErrInstalledTooOld   = errors.New("installed version is older than the release's min_installed")
	ErrWrongArch         = errors.New("release has no bundle for this platform")
	ErrInsecureURL       = errors.New("url is not allowed (https required)")
	ErrInsufficientSpace = errors.New("not enough free disk space")
	ErrDownload          = errors.New("download failed")
	ErrTruncated         = errors.New("download is shorter than the manifest says")
	ErrTooLarge          = errors.New("download is longer than the manifest says")
	ErrHashMismatch      = errors.New("download does not match the manifest sha256")
	ErrUnsafeArchive     = errors.New("archive holds an unsafe entry")
	ErrArchiveTooLarge   = errors.New("archive exceeds the size limits")
	ErrBundleMismatch    = errors.New("extracted bundle does not match its own manifest")
	ErrTrialFailed       = errors.New("trial of the staged bundle failed")
	ErrRenameFailed      = errors.New("could not move the bundle into versions/")
	ErrVersionBad        = errors.New("version is marked bad in state.json")
	ErrVersionExists     = errors.New("versions/<version> already exists and is not a valid bundle")
	ErrConfig            = errors.New("invalid update configuration")
)

// Error is the typed error of Check and Stage: Op names the step that failed (manifest, signature, version,
// platform, disk, download, extract, verify, trial, install, config) and Err is the cause, a sentinel above or
// something wrapping one.
type Error struct {
	Op  string
	Err error
}

func (e *Error) Error() string { return "update " + e.Op + ": " + e.Err.Error() }
func (e *Error) Unwrap() error { return e.Err }

// opErr wraps err in an *Error for the step op (nil stays nil, an existing *Error is kept).
func opErr(op string, err error) error {
	if err == nil {
		return nil
	}
	var te *Error
	if errors.As(err, &te) {
		return err
	}
	return &Error{Op: op, Err: err}
}

// Keys are the public keys a manifest signature is checked against.
type Keys struct {
	Primary ed25519.PublicKey
	Spare   ed25519.PublicKey
}

// CompiledKeys decodes PrimaryPublicKey and SparePublicKey. A key that is empty, not base64 or not 32 bytes is
// left nil, so a bad injection can only make verification fail, never pass.
func CompiledKeys() Keys {
	return Keys{Primary: decodeKey(PrimaryPublicKey), Spare: decodeKey(SparePublicKey)}
}

// decodeKey turns a base64 public key into an ed25519 key, or nil if it is not exactly one.
func decodeKey(s string) ed25519.PublicKey {
	raw, err := base64.StdEncoding.DecodeString(s)
	if err != nil || len(raw) != ed25519.PublicKeySize {
		return nil
	}
	return ed25519.PublicKey(raw)
}

// Platform is the machine a bundle is chosen for. EmulatedHostArch is the CPU of the real machine when this
// binary runs under emulation ("arm64" for x64 on Windows ARM64), empty otherwise.
type Platform struct {
	GOOS             string
	GOARCH           string
	EmulatedHostArch string
}

// Limits bound what extraction may write; zero fields take the Default* values.
type Limits struct {
	MaxFileBytes  int64
	MaxTotalBytes int64
	MaxEntries    int
}

// Defaults for Limits and the download.
const (
	DefaultMaxFileBytes   = 1 << 30
	DefaultMaxTotalBytes  = 4 << 30
	DefaultMaxEntries     = 20000
	DefaultRenameAttempts = 6
	DefaultRenameBackoff  = 100 * time.Millisecond
	maxRenameBackoff      = 5 * time.Second
	maxManifestBytes      = 1 << 20
	maxSigBytes           = 4 << 10
	maxBundleBytes        = 4 << 30
	defaultStageTimeout   = 2 * time.Hour
	manifestTimeout       = 2 * time.Minute
)

// Config is the input of Check and Stage. Only ManifestURL and Installed are needed by Check; Stage also needs
// Root. Every func field is a test seam with a production default.
type Config struct {
	// Root is the launcher install root: Stage writes only under Root/staging and Root/versions.
	Root string
	// ManifestURL is the https URL of the manifest JSON; SigURL defaults to ManifestURL + ".sig".
	ManifestURL string
	SigURL      string
	// Installed is the version that is running now (semantic version).
	Installed string
	// Force allows a release equal to the installed version (never a lower one).
	Force bool
	// Keys overrides CompiledKeys (tests).
	Keys *Keys
	// HTTPClient, when set, replaces the default client and also allows plain http to a loopback IP (tests).
	HTTPClient *http.Client
	// Platform overrides NativePlatform.
	Platform *Platform
	// DatabaseBytes is the size of the agent database; Stage needs 2x bundle + 3x this much free space.
	DatabaseBytes int64
	// FreeSpace reports the free bytes available to the caller under dir (default: the platform call).
	FreeSpace func(dir string) (uint64, error)
	// Trial, when set, is called with the verified, still-staged bundle directory. An error discards the bundle
	// and marks the version bad in state.json.
	Trial func(bundleDir string) error
	// Rename moves a directory (default os.Rename); RenameRetryable says which of its errors are transient file
	// locks (default: the Windows sharing, lock and access-denied errors); Sleep is the backoff wait.
	Rename          func(oldPath, newPath string) error
	RenameRetryable func(error) bool
	Sleep           func(time.Duration)
	RenameAttempts  int
	RenameBackoff   time.Duration
	Limits          Limits
	// Timeout bounds the whole of Stage (default two hours).
	Timeout time.Duration
	// Logf receives progress lines (default: none).
	Logf func(format string, args ...any)
}

// Plan is what Check learned: a verified release and the bundle chosen for this platform.
type Plan struct {
	Release Release
	Bundle  Bundle
	// Newer is true when the release is above the installed version.
	Newer bool
}

// Result describes a successful Stage.
type Result struct {
	Version string
	// Dir is <root>/versions/<version>.
	Dir  string
	Plan Plan
	// AlreadyStaged is true when Dir already held a valid bundle of that version and nothing was downloaded.
	AlreadyStaged bool
	// TrialRan is true when Config.Trial was set and passed.
	TrialRan bool
}

// logf writes a progress line if the caller asked for them.
func (c *Config) logf(format string, args ...any) {
	if c.Logf != nil {
		c.Logf(format, args...)
	}
}

// describe renders a Bundle for a log line.
func (b Bundle) describe() string {
	return fmt.Sprintf("%s/%s %d bytes", b.GOOS, b.GOARCH, b.Size)
}
