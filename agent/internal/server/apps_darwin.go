//go:build darwin

package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	maxMacApps             = 2000
	maxMacAppCandidates    = 5000
	maxMacScanEntries      = 50000
	maxMacInfoPlistBytes   = 2 << 20
	maxMacPlistOutputBytes = 4 << 20
)

var (
	macBundleIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.-]{0,254}$`)
	macIconNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,79}$`)
)

type macAppRoot struct {
	path      string
	identity  string
	singleApp bool
}

type macBundleMetadata struct {
	BundleID string `json:"CFBundleIdentifier"`
	Display  string `json:"CFBundleDisplayName"`
	Name     string `json:"CFBundleName"`
	Package  string `json:"CFBundlePackageType"`
	Icon     string `json:"CFBundleIconFile"`
}

type macPlistCapture struct {
	buffer bytes.Buffer
	limit  int
	tooBig bool
	cancel context.CancelFunc
}

func (c *macPlistCapture) Write(p []byte) (int, error) {
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
	// Consume the stream after cancellation to avoid blocking the child on a
	// full pipe. The process is canceled immediately when the cap is crossed.
	return len(p), nil
}

func listHostApps() (string, []appRecord, error) {
	plutil := "/usr/bin/plutil"
	if info, err := os.Stat(plutil); err != nil || !info.Mode().IsRegular() {
		return "darwin", nil, errLauncherMissing
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	roots := macApplicationRoots()
	apps := make([]appRecord, 0)
	seenIdentity := make(map[string]struct{})
	candidates := 0
	entries := 0
	limitHit := false

	for _, root := range roots {
		if ctx.Err() != nil || len(apps) >= maxMacApps {
			break
		}
		if root.singleApp {
			entries++
			app, identity, ok := inspectMacApp(ctx, plutil, root.path, root, "")
			if ok {
				if _, exists := seenIdentity[identity]; !exists {
					seenIdentity[identity] = struct{}{}
					apps = append(apps, app)
				}
			}
			continue
		}

		rootInfo, err := os.Lstat(root.path)
		if err != nil || !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 {
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
			entries++
			if entries > maxMacScanEntries {
				limitHit = true
				return fs.SkipAll
			}
			if !entry.IsDir() {
				return nil
			}
			if !strings.EqualFold(filepath.Ext(entry.Name()), ".app") {
				return nil
			}
			candidates++
			if candidates > maxMacAppCandidates {
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
			app, identity, ok := inspectMacApp(ctx, plutil, path, root, rel)
			if ok {
				if _, exists := seenIdentity[identity]; !exists {
					seenIdentity[identity] = struct{}{}
					apps = append(apps, app)
				}
			}
			// Never recurse into an app bundle, including malformed bundles.
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
		return "darwin", nil, errLauncherMissing
	}

	sort.Slice(apps, func(i, j int) bool {
		left, right := strings.ToLower(apps[i].Name), strings.ToLower(apps[j].Name)
		if left == right {
			return apps[i].ID < apps[j].ID
		}
		return left < right
	})
	return "darwin", apps, nil
}

func macApplicationRoots() []macAppRoot {
	roots := []macAppRoot{
		{path: "/Applications", identity: "applications"},
		{path: "/System/Applications", identity: "system-applications"},
		{path: "/System/Library/CoreServices/Applications", identity: "core-services-applications"},
		{path: "/System/Library/CoreServices/Finder.app", identity: "finder", singleApp: true},
	}
	if home, err := os.UserHomeDir(); err == nil && filepath.IsAbs(home) {
		// User-installed applications take precedence over shared/system copies
		// with the same bundle identifier. Insert the root before global roots.
		roots = append([]macAppRoot{{path: filepath.Join(home, "Applications"), identity: "user-applications"}}, roots...)
	}
	return roots
}

func inspectMacApp(ctx context.Context, plutil, bundlePath string, root macAppRoot, relativePath string) (appRecord, string, bool) {
	bundleInfo, err := os.Lstat(bundlePath)
	if err != nil || !bundleInfo.IsDir() || bundleInfo.Mode()&os.ModeSymlink != 0 || !strings.EqualFold(filepath.Ext(bundlePath), ".app") {
		return appRecord{}, "", false
	}
	contentsPath := filepath.Join(bundlePath, "Contents")
	contentsInfo, err := os.Lstat(contentsPath)
	if err != nil || !contentsInfo.IsDir() || contentsInfo.Mode()&os.ModeSymlink != 0 {
		return appRecord{}, "", false
	}
	infoPath := filepath.Join(contentsPath, "Info.plist")
	plistInfo, err := os.Lstat(infoPath)
	if err != nil || !plistInfo.Mode().IsRegular() || plistInfo.Mode()&os.ModeSymlink != 0 || plistInfo.Size() <= 0 || plistInfo.Size() > maxMacInfoPlistBytes {
		return appRecord{}, "", false
	}

	metadata, ok := readMacBundleMetadata(ctx, plutil, infoPath)
	if !ok || metadata.Package != "APPL" {
		return appRecord{}, "", false
	}
	identity := ""
	bundleID := metadata.BundleID
	if strings.TrimSpace(bundleID) == bundleID && macBundleIDPattern.MatchString(bundleID) {
		identity = "bundle:" + strings.ToLower(bundleID)
	} else if relativePath != "" {
		identity = "path:" + root.identity + ":" + filepath.ToSlash(relativePath)
	} else {
		return appRecord{}, "", false
	}

	name := metadata.Display
	if name == "" {
		name = metadata.Name
	}
	if name == "" {
		name = strings.TrimSuffix(filepath.Base(bundlePath), filepath.Ext(bundlePath))
	}
	name = safeAppDisplay(name)
	if name == "" {
		return appRecord{}, "", false
	}
	icon := safeMacIconName(metadata.Icon)
	return appRecord{
		ID:         macAppID(identity),
		Name:       name,
		Icon:       icon,
		Launchable: true,
		launchRef:  bundlePath,
	}, identity, true
}

func readMacBundleMetadata(ctx context.Context, plutil, infoPath string) (macBundleMetadata, bool) {
	cmdCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	// infoPath is constructed beneath a fixed absolute bundle root, so it
	// cannot be interpreted as a plutil option and remains a fixed argv value.
	cmd := exec.CommandContext(cmdCtx, plutil, "-convert", "json", "-o", "-", infoPath)
	capture := &macPlistCapture{limit: maxMacPlistOutputBytes, cancel: cancel}
	cmd.Stdout = capture
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil || capture.tooBig || cmdCtx.Err() != nil {
		return macBundleMetadata{}, false
	}
	var metadata macBundleMetadata
	if err := json.Unmarshal(capture.buffer.Bytes(), &metadata); err != nil {
		return macBundleMetadata{}, false
	}
	return metadata, true
}

func safeMacIconName(icon string) string {
	if icon == "" || strings.ContainsAny(icon, `/\\`) {
		return ""
	}
	icon = strings.TrimSuffix(strings.ToLower(icon), ".icns")
	if !macIconNamePattern.MatchString(icon) {
		return ""
	}
	return icon
}

func macAppID(identity string) string {
	sum := sha256.Sum256([]byte("rfe-host-app-darwin-v1\x00" + identity))
	return "app_" + hex.EncodeToString(sum[:])
}

func launchHostApp(ctx context.Context, requested appRecord) error {
	if !requested.Launchable {
		return errAppNotLaunchable
	}
	if !macHasActiveGUIUserSession() {
		return errNoDesktopSession
	}
	_, currentApps, err := listHostApps()
	if err != nil {
		return err
	}
	var current *appRecord
	for i := range currentApps {
		if currentApps[i].ID == requested.ID {
			current = &currentApps[i]
			break
		}
	}
	if current == nil {
		return errAppNotFound
	}
	if !current.Launchable {
		return errAppNotLaunchable
	}
	if !filepath.IsAbs(current.launchRef) {
		return errAppNotFound
	}
	open := "/usr/bin/open"
	if info, err := os.Stat(open); err != nil || !info.Mode().IsRegular() {
		return errLauncherMissing
	}
	launchCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(launchCtx, open, "-a", current.launchRef)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		return errAppStartFailed
	}
	return nil
}

func macHasActiveGUIUserSession() bool {
	if os.Getuid() <= 0 {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/bin/launchctl", "print", "gui/"+strconv.Itoa(os.Getuid()))
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	return cmd.Run() == nil
}
