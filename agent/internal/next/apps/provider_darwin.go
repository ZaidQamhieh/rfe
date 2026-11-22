//go:build darwin

package apps

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Limits of the macOS bundle scan (the old agent's) and the fixed programs.
const (
	maxMacApps             = 2000
	maxMacAppCandidates    = 5000
	maxMacScanEntries      = 50000
	maxMacInfoPlistBytes   = 2 << 20
	maxMacPlistOutputBytes = 4 << 20
	macScanTimeout         = 20 * time.Second
	macPlistTimeout        = 3 * time.Second
	macSessionTimeout      = 2 * time.Second

	plutilPath    = "/usr/bin/plutil"
	openPath      = "/usr/bin/open"
	launchctlPath = "/bin/launchctl"
	appSuffix     = ".app"
)

// macRoot is one place to look for bundles; singleApp means path is itself a bundle.
type macRoot struct {
	path      string
	identity  string
	singleApp bool
}

// macProvider scans the standard .app locations and launches with open(1).
type macProvider struct{ run runFunc }

func platformProviders() (Lister, Launcher) {
	p := macProvider{run: execRun}
	return p, p
}

// capture collects a child's stdout up to limit bytes and cancels the child
// when the limit is crossed, still consuming the stream so it cannot block.
type capture struct {
	buffer bytes.Buffer
	limit  int
	tooBig bool
	cancel context.CancelFunc
}

// Write stores what fits under the limit and cancels the process beyond it.
func (c *capture) Write(p []byte) (int, error) {
	remaining := c.limit - c.buffer.Len()
	if remaining > len(p) {
		remaining = len(p)
	}
	if remaining > 0 {
		_, _ = c.buffer.Write(p[:remaining])
	}
	if remaining < len(p) {
		c.tooBig = true
		c.cancel()
	}
	return len(p), nil
}

// macRoots returns the scan roots, the user's Applications folder first so a user
// copy wins over a shared one with the same bundle id.
func macRoots() []macRoot {
	roots := []macRoot{
		{path: "/Applications", identity: "applications"},
		{path: "/System/Applications", identity: "system-applications"},
		{path: "/System/Library/CoreServices/Applications", identity: "core-services-applications"},
		{path: "/System/Library/CoreServices/Finder.app", identity: "finder", singleApp: true},
	}
	if home, err := os.UserHomeDir(); err == nil && filepath.IsAbs(home) {
		roots = append([]macRoot{{path: filepath.Join(home, "Applications"), identity: "user-applications"}}, roots...)
	}
	return roots
}

// List scans the roots for .app bundles (never entering a bundle, never
// following a symlink), reads each Info.plist through plutil, and returns the
// apps sorted. Hitting a scan limit or the time budget is ErrProviderMissing:
// a partial catalog is not returned as if it were complete.
func (p macProvider) List(ctx context.Context) (string, []Listing, error) {
	if info, err := os.Stat(plutilPath); err != nil || !info.Mode().IsRegular() {
		return PlatformDarwin, nil, ErrProviderMissing
	}
	ctx, cancel := context.WithTimeout(ctx, macScanTimeout)
	defer cancel()

	apps := make([]Listing, 0)
	seen := make(map[string]struct{})
	candidates, entries := 0, 0
	limitHit := false
	add := func(app Listing, identity string, ok bool) {
		if !ok {
			return
		}
		if _, exists := seen[identity]; !exists {
			seen[identity] = struct{}{}
			apps = append(apps, app)
		}
	}

	for _, root := range macRoots() {
		if ctx.Err() != nil || len(apps) >= maxMacApps {
			break
		}
		if root.singleApp {
			entries++
			add(inspectBundle(ctx, root.path, root, ""))
			continue
		}
		if info, err := os.Lstat(root.path); err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		_ = filepath.WalkDir(root.path, func(path string, entry fs.DirEntry, walkErr error) error {
			if ctx.Err() != nil {
				limitHit = true
				return fs.SkipAll
			}
			if walkErr != nil || entry == nil {
				return nil
			}
			if entries++; entries > maxMacScanEntries {
				limitHit = true
				return fs.SkipAll
			}
			if !entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), appSuffix) {
				return nil
			}
			if candidates++; candidates > maxMacAppCandidates {
				limitHit = true
				return fs.SkipAll
			}
			if entry.Type()&fs.ModeSymlink != 0 {
				return filepath.SkipDir
			}
			rel, err := filepath.Rel(root.path, path)
			if err != nil {
				return filepath.SkipDir
			}
			add(inspectBundle(ctx, path, root, rel))
			if len(apps) >= maxMacApps {
				return fs.SkipAll
			}
			return filepath.SkipDir
		})
		if limitHit || len(apps) >= maxMacApps {
			break
		}
	}
	if limitHit || ctx.Err() != nil {
		return PlatformDarwin, nil, ErrProviderMissing
	}
	sortApps(apps)
	return PlatformDarwin, apps, nil
}

