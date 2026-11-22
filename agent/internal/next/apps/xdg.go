package apps

import (
	"context"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Limits of the XDG scan (the old agent's).
const (
	maxDesktopEntries    = 10000
	maxDesktopEntryBytes = 1 << 20
)

// Environment variable names read by the XDG lister and the gio launcher.
const (
	envXDGDataHome    = "XDG_DATA_HOME"
	envXDGDataDirs    = "XDG_DATA_DIRS"
	envXDGDesktop     = "XDG_CURRENT_DESKTOP"
	envDisplay        = "DISPLAY"
	envWaylandDisplay = "WAYLAND_DISPLAY"

	defaultXDGDataDirs = "/usr/local/share:/usr/share"
	desktopSuffix      = ".desktop"
	desktopGroup       = "[Desktop Entry]"
	gioProgram         = "gio"
	gioLaunchVerb      = "launch"
)

// xdgLister lists visible freedesktop application entries. Its three inputs
// are replaceable so a test can point it at a temporary directory.
type xdgLister struct {
	getenv   func(string) string
	homeDir  func() (string, error)
	lookPath func(string) (string, error)
}

// newXDGLister returns a lister that reads the real environment.
func newXDGLister() *xdgLister {
	return &xdgLister{getenv: os.Getenv, homeDir: os.UserHomeDir, lookPath: exec.LookPath}
}

// List walks every XDG applications directory in priority order (a higher
// priority entry masks a lower one with the same desktop id even if it is
// hidden or broken), keeps the visible ones, and returns them sorted. Symlinks,
// oversized files and anything past the entry cap are skipped; unreadable
// entries are skipped rather than failing the whole list.
func (x *xdgLister) List(ctx context.Context) (string, []Listing, error) {
	seen := make(map[string]struct{})
	apps := make([]Listing, 0)
	visited := 0

	for _, dataDir := range x.dataDirs() {
		appsDir := filepath.Join(dataDir, "applications")
		_ = filepath.WalkDir(appsDir, func(path string, entry fs.DirEntry, walkErr error) error {
			if ctx.Err() != nil {
				return fs.SkipAll
			}
			if walkErr != nil || entry == nil || entry.IsDir() {
				return nil
			}
			if !strings.HasSuffix(strings.ToLower(entry.Name()), desktopSuffix) {
				return nil
			}
			visited++
			if visited > maxDesktopEntries {
				return fs.SkipAll
			}
			rel, err := filepath.Rel(appsDir, path)
			if err != nil {
				return nil
			}
			desktopID := strings.ReplaceAll(rel, string(os.PathSeparator), "-")
			if _, exists := seen[desktopID]; exists {
				return nil
			}
			seen[desktopID] = struct{}{}

			if entry.Type()&fs.ModeSymlink != 0 {
				return nil
			}
			info, err := entry.Info()
			if err != nil || !info.Mode().IsRegular() || info.Size() > maxDesktopEntryBytes {
				return nil
			}
			name, icon, fields, err := readDesktopEntry(path)
			if err != nil || !x.visible(fields) {
				return nil
			}
			name = displayName(name)
			if name == "" {
				return nil
			}
			launchable := x.launchable(fields)
			app := Listing{App: App{
				ID:         makeID(linuxIDDomain, desktopID),
				Name:       name,
				Icon:       iconName(icon),
				Category:   desktopCategory(fields["Categories"]),
				Launchable: launchable,
			}}
			if launchable {
				app.Ref = path
			}
			apps = append(apps, app)
			return nil
		})
		if visited > maxDesktopEntries {
			break
		}
	}
	if err := ctx.Err(); err != nil {
		return PlatformLinux, nil, err
	}
	sortApps(apps)
	return PlatformLinux, apps, nil
}

// dataDirs returns the XDG data directories, user directory first: XDG_DATA_HOME
// (or ~/.local/share), then each absolute XDG_DATA_DIRS entry.
func (x *xdgLister) dataDirs() []string {
	var dirs []string
	dataHome := x.getenv(envXDGDataHome)
	if !filepath.IsAbs(dataHome) {
		dataHome = ""
		if home, err := x.homeDir(); err == nil && home != "" {
			dataHome = filepath.Join(home, ".local", "share")
		}
	}
	if dataHome != "" {
		dirs = append(dirs, filepath.Clean(dataHome))
	}
	dataDirs := x.getenv(envXDGDataDirs)
	if dataDirs == "" {
		dataDirs = defaultXDGDataDirs
	}
	for _, dir := range filepath.SplitList(dataDirs) {
		if filepath.IsAbs(dir) {
			dirs = append(dirs, filepath.Clean(dir))
		}
	}
	return dirs
}

// readDesktopEntry parses the [Desktop Entry] group of a .desktop file (at most
// maxDesktopEntryBytes) into its key/value fields and returns Name and Icon. A
// file whose Type is not Application is rejected.
func readDesktopEntry(path string) (name, icon string, fields map[string]string, err error) {
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
	fields = make(map[string]string)
	inGroup := false
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSuffix(line, "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == desktopGroup {
			inGroup = true
			continue
		}
		if strings.HasPrefix(trimmed, "[") {
			inGroup = false
			continue
		}
		if !inGroup || trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		if key = strings.TrimSpace(key); key != "" {
			fields[key] = unescapeDesktopString(strings.TrimSpace(value))
		}
	}
	if fields["Type"] != "Application" {
		return "", "", fields, os.ErrInvalid
	}
	return fields["Name"], fields["Icon"], fields, nil
}

// unescapeDesktopString decodes the freedesktop escapes \s \n \t \r \\ ; any
// other escaped character stands for itself.
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
		default:
			b.WriteByte(value[i])
		}
	}
	return b.String()
}

