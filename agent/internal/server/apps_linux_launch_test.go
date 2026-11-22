package server

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/store"
)

// makeUnixSocketFile leaves a unix socket inode at path (listener closed, file kept).
func makeUnixSocketFile(t *testing.T, path string) {
	t.Helper()
	l, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	l.(*net.UnixListener).SetUnlinkOnClose(false)
	_ = l.Close()
}

// fakeGio installs a fake "gio" first on PATH that runs the given shell body.
func fakeGio(t *testing.T, body string) {
	t.Helper()
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "gio"), []byte("#!/bin/sh\n"+body), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+"/usr/bin"+string(os.PathListSeparator)+"/bin")
}

// launchFixture prepares one launchable catalog entry and returns its record.
func launchFixture(t *testing.T) appRecord {
	t.Helper()
	dataHome := setAppCatalogDirs(t, t.TempDir())
	writeDesktopEntry(t, filepath.Join(dataHome, "applications"), "safe.desktop", "[Desktop Entry]\nType=Application\nName=Safe App\nExec=/bin/true\n")
	return appRecord{ID: desktopAppID("safe.desktop"), Launchable: true}
}

// killPidFile kills the process whose pid the fake launcher wrote to path.
func killPidFile(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	if pid, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil && pid > 1 {
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}
}

func TestLaunchAppHandlerDoesNotWaitForLauncherDescendants(t *testing.T) {
	rec := launchFixture(t)
	out := filepath.Join(t.TempDir(), "out")
	t.Setenv("DISPLAY", ":rfe-test")
	t.Setenv("RFE_TEST_OUT", out)
	// The launcher hands off to a long-lived GUI process that keeps running
	// after the launcher exits.
	fakeGio(t, "sleep 20 &\necho $! > \"$RFE_TEST_OUT.child\"\nexit 0\n")
	t.Cleanup(func() { killPidFile(t, out+".child") })
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	req := appRequestWithID(http.MethodPost, rec.ID)
	req = req.WithContext(withDevice(req.Context(), &store.Device{ID: "launch-dev", ViewApps: true, LaunchApps: true}))
	rr := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		launchAppHandler(db)(rr, req)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("launch handler still blocked 5s after the launcher exited while its child keeps running")
	}
	if rr.Code != http.StatusAccepted {
		t.Fatalf("launch status = %d: %s", rr.Code, rr.Body.String())
	}
}

func TestLaunchHostAppRunsLauncherDetachedWithoutPipes(t *testing.T) {
	rec := launchFixture(t)
	out := filepath.Join(t.TempDir(), "out")
	t.Setenv("DISPLAY", ":rfe-test")
	t.Setenv("RFE_TEST_OUT", out)
	fakeGio(t, "echo $$ > \"$RFE_TEST_OUT.pid\"\nfds=$(readlink /proc/$$/fd/0 /proc/$$/fd/1 /proc/$$/fd/2)\necho $fds > \"$RFE_TEST_OUT.fds\"\nawk '{print $6}' /proc/$$/stat > \"$RFE_TEST_OUT.sid\"\n")
	if err := launchHostApp(context.Background(), rec); err != nil {
		t.Fatalf("launchHostApp: %v", err)
	}
	fds, _ := os.ReadFile(out + ".fds")
	if got := strings.Fields(string(fds)); !reflect.DeepEqual(got, []string{os.DevNull, os.DevNull, os.DevNull}) {
		t.Fatalf("launcher stdio = %v, want all %s", got, os.DevNull)
	}
	pid, _ := os.ReadFile(out + ".pid")
	sid, _ := os.ReadFile(out + ".sid")
	if strings.TrimSpace(string(pid)) == "" || strings.TrimSpace(string(pid)) != strings.TrimSpace(string(sid)) {
		t.Fatalf("launcher pid %q and session id %q differ: not a new session leader", pid, sid)
	}
}

