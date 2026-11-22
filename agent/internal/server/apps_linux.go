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
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// x11SocketDir is where X servers create their listening sockets (X0, X1, ...).
// It is a variable only so tests can point it at a temporary directory.
var x11SocketDir = "/tmp/.X11-unix"

// launchHandoffWait is how long a launch waits for the launcher process to
// report a failure before the request is answered as started. gio normally
// exits within milliseconds after handing the app to the session.
var launchHandoffWait = 3 * time.Second

const maxDesktopEntries = 10000
const maxDesktopEntryBytes = 1 << 20

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
	// A service started by systemd --user usually has neither variable even
	// though the user is logged in; find the session from its sockets instead.
	var sessionEnv []string
	if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
		x11Dir := x11SocketDir
		if v := os.Getenv("RFE_TEST_X11_SOCKET_DIR"); v != "" { // tests only: keep a real X server out of the lookup
			x11Dir = v
		}
		sessionEnv = discoverDesktopSession(os.Getenv, os.Getuid(), x11Dir)
		if len(sessionEnv) == 0 {
			return errNoDesktopSession
		}
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

	// GIO can confirm that it accepted a launch even if the app later fails
	// during startup; the API reports only this handoff result.
	return startDetached(gio, []string{"launch", current.launchRef}, append(os.Environ(), sessionEnv...))
}

// startDetached runs a launcher as its own session leader with stdin, stdout
// and stderr on /dev/null, so no pipe is shared with the agent and a GUI app
// the launcher spawns cannot keep the request waiting. It answers nil when the
// launcher exits 0, or is still running after launchHandoffWait; a failed
// start or a non-zero exit within that time is errAppStartFailed. The process
// is always reaped in a background goroutine and never killed by the request.
// The wait deliberately ignores request cancellation: a client that drops
// mid-wait does not stop the launcher, so the answer must still be the real
// handoff result (a cancelled wait would report failure for a launch that
// goes on to succeed, and a retry would start a second instance).
func startDetached(path string, args, env []string) error {
	cmd := exec.Command(path, args...)
	cmd.Env = env
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	// Nil Stdin/Stdout/Stderr make os/exec open os.DevNull for the child.
	if err := cmd.Start(); err != nil {
		return errAppStartFailed
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	timer := time.NewTimer(launchHandoffWait)
	defer timer.Stop()
	select {
	case err := <-exited:
		if err != nil {
			return errAppStartFailed
		}
		return nil
	case <-timer.C:
		return nil
	}
}

// discoverDesktopSession finds a running graphical session for the current
// user when the agent's own environment has none. It returns the extra
// KEY=VALUE entries to give a launched app: a wayland-N socket in the user's
// runtime directory (WAYLAND_DISPLAY plus XDG_RUNTIME_DIR) and/or an X server
// socket XN owned by uid in x11Dir (DISPLAY=:N); the lowest number of each
// kind wins. It returns nil when the environment already names a session or
// none is found.
func discoverDesktopSession(getenv func(string) string, uid int, x11Dir string) []string {
	if getenv("DISPLAY") != "" || getenv("WAYLAND_DISPLAY") != "" {
		return nil
	}
	runtimeDir := getenv("XDG_RUNTIME_DIR")
	if !filepath.IsAbs(runtimeDir) {
		runtimeDir = filepath.Join("/run/user", strconv.Itoa(uid))
	}
	var extra []string
	if name := lowestSocket(runtimeDir, uid, "wayland-"); name != "" {
		extra = append(extra, "WAYLAND_DISPLAY="+name, "XDG_RUNTIME_DIR="+runtimeDir)
	}
	if name := lowestSocket(x11Dir, uid, "X"); name != "" {
		extra = append(extra, "DISPLAY=:"+strings.TrimPrefix(name, "X"))
	}
	return extra
}

// lowestSocket returns the name of the unix socket in dir owned by uid whose
// name is prefix followed only by a decimal number (so "wayland-0.lock" and
// non-sockets are skipped), choosing the smallest number; "" when none.
func lowestSocket(dir string, uid int, prefix string) string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	best, bestN := "", -1
	for _, entry := range entries {
		name := entry.Name()
		n, err := strconv.Atoi(strings.TrimPrefix(name, prefix))
		if !strings.HasPrefix(name, prefix) || err != nil || n < 0 {
			continue
		}
		info, err := entry.Info()
		if err != nil || info.Mode().Type() != fs.ModeSocket {
			continue
		}
		if st, ok := info.Sys().(*syscall.Stat_t); !ok || int(st.Uid) != uid {
			continue
		}
		if bestN < 0 || n < bestN {
			best, bestN = name, n
		}
	}
	return best
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