// inspectBundle checks that bundlePath is a real (non-symlink) bundle whose
// Contents and Info.plist are regular and of sane size, then reads the plist.
func inspectBundle(ctx context.Context, bundlePath string, root macRoot, relative string) (Listing, string, bool) {
	info, err := os.Lstat(bundlePath)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || !strings.EqualFold(filepath.Ext(bundlePath), appSuffix) {
		return Listing{}, "", false
	}
	contents := filepath.Join(bundlePath, "Contents")
	if ci, err := os.Lstat(contents); err != nil || !ci.IsDir() || ci.Mode()&os.ModeSymlink != 0 {
		return Listing{}, "", false
	}
	plist := filepath.Join(contents, "Info.plist")
	pi, err := os.Lstat(plist)
	if err != nil || !pi.Mode().IsRegular() || pi.Size() <= 0 || pi.Size() > maxMacInfoPlistBytes {
		return Listing{}, "", false
	}
	meta, ok := readMetadata(ctx, plist)
	if !ok {
		return Listing{}, "", false
	}
	return macListing(meta, bundlePath, root.identity, relative)
}

// readMetadata converts an Info.plist to JSON with plutil (fixed arguments, the
// plist path is built under an absolute bundle root) and decodes it, bounded in
// time and output size.
func readMetadata(ctx context.Context, infoPath string) (macBundleMetadata, bool) {
	cmdCtx, cancel := context.WithTimeout(ctx, macPlistTimeout)
	defer cancel()
	cmd := exec.CommandContext(cmdCtx, plutilPath, "-convert", "json", "-o", "-", infoPath)
	out := &capture{limit: maxMacPlistOutputBytes, cancel: cancel}
	cmd.Stdout = out
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil || out.tooBig || cmdCtx.Err() != nil {
		return macBundleMetadata{}, false
	}
	var meta macBundleMetadata
	if err := json.Unmarshal(out.buffer.Bytes(), &meta); err != nil {
		return macBundleMetadata{}, false
	}
	return meta, true
}

// Launch runs "open -a <bundle path>" for a bundle path from the fresh
// listing, only when a non-root user has a GUI session.
func (p macProvider) Launch(ctx context.Context, app Listing) error {
	if !app.Launchable {
		return ErrNotLaunchable
	}
	if !macHasGUISession(ctx, p.run) {
		return ErrNoDesktopSession
	}
	if !filepath.IsAbs(app.Ref) {
		return ErrGone
	}
	if info, err := os.Stat(openPath); err != nil || !info.Mode().IsRegular() {
		return ErrProviderMissing
	}
	if err := p.run(ctx, openPath, "-a", app.Ref); err != nil {
		return ErrStartFailed
	}
	return nil
}

// macHasGUISession reports whether launchd knows a GUI domain for this
// (non-root) user.
func macHasGUISession(ctx context.Context, run runFunc) bool {
	uid := os.Getuid()
	if uid <= 0 {
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, macSessionTimeout)
	defer cancel()
	return run(ctx, launchctlPath, "print", "gui/"+strconv.Itoa(uid)) == nil
}