// visible applies Hidden, NoDisplay, OnlyShowIn and NotShowIn against the
// current desktop (XDG_CURRENT_DESKTOP).
func (x *xdgLister) visible(fields map[string]string) bool {
	if strings.EqualFold(fields["Hidden"], "true") || strings.EqualFold(fields["NoDisplay"], "true") {
		return false
	}
	desktops := splitList(x.getenv(envXDGDesktop), ":")
	if only := splitList(fields["OnlyShowIn"], ";"); len(only) > 0 && len(desktops) > 0 && !intersects(only, desktops) {
		return false
	}
	if excluded := splitList(fields["NotShowIn"], ";"); len(excluded) > 0 && intersects(excluded, desktops) {
		return false
	}
	return true
}

// launchable reports whether the entry has something to run (Exec or
// DBusActivatable) and, when it names a TryExec, that program is installed.
func (x *xdgLister) launchable(fields map[string]string) bool {
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
	_, err := x.lookPath(tryExec)
	return err == nil
}

// splitList splits value on sep and drops empty items.
func splitList(value, sep string) []string {
	var out []string
	for _, item := range strings.Split(value, sep) {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}

// intersects reports whether a and b share an item.
func intersects(a, b []string) bool {
	for _, l := range a {
		for _, r := range b {
			if l == r {
				return true
			}
		}
	}
	return false
}

// desktopCategories maps freedesktop main categories to the short labels the
// phone shows, in priority order (Development and Utility shows Development).
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

// desktopCategory returns the label of the first matching main category in a
// Categories= value, or "".
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

// gioLauncher starts a .desktop entry with "gio launch <path>". It needs a
// graphical session (DISPLAY or WAYLAND_DISPLAY) and the gio program.
type gioLauncher struct {
	getenv   func(string) string
	lookPath func(string) (string, error)
	run      runFunc
}

// newGioLauncher returns a launcher that uses the real environment and gio.
func newGioLauncher() *gioLauncher {
	return &gioLauncher{getenv: os.Getenv, lookPath: exec.LookPath, run: execRun}
}

// Launch runs gio with the fixed arguments [launch, <desktop file path>]; no
// shell is involved and the path comes from the service's own fresh listing.
func (g *gioLauncher) Launch(ctx context.Context, app Listing) error {
	if !app.Launchable {
		return ErrNotLaunchable
	}
	if g.getenv(envDisplay) == "" && g.getenv(envWaylandDisplay) == "" {
		return ErrNoDesktopSession
	}
	gio, err := g.lookPath(gioProgram)
	if err != nil {
		return ErrProviderMissing
	}
	if !filepath.IsAbs(app.Ref) {
		return ErrGone
	}
	if err := g.run(ctx, gio, gioLaunchVerb, app.Ref); err != nil {
		// gio can accept a launch and the app fail later; only the handoff is reported.
		return ErrStartFailed
	}
	return nil
}