func TestLaunchHostAppReturnsAfterHandoffWaitWhenLauncherKeepsRunning(t *testing.T) {
	rec := launchFixture(t)
	out := filepath.Join(t.TempDir(), "out")
	t.Setenv("DISPLAY", ":rfe-test")
	t.Setenv("RFE_TEST_OUT", out)
	fakeGio(t, "echo $$ > \"$RFE_TEST_OUT.pid\"\nexec sleep 20\n")
	t.Cleanup(func() { killPidFile(t, out+".pid") })
	prev := launchHandoffWait
	launchHandoffWait = 200 * time.Millisecond
	t.Cleanup(func() { launchHandoffWait = prev })
	start := time.Now()
	if err := launchHostApp(context.Background(), rec); err != nil {
		t.Fatalf("launchHostApp: %v", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatalf("launchHostApp took %v", time.Since(start))
	}
}

// A client that disconnects during the handoff window must not turn a launch
// that is still in progress into a failure (a retry would start a second app).
func TestLaunchHostAppIgnoresClientCancelDuringHandoffWait(t *testing.T) {
	rec := launchFixture(t)
	out := filepath.Join(t.TempDir(), "out")
	t.Setenv("DISPLAY", ":rfe-test")
	t.Setenv("RFE_TEST_OUT", out)
	fakeGio(t, "echo $$ > \"$RFE_TEST_OUT.pid\"\nexec sleep 20\n")
	t.Cleanup(func() { killPidFile(t, out+".pid") })
	prev := launchHandoffWait
	launchHandoffWait = 600 * time.Millisecond
	t.Cleanup(func() { launchHandoffWait = prev })
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, cancel)
	defer cancel()
	if err := launchHostApp(ctx, rec); err != nil {
		t.Fatalf("launchHostApp after client cancel = %v, want nil (launcher still running)", err)
	}
}

// A real launcher failure inside the handoff window is still reported even
// when the client has already gone away.
func TestLaunchHostAppReportsLauncherFailureAfterClientCancel(t *testing.T) {
	rec := launchFixture(t)
	t.Setenv("DISPLAY", ":rfe-test")
	fakeGio(t, "sleep 0.4\nexit 3\n")
	prev := launchHandoffWait
	launchHandoffWait = 5 * time.Second
	t.Cleanup(func() { launchHandoffWait = prev })
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, cancel)
	defer cancel()
	if err := launchHostApp(ctx, rec); err != errAppStartFailed {
		t.Fatalf("launchHostApp = %v, want errAppStartFailed", err)
	}
}

func TestLaunchHostAppReportsQuickLauncherFailure(t *testing.T) {
	rec := launchFixture(t)
	t.Setenv("DISPLAY", ":rfe-test")
	fakeGio(t, "exit 3\n")
	if err := launchHostApp(context.Background(), rec); err != errAppStartFailed {
		t.Fatalf("launchHostApp = %v, want errAppStartFailed", err)
	}
}

func TestLaunchHostAppUsesDiscoveredSessionEnvironment(t *testing.T) {
	rec := launchFixture(t)
	runtime := t.TempDir()
	makeUnixSocketFile(t, filepath.Join(runtime, "wayland-7"))
	out := filepath.Join(t.TempDir(), "env")
	t.Setenv("DISPLAY", "")
	t.Setenv("WAYLAND_DISPLAY", "")
	t.Setenv("XDG_RUNTIME_DIR", runtime)
	t.Setenv("RFE_TEST_OUT", out)
	prevX := x11SocketDir
	x11SocketDir = t.TempDir()
	t.Cleanup(func() { x11SocketDir = prevX })
	fakeGio(t, "env > \"$RFE_TEST_OUT\"\n")
	if err := launchHostApp(context.Background(), rec); err != nil {
		t.Fatalf("launchHostApp: %v", err)
	}
	env, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains("\n"+string(env), "\nWAYLAND_DISPLAY=wayland-7\n") {
		t.Fatalf("launcher env lacks discovered WAYLAND_DISPLAY:\n%s", env)
	}
}

