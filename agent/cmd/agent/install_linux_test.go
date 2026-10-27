//go:build linux

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func installFakeSystemctl(t *testing.T, script string) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(bin, "systemctl")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	return path
}

func TestSystemdPathEscapingAndServiceLifecycle(t *testing.T) {
	if got, err := systemdExecArg(`/opt/RFE\rfe"agent%1`); err != nil || got != `"/opt/RFE\\rfe\"agent%%1"` {
		t.Fatalf("systemdExecArg escaped = (%q, %v)", got, err)
	}
	for _, value := range []string{"/bin/rfe\nagent", "/bin/rfe\ragent"} {
		if _, err := systemdExecArg(value); err == nil {
			t.Errorf("systemdExecArg accepted line break in %q", value)
		}
	}

	home := t.TempDir()
	t.Setenv("HOME", home)
	logPath := filepath.Join(t.TempDir(), "systemctl.log")
	t.Setenv("RFE_TEST_SYSTEMCTL_LOG", logPath)
	installFakeSystemctl(t, `printf '%s\n' "$*" >> "$RFE_TEST_SYSTEMCTL_LOG"
case "$2" in
 is-active) echo active;;
 is-enabled) echo enabled;;
 *) exit 0;;
esac`)
	unitPath, err := linuxUnitPath()
	if err != nil || unitPath != filepath.Join(home, ".config", "systemd", "user", linuxUnitName) {
		t.Fatalf("linuxUnitPath = (%q, %v)", unitPath, err)
	}
	if got, err := userSystemdUnitDir(); err != nil || got != filepath.Join(home, ".config", "systemd", "user") {
		t.Fatalf("userSystemdUnitDir = (%q, %v)", got, err)
	}
	if status, err := serviceStatus(); err != nil || status != "not installed" {
		t.Fatalf("serviceStatus before install = (%q, %v)", status, err)
	}
	if err := startService(); err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Fatalf("startService before install = %v, want not-installed error", err)
	}
	if err := stopService(); err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Fatalf("stopService before install = %v, want not-installed error", err)
	}
	if _, err := captureStdout(t, func() error { return installService(`/opt/RFE Agent/rfe%1`) }); err != nil {
		t.Fatalf("installService: %v", err)
	}
	unit, err := os.ReadFile(unitPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(unit), `ExecStart="/opt/RFE Agent/rfe%%1"`) || !strings.Contains(string(unit), "Restart=on-failure") {
		t.Fatalf("generated service unit does not preserve a safe executable path:\n%s", unit)
	}
	if _, err := captureStdout(t, startService); err != nil {
		t.Fatalf("startService: %v", err)
	}
	if _, err := captureStdout(t, stopService); err != nil {
		t.Fatalf("stopService: %v", err)
	}
	if status, err := serviceStatus(); err != nil || status != "installed; enabled; active" {
		t.Fatalf("serviceStatus installed = (%q, %v)", status, err)
	}
	if got := runSystemctlOutput("is-active", linuxUnitName); got != "active" {
		t.Fatalf("runSystemctlOutput = %q, want active", got)
	}
	if _, err := captureStdout(t, uninstallService); err != nil {
		t.Fatalf("uninstallService: %v", err)
	}
	if _, err := os.Stat(unitPath); !os.IsNotExist(err) {
		t.Fatalf("unit still exists after uninstall (stat error %v)", err)
	}
	if status, err := serviceStatus(); err != nil || status != "not installed" {
		t.Fatalf("serviceStatus after uninstall = (%q, %v)", status, err)
	}
	if _, err := captureStdout(t, uninstallService); err != nil {
		t.Fatalf("uninstallService on absent unit should be idempotent: %v", err)
	}
	log, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, command := range []string{"--user daemon-reload", "--user enable --now rfe.service", "--user start rfe.service", "--user stop rfe.service", "--user disable --now rfe.service"} {
		if !strings.Contains(string(log), command) {
			t.Errorf("systemctl call %q missing from log:\n%s", command, log)
		}
	}
}

func TestSystemdFailurePathsAndStatusFallbacks(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	installFakeSystemctl(t, `case "$2" in
 is-active) echo active;;
 is-enabled) echo;;
	*) exit 0;;
esac`)
	if err := installService("/bin/rfe\nextra"); err == nil || !strings.Contains(err.Error(), "line break") {
		t.Fatalf("installService accepted unsafe path: %v", err)
	}
	if got := runSystemctlOutput("other"); got != "unknown" {
		t.Fatalf("empty successful output = %q, want unknown", got)
	}
	installFakeSystemctl(t, `exit 1`)
	if got := runSystemctlOutput("other"); got != "unavailable (systemd --user did not respond)" {
		t.Fatalf("failed systemctl output = %q", got)
	}
	if err := runSystemctl("restart", linuxUnitName); err == nil {
		t.Fatal("runSystemctl swallowed a command failure")
	}
	if err := installService("/bin/rfe"); err == nil || !strings.Contains(err.Error(), "daemon-reload") {
		t.Fatalf("installService did not wrap systemctl error: %v", err)
	}
	if _, err := serviceStatus(); err != nil {
		t.Fatalf("serviceStatus with no unit should return not-installed, got %v", err)
	}
}
