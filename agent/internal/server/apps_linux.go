package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const maxDesktopEntries = 10000
const maxDesktopEntryBytes = 1 << 20

var safeIconNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,79}$`)

func listHostApps() (string, []appRecord, error) {
	dirs := xdgApplicationDirs()
	seenDesktopIDs := make(map[string]struct{})
	apps := make([]appRecord, 0)
	visited := 0

	for _, dataDir := range dirs {
		applicationsDir := filepath.Join(dataDir, "applications")
		_ = filepath.WalkDir(applicationsDir, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil || entry == nil {
				return nil
			}
			if entry.IsDir() {
				return nil
			}
			if !strings.HasSuffix(strings.ToLower(entry.Name()), ".desktop") {
				return nil
			}
			visited++
			if visited > maxDesktopEntries {
				return fs.SkipAll
			}

			rel, err := filepath.Rel(applicationsDir, path)
			if err != nil {
				return nil
			}
			desktopID := strings.ReplaceAll(rel, string(os.PathSeparator), "-")
			if _, exists := seenDesktopIDs[desktopID]; exists {
				return nil
			}
			// A higher-priority desktop entry masks lower-priority entries even
			// when it is Hidden, malformed, or a symlink tombstone.
			seenDesktopIDs[desktopID] = struct{}{}

			if entry.Type()&fs.ModeSymlink != 0 {
				return nil
			}
			info, err := entry.Info()
			if err != nil || !info.Mode().IsRegular() || info.Size() > maxDesktopEntryBytes {
				return nil
			}
			name, icon, fields, err := readDesktopApplication(path)
			if err != nil || !desktopEntryVisible(fields) {
				return nil
			}
			name = safeAppDisplay(name)
			if name == "" {
				return nil
			}
			if !safeIconNamePattern.MatchString(icon) {
				icon = ""
			}
			launchable := desktopEntryLaunchable(fields)
			app := appRecord{
				ID:         desktopAppID(desktopID),
				Name:       name,
				Icon:       icon,
				Category:   desktopCategory(fields["Categories"]),
				Launchable: launchable,
			}
			if launchable {
				app.launchRef = path
			}
			apps = append(apps, app)
			return nil
		})
		if visited > maxDesktopEntries {
			break
		}
	}

	sort.Slice(apps, func(i, j int) bool {
		left, right := strings.ToLower(apps[i].Name), strings.ToLower(apps[j].Name)
		if left == right {
			return apps[i].ID < apps[j].ID
		}
		return left < right
	})
	return "linux", apps, nil
}

func launchHostApp(ctx context.Context, requested appRecord) error {
	if !requested.Launchable {
		return errAppNotLaunchable
	}
	if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
		return errNoDesktopSession
	}
	gio, err := exec.LookPath("gio")
	if err != nil {
		return errLauncherMissing
	}

	// Rebuild the current catalog immediately before launch. The client only
	// supplies an opaque ID; local references are recovered from current XDG
	// registrations and never accepted from the request.
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

	launchCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(launchCtx, gio, "launch", current.launchRef)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		// GIO can confirm that it accepted a launch even if the app later fails
		// during startup; the API reports only this handoff result.
		return errAppStartFailed
	}
	return nil
}

func xdgApplicationDirs() []string {
	var dirs []string
	dataHome := os.Getenv("XDG_DATA_HOME")
	if !filepath.IsAbs(dataHome) {
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			dataHome = filepath.Join(home, ".local", "share")
		} else {
			dataHome = ""
		}
	}
	if dataHome != "" {
		dirs = append(dirs, filepath.Clean(dataHome))
	}

	dataDirs := os.Getenv("XDG_DATA_DIRS")
	if dataDirs == "" {
		dataDirs = "/usr/local/share:/usr/share"
	}
	for _, dir := range filepath.SplitList(dataDirs) {
		if filepath.IsAbs(dir) {
			dirs = append(dirs, filepath.Clean(dir))
		}
	}
	return dirs
}

func desktopAppID(desktopID string) string {
	sum := sha256.Sum256([]byte("rfe-host-app-v1\x00" + desktopID))
	return "app_" + hex.EncodeToString(sum[:])
}

func readDesktopApplication(path string) (string, string, map[string]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", "", nil, err
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, maxDesktopEntryBytes+1))
	if err != nil {
		return "", "", nil, err
	}
	if len(data) > maxDesktopEntryBytes {
		return "", "", nil, os.ErrInvalid
	}

	fields := make(map[string]string)
	inDesktopEntry := false
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSuffix(line, "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "[Desktop Entry]" {
			inDesktopEntry = true
			continue
		}
		if strings.HasPrefix(trimmed, "[") {
			inDesktopEntry = false
			continue
		}
		if !inDesktopEntry || trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		fields[key] = unescapeDesktopString(strings.TrimSpace(value))
	}

	if fields["Type"] != "Application" {
		return "", "", fields, os.ErrInvalid
	}
	return fields["Name"], fields["Icon"], fields, nil
}

func unescapeDesktopString(value string) string {
	var b strings.Builder
	b.Grow(len(value))
	for i := 0; i < len(value); i++ {
		if value[i] != '\\' || i+1 >= len(value) {
			b.WriteByte(value[i])
			continue
		}
		i++
		switch value[i] {
		case 's':
			b.WriteByte(' ')
		case 'n':
			b.WriteByte('\n')
		case 't':
			b.WriteByte('\t')
		case 'r':
			b.WriteByte('\r')
		case '\\':
			b.WriteByte('\\')
		default:
			b.WriteByte(value[i])
		}
	}
	return b.String()
}

func desktopEntryVisible(fields map[string]string) bool {
	if strings.EqualFold(fields["Hidden"], "true") || strings.EqualFold(fields["NoDisplay"], "true") {
		return false
	}
	desktops := desktopList(os.Getenv("XDG_CURRENT_DESKTOP"), ":")
	if only := desktopList(fields["OnlyShowIn"], ";"); len(only) > 0 && len(desktops) > 0 && !intersects(only, desktops) {
		return false
	}
	if excluded := desktopList(fields["NotShowIn"], ";"); len(excluded) > 0 && intersects(excluded, desktops) {
		return false
	}
	return true
}

func desktopEntryLaunchable(fields map[string]string) bool {
	if fields["Exec"] == "" && !strings.EqualFold(fields["DBusActivatable"], "true") {
		return false
	}
	tryExec := strings.TrimSpace(fields["TryExec"])
	if tryExec == "" {
		return true
	}
	if strings.ContainsAny(tryExec, " \t\r\n") {
		return false
	}
	_, err := exec.LookPath(tryExec)
	return err == nil
}

func desktopList(value, separator string) []string {
	var out []string
	for _, item := range strings.Split(value, separator) {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}

func intersects(a, b []string) bool {
	for _, left := range a {
		for _, right := range b {
			if left == right {
				return true
			}
		}
	}
	return false
}

// desktopCategories maps freedesktop main categories to the short labels the phone shows, in priority order
// (an app listing both Development and Utility is shown as Development).
var desktopCategories = []struct{ key, label string }{
	{"Development", "Development"},
	{"WebBrowser", "Internet"},
	{"Network", "Internet"},
	{"Office", "Office"},
	{"AudioVideo", "Media"},
	{"Audio", "Media"},
	{"Video", "Media"},
	{"Graphics", "Graphics"},
	{"Game", "Games"},
	{"System", "System"},
	{"Settings", "System"},
	{"Utility", "Utilities"},
}

// desktopCategory returns the label for the first matching main category of a Categories= value, or "".
func desktopCategory(categories string) string {
	have := map[string]bool{}
	for _, c := range strings.Split(categories, ";") {
		have[strings.TrimSpace(c)] = true
	}
	for _, c := range desktopCategories {
		if have[c.key] {
			return c.label
		}
	}
	return ""
}