func TestLaunchHostAppWithoutAnySessionIsUnavailable(t *testing.T) {
	rec := launchFixture(t)
	t.Setenv("DISPLAY", "")
	t.Setenv("WAYLAND_DISPLAY", "")
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	prevX := x11SocketDir
	x11SocketDir = t.TempDir()
	t.Cleanup(func() { x11SocketDir = prevX })
	fakeGio(t, "exit 0\n")
	if err := launchHostApp(context.Background(), rec); err != errNoDesktopSession {
		t.Fatalf("launchHostApp = %v, want errNoDesktopSession", err)
	}
}

func TestDiscoverDesktopSession(t *testing.T) {
	uid := os.Getuid()
	type tc struct {
		name    string
		env     map[string]string
		setup   func(runtime, x11 string)
		uid     int
		wantEnv func(runtime string) []string
	}
	sock := func(dir, name string) func(runtime, x11 string) {
		return func(runtime, x11 string) {
			d := runtime
			if dir == "x11" {
				d = x11
			}
			makeUnixSocketFile(t, filepath.Join(d, name))
		}
	}
	cases := []tc{
		{name: "nothing found", uid: uid, setup: func(string, string) {}},
		{name: "session already in environment", uid: uid, env: map[string]string{"DISPLAY": ":3"}, setup: sock("rt", "wayland-0")},
		{name: "wayland socket", uid: uid, setup: sock("rt", "wayland-0"),
			wantEnv: func(rt string) []string { return []string{"WAYLAND_DISPLAY=wayland-0", "XDG_RUNTIME_DIR=" + rt} }},
		{name: "wayland lock file and plain file ignored", uid: uid, setup: func(rt, x string) {
			_ = os.WriteFile(filepath.Join(rt, "wayland-0.lock"), nil, 0o600)
			_ = os.WriteFile(filepath.Join(rt, "wayland-1"), nil, 0o600)
		}},
		{name: "lowest wayland socket wins", uid: uid, setup: func(rt, x string) {
			makeUnixSocketFile(t, filepath.Join(rt, "wayland-1"))
			makeUnixSocketFile(t, filepath.Join(rt, "wayland-0"))
		}, wantEnv: func(rt string) []string { return []string{"WAYLAND_DISPLAY=wayland-0", "XDG_RUNTIME_DIR=" + rt} }},
		{name: "x11 socket", uid: uid, setup: sock("x11", "X1"),
			wantEnv: func(string) []string { return []string{"DISPLAY=:1"} }},
		{name: "x11 numerically lowest, non-socket names ignored", uid: uid, setup: func(rt, x string) {
			makeUnixSocketFile(t, filepath.Join(x, "X10"))
			makeUnixSocketFile(t, filepath.Join(x, "X2"))
			_ = os.WriteFile(filepath.Join(x, "X0"), nil, 0o600)
			makeUnixSocketFile(t, filepath.Join(x, "Xfoo"))
		}, wantEnv: func(string) []string { return []string{"DISPLAY=:2"} }},
		{name: "x11 socket of another user ignored", uid: uid + 1, setup: sock("x11", "X1")},
		{name: "both session kinds", uid: uid, setup: func(rt, x string) {
			makeUnixSocketFile(t, filepath.Join(rt, "wayland-0"))
			makeUnixSocketFile(t, filepath.Join(x, "X0"))
		}, wantEnv: func(rt string) []string {
			return []string{"WAYLAND_DISPLAY=wayland-0", "XDG_RUNTIME_DIR=" + rt, "DISPLAY=:0"}
		}},
		// uid 1<<30 has no /run/user/<uid>, so the default location holds nothing.
		{name: "relative XDG_RUNTIME_DIR is not trusted", uid: 1 << 30, env: map[string]string{"XDG_RUNTIME_DIR": "rel"}, setup: sock("rt", "wayland-0")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			runtime, x11 := t.TempDir(), t.TempDir()
			c.setup(runtime, x11)
			env := map[string]string{"XDG_RUNTIME_DIR": runtime}
			for k, v := range c.env {
				env[k] = v
			}
			var want []string
			if c.wantEnv != nil {
				want = c.wantEnv(runtime)
			}
			got := discoverDesktopSession(func(k string) string { return env[k] }, c.uid, x11)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("discoverDesktopSession = %v, want %v", got, want)
			}
		})
	}
}
