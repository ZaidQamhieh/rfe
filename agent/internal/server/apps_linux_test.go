//go:build linux

package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ZaidQamhieh/rfe/agent/internal/store"
)

func setAppCatalogDirs(t *testing.T, home string) string {
	t.Helper()
	dataHome := filepath.Join(home, "user-data")
	systemData := filepath.Join(home, "system-data")
	if err := os.MkdirAll(filepath.Join(dataHome, "applications"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(systemData, "applications"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_DATA_HOME", dataHome)
	t.Setenv("XDG_DATA_DIRS", systemData)
	return dataHome
}

func writeDesktopEntry(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestXDGApplicationDirsUsesAbsoluteConfiguredPaths(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", "relative")
	configured := filepath.Join(home, "custom-share")
	t.Setenv("XDG_DATA_DIRS", "relative"+string(os.PathListSeparator)+configured)
	want := []string{filepath.Join(home, ".local", "share"), configured}
	if got := xdgApplicationDirs(); !reflect.DeepEqual(got, want) {
		t.Fatalf("xdgApplicationDirs() = %#v, want %#v", got, want)
	}
}

func TestDesktopEntryParsingVisibilityAndLaunchability(t *testing.T) {
	root := t.TempDir()
	entry := writeDesktopEntry(t, root, "demo.desktop", "# comment\n[Desktop Entry]\r\nType=Application\r\nName=Demo\\sApp\r\nIcon=demo\r\nExec=/bin/true\r\nOnlyShowIn=GNOME;KDE;\r\n")
	name, icon, fields, err := readDesktopApplication(entry)
	if err != nil || name != "Demo App" || icon != "demo" || fields["Exec"] != "/bin/true" {
		t.Fatalf("readDesktopApplication = (%q,%q,%v,%v)", name, icon, fields, err)
	}
	t.Setenv("XDG_CURRENT_DESKTOP", "GNOME:XFCE")
	if !desktopEntryVisible(fields) || !desktopEntryLaunchable(fields) {
		t.Fatal("matching desktop entry should be visible and launchable")
	}
	if desktopEntryVisible(map[string]string{"Hidden": "TRUE"}) || desktopEntryVisible(map[string]string{"NoDisplay": "true"}) {
		t.Fatal("Hidden and NoDisplay entries must be excluded")
	}
	if desktopEntryVisible(map[string]string{"OnlyShowIn": "KDE;"}) {
		t.Fatal("OnlyShowIn mismatch should hide the entry")
	}
	if desktopEntryVisible(map[string]string{"NotShowIn": "XFCE;"}) {
		t.Fatal("NotShowIn match should hide the entry")
	}
	if !desktopEntryVisible(map[string]string{"NotShowIn": "MATE;"}) {
		t.Fatal("NotShowIn mismatch should preserve visibility")
	}
	if desktopEntryLaunchable(map[string]string{}) || desktopEntryLaunchable(map[string]string{"TryExec": "missing-rfe-test-executable-xyz"}) {
		t.Fatal("missing Exec/TryExec registrations must not be launchable")
	}
	if desktopEntryLaunchable(map[string]string{"Exec": "/bin/true", "TryExec": "two words"}) {
		t.Fatal("TryExec containing arguments must be rejected")
	}
	if !desktopEntryLaunchable(map[string]string{"DBusActivatable": "true"}) {
		t.Fatal("DBusActivatable application should be launchable without Exec")
	}
	if got := desktopList(" one ;; two ; ", ";"); !reflect.DeepEqual(got, []string{"one", "two"}) {
		t.Fatalf("desktopList = %#v", got)
	}
	if !intersects([]string{"GNOME", "KDE"}, []string{"XFCE", "KDE"}) || intersects([]string{"GNOME"}, []string{"XFCE"}) {
		t.Fatal("intersects did not distinguish overlapping and disjoint desktop lists")
	}
	if got := unescapeDesktopString(`a\sb\nc\td\re\\f\q\`); got != "a b\nc\td\re\\fq\\" {
		t.Fatalf("unescapeDesktopString = %q", got)
	}
	if got := desktopAppID("nested-demo.desktop"); !strings.HasPrefix(got, "app_") || len(got) != len("app_")+64 {
		t.Fatalf("unexpected opaque app ID: %q", got)
	}

	badType := writeDesktopEntry(t, root, "bad.desktop", "[Desktop Entry]\nType=Link\nName=Bad\n")
	if _, _, _, err := readDesktopApplication(badType); !errors.Is(err, os.ErrInvalid) {
		t.Fatalf("non-application type error = %v, want os.ErrInvalid", err)
	}
	tooLarge := writeDesktopEntry(t, root, "large.desktop", "[Desktop Entry]\nType=Application\nName="+strings.Repeat("x", maxDesktopEntryBytes+1))
	if _, _, _, err := readDesktopApplication(tooLarge); !errors.Is(err, os.ErrInvalid) {
		t.Fatalf("oversized desktop file error = %v, want os.ErrInvalid", err)
	}
}

func TestListHostAppsMasksDuplicatesAndFiltersUnsafeEntries(t *testing.T) {
	dataHome := setAppCatalogDirs(t, t.TempDir())
	userApps := filepath.Join(dataHome, "applications")
	systemApps := filepath.Join(filepath.Dir(dataHome), "system-data", "applications")

	// A hidden user entry masks a same-ID system entry, preventing a lower
	// priority registration from unexpectedly appearing or being launched.
	writeDesktopEntry(t, userApps, "hidden.desktop", "[Desktop Entry]\nType=Application\nName=Hidden\nHidden=true\nExec=/bin/true\n")
	writeDesktopEntry(t, systemApps, "hidden.desktop", "[Desktop Entry]\nType=Application\nName=Lower Priority\nExec=/bin/true\n")
	writeDesktopEntry(t, userApps, "no-exec.desktop", "[Desktop Entry]\nType=Application\nName=Not Launchable\n")
	writeDesktopEntry(t, userApps, "unsafe-icon.desktop", "[Desktop Entry]\nType=Application\nName=  Zeta/ App  \nIcon=bad/icon\nExec=/bin/true\n")
	writeDesktopEntry(t, userApps, "alpha.desktop", "[Desktop Entry]\nType=Application\nName=alpha\nIcon=good-icon_1\nExec=/bin/true\n")
	if err := os.Symlink("/does/not/matter.desktop", filepath.Join(userApps, "tombstone.desktop")); err != nil {
		t.Fatal(err)
	}
	writeDesktopEntry(t, systemApps, "tombstone.desktop", "[Desktop Entry]\nType=Application\nName=Must Stay Masked\nExec=/bin/true\n")

	platform, apps, err := listHostApps()
	if err != nil {
		t.Fatalf("listHostApps: %v", err)
	}
	if platform != "linux" || len(apps) != 3 {
		t.Fatalf("catalog = platform %q, %d apps (%+v)", platform, len(apps), apps)
	}
	if apps[0].Name != "alpha" || apps[1].Name != "Not Launchable" || apps[2].Name != "Zeta  App" {
		t.Fatalf("catalog order or display sanitization incorrect: %+v", apps)
	}
	if apps[0].Icon != "good-icon_1" || apps[2].Icon != "" {
		t.Fatalf("unsafe icons were not filtered: %+v", apps)
	}
	if apps[0].launchRef == "" || apps[0].ID != desktopAppID("alpha.desktop") || !apps[0].Launchable {
		t.Fatalf("catalog record lacks local registration reference or stable ID: %+v", apps[0])
	}
	if apps[1].Launchable || apps[1].launchRef != "" || apps[1].ID != desktopAppID("no-exec.desktop") {
		t.Fatalf("non-launchable registration should be listed without a launch reference: %+v", apps[1])
	}
}

func TestLaunchHostAppRevalidatesCatalogAndUsesNativeLauncher(t *testing.T) {
	dataHome := setAppCatalogDirs(t, t.TempDir())
	entry := writeDesktopEntry(t, filepath.Join(dataHome, "applications"), "safe.desktop", "[Desktop Entry]\nType=Application\nName=Safe App\nExec=/bin/true\n")
	requested := appRecord{ID: desktopAppID("safe.desktop"), Launchable: true, launchRef: "/client/controlled/path"}
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	viewDevice := &store.Device{ID: "catalog-view-integration", ViewApps: true}
	listReq := httptest.NewRequest(http.MethodGet, "/v1/apps", nil)
	listReq = listReq.WithContext(withDevice(listReq.Context(), viewDevice))
	listRR := httptest.NewRecorder()
	listAppsHandler()(listRR, listReq)
	if listRR.Code != http.StatusOK {
		t.Fatalf("list app catalog status = %d: %s", listRR.Code, listRR.Body.String())
	}
	var catalog appCatalogResponse
	if err := json.Unmarshal(listRR.Body.Bytes(), &catalog); err != nil {
		t.Fatal(err)
	}
	if catalog.Platform != "linux" || catalog.LaunchAllowed || len(catalog.Apps) != 1 || catalog.Apps[0].Name != "Safe App" || !catalog.Apps[0].Launchable {
		t.Fatalf("app catalog response = %+v", catalog)
	}
	launchDevice := &store.Device{ID: "app-launch-integration", ViewApps: true, LaunchApps: true}
	t.Setenv("DISPLAY", "")
	t.Setenv("WAYLAND_DISPLAY", "")
	noSessionReq := appRequestWithID(http.MethodPost, requested.ID)
	noSessionReq = noSessionReq.WithContext(withDevice(noSessionReq.Context(), launchDevice))
	noSessionRR := httptest.NewRecorder()
	launchAppHandler(db)(noSessionRR, noSessionReq)
	if noSessionRR.Code != http.StatusServiceUnavailable || !strings.Contains(noSessionRR.Body.String(), "NO_INTERACTIVE_SESSION") {
		t.Fatalf("without a graphical session, launch response = %d: %s", noSessionRR.Code, noSessionRR.Body.String())
	}

	bin := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(t.TempDir(), "launcher-args")
	gio := filepath.Join(bin, "gio")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$RFE_TEST_LAUNCH_LOG\"\n"
	if err := os.WriteFile(gio, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	t.Setenv("RFE_TEST_LAUNCH_LOG", logPath)
	t.Setenv("DISPLAY", ":rfe-test")
	launchReq := appRequestWithID(http.MethodPost, requested.ID)
	launchReq = launchReq.WithContext(withDevice(launchReq.Context(), launchDevice))
	launchRR := httptest.NewRecorder()
	launchAppHandler(db)(launchRR, launchReq)
	if launchRR.Code != http.StatusAccepted || !strings.Contains(launchRR.Body.String(), "started") {
		t.Fatalf("registered app launch response = %d: %s", launchRR.Code, launchRR.Body.String())
	}
	args, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read native launcher arguments: %v", err)
	}
	if got, want := string(args), "launch\n"+entry+"\n"; got != want {
		t.Fatalf("native launcher args = %q, want %q", got, want)
	}
	missingReq := appRequestWithID(http.MethodPost, "app_"+strings.Repeat("0", 64))
	missingReq = missingReq.WithContext(withDevice(missingReq.Context(), launchDevice))
	missingRR := httptest.NewRecorder()
	launchAppHandler(db)(missingRR, missingReq)
	if missingRR.Code != http.StatusNotFound || !strings.Contains(missingRR.Body.String(), "APP_NOT_FOUND") {
		t.Fatalf("removed catalog entry response = %d: %s", missingRR.Code, missingRR.Body.String())
	}
	entries, err := db.AuditEntries(10, 0)
	if err != nil || len(entries) != 3 {
		t.Fatalf("app launch audit = (%+v, %v), want three events", entries, err)
	}
	if !strings.Contains(entries[0].Detail, "outcome=not_found") || !strings.Contains(entries[1].Detail, "outcome=started") || !strings.Contains(entries[2].Detail, "outcome=no_interactive_session") {
		t.Fatalf("app launch audit outcomes incorrect: %+v", entries)
	}
}

func TestLaunchAppHandlerRejectsDiscoveredNonLaunchableEntry(t *testing.T) {
	dataHome := setAppCatalogDirs(t, t.TempDir())
	writeDesktopEntry(t, filepath.Join(dataHome, "applications"), "unavailable.desktop", "[Desktop Entry]\nType=Application\nName=Unavailable App\n")
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	device := &store.Device{ID: "non-launchable-app-client", ViewApps: true, LaunchApps: true}
	req := appRequestWithID(http.MethodPost, desktopAppID("unavailable.desktop"))
	req = req.WithContext(withDevice(req.Context(), device))
	rr := httptest.NewRecorder()
	launchAppHandler(db)(rr, req)
	if rr.Code != http.StatusConflict || !strings.Contains(rr.Body.String(), "APP_NOT_LAUNCHABLE") {
		t.Fatalf("non-launchable app response = %d: %s", rr.Code, rr.Body.String())
	}
	entries, err := db.AuditEntries(10, 0)
	if err != nil || len(entries) != 1 || !strings.Contains(entries[0].Detail, "outcome=not_launchable") {
		t.Fatalf("non-launchable audit = (%+v, %v)", entries, err)
	}
}

func TestDesktopCategory(t *testing.T) {
	cases := map[string]string{
		"Development;IDE;":                "Development",
		"Utility;TextEditor;Development;": "Development",
		"Network;WebBrowser;":             "Internet",
		"AudioVideo;Player;":              "Media",
		"System;TerminalEmulator;":        "System",
		"X-Custom;":                       "",
		"":                                "",
	}
	for in, want := range cases {
		if got := desktopCategory(in); got != want {
			t.Errorf("desktopCategory(%q) = %q, want %q", in, got, want)
		}
	}
}
