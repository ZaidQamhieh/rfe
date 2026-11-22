package apps

import (
	"path/filepath"
	"regexp"
	"strings"
)

// macBundlePackageType is the CFBundlePackageType of an application bundle.
const macBundlePackageType = "APPL"

// macIconSuffix is the extension of a bundle icon file name.
const macIconSuffix = ".icns"

// macBundleIDPattern accepts a reverse-DNS bundle identifier.
var macBundleIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.-]{0,254}$`)

// macBundleMetadata is the part of an Info.plist (converted to JSON) the
// catalog reads.
type macBundleMetadata struct {
	BundleID string `json:"CFBundleIdentifier"`
	Display  string `json:"CFBundleDisplayName"`
	Name     string `json:"CFBundleName"`
	Package  string `json:"CFBundlePackageType"`
	Icon     string `json:"CFBundleIconFile"`
}

// macIconName returns the bundle's icon file name as a safe lower-case theme
// name (without ".icns"), or "" when it has a path or odd characters.
func macIconName(icon string) string {
	if icon == "" || strings.ContainsAny(icon, `/\`) {
		return ""
	}
	return iconName(strings.TrimSuffix(strings.ToLower(icon), macIconSuffix))
}

// macListing builds the entry of the bundle at bundlePath from its metadata.
// The identity that feeds the id is the bundle id when it is well formed (so a
// user copy and a system copy of one app are the same app), else the bundle's
// path under its scan root; a bundle with neither is rejected. It returns the
// identity so the caller can drop duplicates.
func macListing(meta macBundleMetadata, bundlePath, rootIdentity, relative string) (Listing, string, bool) {
	if meta.Package != macBundlePackageType {
		return Listing{}, "", false
	}
	var identity string
	switch {
	case strings.TrimSpace(meta.BundleID) == meta.BundleID && macBundleIDPattern.MatchString(meta.BundleID):
		identity = "bundle:" + strings.ToLower(meta.BundleID)
	case relative != "":
		identity = "path:" + rootIdentity + ":" + filepath.ToSlash(relative)
	default:
		return Listing{}, "", false
	}
	name := meta.Display
	if name == "" {
		name = meta.Name
	}
	if name == "" {
		name = strings.TrimSuffix(filepath.Base(bundlePath), filepath.Ext(bundlePath))
	}
	if name = displayName(name); name == "" {
		return Listing{}, "", false
	}
	return Listing{
		App: App{ID: makeID(darwinIDDomain, identity), Name: name, Icon: macIconName(meta.Icon), Launchable: true},
		Ref: bundlePath,
	}, identity, true
}
