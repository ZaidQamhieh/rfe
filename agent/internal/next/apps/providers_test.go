package apps

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// writeFile creates dir/rel with content, making parent directories.
func writeFile(t *testing.T, dir, rel, content string) string {
	t.Helper()
	path := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// entry builds a minimal valid desktop entry with extra lines appended.
func entry(name string, extra ...string) string {
	return "[Desktop Entry]\nType=Application\nName=" + name + "\n" + strings.Join(extra, "\n") + "\n"
}

// testXDG returns a lister whose user and system data dirs are temp dirs and
// whose environment is a fixed map.
func testXDG(t *testing.T, env map[string]string, installed ...string) (*xdgLister, string, string) {
	t.Helper()
	user, system := t.TempDir(), t.TempDir()
	env[envXDGDataHome] = user
	env[envXDGDataDirs] = system
	have := map[string]bool{}
	for _, n := range installed {
		have[n] = true
	}
	return &xdgLister{
		getenv:  func(k string) string { return env[k] },
		homeDir: func() (string, error) { return "", errors.New("no home") },
		lookPath: func(n string) (string, error) {
			if have[n] {
				return "/fake/bin/" + n, nil
			}
			return "", errors.New("not found")
		},
	}, user, system
}

func TestXDGListsVisibleEntriesWithStableIDs(t *testing.T) {
	x, user, system := testXDG(t, map[string]string{}, "tool")
	writeFile(t, user, "applications/firefox.desktop", entry("Firefox", "Exec=firefox %u", "Icon=firefox", "Categories=Network;WebBrowser;"))
	writeFile(t, system, "applications/kde/foo.desktop", entry("Zed Foo", "Exec=foo", "Icon=../evil", "Categories=Utility;Development;"))
	writeFile(t, system, "applications/hidden.desktop", entry("Hidden One", "Exec=x", "NoDisplay=true"))
	writeFile(t, system, "applications/noexec.desktop", entry("No Exec"))
	writeFile(t, system, "applications/needs-missing.desktop", entry("Needs Missing", "Exec=x", "TryExec=missing"))
	writeFile(t, system, "applications/needs-tool.desktop", entry("Needs Tool", "Exec=x", "TryExec=tool"))
	writeFile(t, system, "applications/link.txt", "not an entry")
	writeFile(t, system, "applications/notapp.desktop", "[Desktop Entry]\nType=Link\nName=Link\n")

	platform, apps, err := x.List(context.Background())
	if err != nil || platform != PlatformLinux {
		t.Fatalf("platform/err = %s/%v", platform, err)
	}
	var names []string
	byName := map[string]Listing{}
	for _, a := range apps {
		names = append(names, a.Name)
		byName[a.Name] = a
	}
	want := []string{"Firefox", "Needs Missing", "Needs Tool", "No Exec", "Zed Foo"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("names = %v, want %v", names, want)
	}
	ff := byName["Firefox"]
	// Golden: sha256("rfe-host-app-v1\x00firefox.desktop"), the old agent's id.
	if ff.ID != "app_4a519e70dc678330b31339e47422d8ae5d0e1f8e529d314c93def0b264cf8540" {
		t.Fatalf("firefox id = %s", ff.ID)
	}
	if !ff.Launchable || ff.Icon != "firefox" || ff.Category != "Internet" || ff.Ref != filepath.Join(user, "applications/firefox.desktop") {
		t.Fatalf("firefox = %+v", ff)
	}
	foo := byName["Zed Foo"]
	if foo.ID != "app_cbd87b06d7bc572e2a755f8f96d36076db60baf0085a8b71e287e384e45dff2f" || foo.Icon != "" || foo.Category != "Development" {
		t.Fatalf("subdir entry = %+v", foo)
	}
	if byName["No Exec"].Launchable || byName["No Exec"].Ref != "" {
		t.Fatalf("an entry with no Exec must be listed but not launchable: %+v", byName["No Exec"])
	}
	if byName["Needs Missing"].Launchable || !byName["Needs Tool"].Launchable {
		t.Fatal("TryExec must decide launchable")
	}
}

func TestXDGHigherPriorityEntryMasksLowerOneEvenWhenHidden(t *testing.T) {
	x, user, system := testXDG(t, map[string]string{})
	writeFile(t, user, "applications/a.desktop", entry("A", "Exec=a", "Hidden=true"))
	writeFile(t, system, "applications/a.desktop", entry("A system", "Exec=a"))
	writeFile(t, system, "applications/b.desktop", entry("B", "Exec=b"))
	_, apps, err := x.List(context.Background())
	if err != nil || len(apps) != 1 || apps[0].Name != "B" {
		t.Fatalf("apps = %+v, err = %v", apps, err)
	}
}

func TestXDGSkipsSymlinksAndOversizedEntries(t *testing.T) {
	x, user, system := testXDG(t, map[string]string{})
	target := writeFile(t, t.TempDir(), "real.desktop", entry("Linked", "Exec=l"))
	if err := os.MkdirAll(filepath.Join(system, "applications"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(system, "applications", "linked.desktop")); err != nil {
		t.Skip("symlinks unavailable: ", err)
	}
	writeFile(t, user, "applications/big.desktop", entry("Big", "Exec=b", "Comment="+strings.Repeat("x", maxDesktopEntryBytes)))
	writeFile(t, user, "applications/ok.desktop", entry("Fine", "Exec=ok"))
	_, apps, err := x.List(context.Background())
	if err != nil || len(apps) != 1 || apps[0].Name != "Fine" {
		t.Fatalf("apps = %+v, err = %v", apps, err)
	}
}

func TestXDGDesktopVisibility(t *testing.T) {
	x, _, system := testXDG(t, map[string]string{envXDGDesktop: "GNOME"})
	writeFile(t, system, "applications/only-kde.desktop", entry("Only KDE", "Exec=x", "OnlyShowIn=KDE;"))
	writeFile(t, system, "applications/not-gnome.desktop", entry("Not GNOME", "Exec=x", "NotShowIn=GNOME;"))
	writeFile(t, system, "applications/gnome.desktop", entry("GNOME one", "Exec=x", "OnlyShowIn=GNOME;"))
	_, apps, _ := x.List(context.Background())
	if len(apps) != 1 || apps[0].Name != "GNOME one" {
		t.Fatalf("apps = %+v", apps)
	}
}

func TestXDGListHonoursCancellationAndNoDirs(t *testing.T) {
	x, _, _ := testXDG(t, map[string]string{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := x.List(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	emptyDir := t.TempDir()
	empty := &xdgLister{getenv: func(k string) string {
		if k == envXDGDataDirs {
			return emptyDir
		}
		return ""
	}, homeDir: func() (string, error) { return "", errors.New("x") }, lookPath: func(string) (string, error) { return "", errors.New("x") }}
	if _, apps, err := empty.List(context.Background()); err != nil || len(apps) != 0 {
		t.Fatalf("apps = %v err = %v", apps, err)
	}
}

func TestUnescapeDesktopString(t *testing.T) {
	if got := unescapeDesktopString(`a\sb\\c\nd\q\`); got != "a b\\c\ndq\\" {
		t.Fatalf("got %q", got)
	}
}

// fakeRun records the programs a launcher runs.
type fakeRun struct {
	calls [][]string
	err   error
}

func (f *fakeRun) run(_ context.Context, name string, args ...string) error {
	f.calls = append(f.calls, append([]string{name}, args...))
	return f.err
}

func testGio(env map[string]string, lookErr error, run *fakeRun) *gioLauncher {
	return &gioLauncher{
		getenv:   func(k string) string { return env[k] },
		lookPath: func(n string) (string, error) { return "/fake/bin/" + n, lookErr },
		run:      run.run,
	}
}

func TestGioLauncherRunsFixedArgumentsWithoutAShell(t *testing.T) {
	run := &fakeRun{}
	g := testGio(map[string]string{envWaylandDisplay: "wayland-0"}, nil, run)
	app := Listing{App: App{ID: idLaunchable, Launchable: true}, Ref: "/usr/share/applications/a b;c.desktop"}
	if err := g.Launch(context.Background(), app); err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"/fake/bin/gio", "launch", "/usr/share/applications/a b;c.desktop"}}
	if !reflect.DeepEqual(run.calls, want) {
		t.Fatalf("calls = %v, want %v", run.calls, want)
	}
}

func TestGioLauncherRefusals(t *testing.T) {
	ok := Listing{App: App{Launchable: true}, Ref: "/usr/share/applications/a.desktop"}
	cases := []struct {
		name string
		env  map[string]string
		look error
		app  Listing
		runE error
		want error
	}{
		{"no display", map[string]string{}, nil, ok, nil, ErrNoDesktopSession},
		{"no gio", map[string]string{envDisplay: ":0"}, errors.New("missing"), ok, nil, ErrProviderMissing},
		{"relative ref", map[string]string{envDisplay: ":0"}, nil, Listing{App: App{Launchable: true}, Ref: "-a/evil"}, nil, ErrGone},
		{"empty ref", map[string]string{envDisplay: ":0"}, nil, Listing{App: App{Launchable: true}}, nil, ErrGone},
		{"not launchable", map[string]string{envDisplay: ":0"}, nil, Listing{Ref: ok.Ref}, nil, ErrNotLaunchable},
		{"gio fails", map[string]string{envDisplay: ":0"}, nil, ok, errors.New("exit 1"), ErrStartFailed},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			run := &fakeRun{err: c.runE}
			err := testGio(c.env, c.look, run).Launch(context.Background(), c.app)
			if !errors.Is(err, c.want) {
				t.Fatalf("err = %v, want %v", err, c.want)
			}
			if c.runE == nil && len(run.calls) != 0 {
				t.Fatalf("a refused launch ran %v", run.calls)
			}
		})
	}
}

func TestParseWindowsInventory(t *testing.T) {
	const calc = "Microsoft.WindowsCalculator_8wekyb3d8bbwe!App"
	in := `{"apps":[
		{"name":"Zip","aumid":"Vendor.Zip_abc!App"},
		{"name":"Calculator","aumid":"` + calc + `"},
		{"name":"Calculator again","aumid":"` + calc + `"},
		{"name":"Evil","aumid":"x;calc.exe"},
		{"name":"Evil2","aumid":" Vendor.Space!App"},
		{"name":"Evil3","aumid":"-Embedding"},
		{"name":"Evil4","aumid":"C:\\Windows\\System32\\cmd.exe"},
		{"name":"  ","aumid":"Vendor.Blank!App"}
	]}`
	apps, err := parseWindowsInventory([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(apps) != 2 || apps[0].Name != "Calculator" || apps[1].Name != "Zip" {
		t.Fatalf("apps = %+v", apps)
	}
	if apps[0].ID != "app_159ebffbb269614fc0895dba58dc10330ba16ec8c19ae4622a348db67199eddb" || apps[0].Ref != calc || !apps[0].Launchable {
		t.Fatalf("calculator = %+v", apps[0])
	}
}

func TestParseWindowsInventoryBadOutput(t *testing.T) {
	if apps, err := parseWindowsInventory([]byte("  \n")); err != nil || len(apps) != 0 {
		t.Fatalf("empty output: %v %v", apps, err)
	}
	if _, err := parseWindowsInventory([]byte("{not json")); !errors.Is(err, ErrProviderMissing) {
		t.Fatalf("err = %v", err)
	}
	if _, err := parseWindowsInventory(make([]byte, maxWindowsOutputBytes+1)); !errors.Is(err, ErrProviderMissing) {
		t.Fatalf("oversized output err = %v", err)
	}
	if !strings.Contains(appsFolderInventoryScript, "5000") || strings.Contains(appsFolderInventoryScript, "%") {
		t.Fatal("script caps must come from the Go constants")
	}
}

func TestMacListing(t *testing.T) {
	meta := macBundleMetadata{BundleID: "com.Example.App", Display: "Example", Package: "APPL", Icon: "AppIcon.icns"}
	app, identity, ok := macListing(meta, "/Applications/Example.app", "applications", "Example.app")
	if !ok || identity != "bundle:com.example.app" || app.Name != "Example" || app.Icon != "appicon" || app.Ref != "/Applications/Example.app" {
		t.Fatalf("app = %+v identity = %q ok = %v", app, identity, ok)
	}
	if app.ID != "app_e3d040b96395fd01a6f45a5795953d930d54081a41df2435bce46b85a006404b" {
		t.Fatalf("id = %s", app.ID)
	}
	// A bundle with a bad id falls back to its relative path, and the same
	// fallback is refused for a single-bundle root (no relative path).
	meta.BundleID = "bad id!"
	if _, identity, ok := macListing(meta, "/A/Example.app", "applications", "Sub/Example.app"); !ok || identity != "path:applications:Sub/Example.app" {
		t.Fatalf("identity = %q ok = %v", identity, ok)
	}
	if _, _, ok := macListing(meta, "/A/Example.app", "finder", ""); ok {
		t.Fatal("a bundle with no usable identity must be rejected")
	}
	meta.Package = "FMWK"
	if _, _, ok := macListing(meta, "/A/Example.app", "applications", "Example.app"); ok {
		t.Fatal("only APPL bundles are apps")
	}
	meta = macBundleMetadata{Package: "APPL", BundleID: "a.b"}
	if app, _, ok := macListing(meta, "/A/Fallback Name.app", "applications", "Fallback Name.app"); !ok || app.Name != "Fallback Name" {
		t.Fatalf("name fallback = %+v", app)
	}
}

func TestMacIconName(t *testing.T) {
	for in, want := range map[string]string{
		"AppIcon.icns": "appicon", "Icon": "icon", "": "", "../x": "", `a\b`: "", ".hidden": "",
		strings.Repeat("a", maxIconNameLen+1): "",
	} {
		if got := macIconName(in); got != want {
			t.Fatalf("macIconName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDisplayName(t *testing.T) {
	if got := displayName("  a/b\\c\x00\x1b d\u00e9 "); got != "a b c d\u00e9" {
		t.Fatalf("got %q", got)
	}
	if got := displayName(strings.Repeat("x", MaxDisplayBytes*2)); len(got) != MaxDisplayBytes {
		t.Fatalf("len = %d", len(got))
	}
}

func TestNewTakesPlatformProvidersWhenNoneGiven(t *testing.T) {
	s := New(Config{})
	if s.cfg.Lister == nil || s.cfg.Launcher == nil {
		t.Fatal("platform providers were not installed")
	}
}
