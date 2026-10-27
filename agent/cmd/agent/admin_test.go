//go:build linux

package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/settings"
	"github.com/ZaidQamhieh/rfe/agent/internal/store"
)

func captureStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	out, err := os.CreateTemp(t.TempDir(), "stdout-*")
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = out
	callErr := fn()
	os.Stdout = old
	if _, err := out.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	data, readErr := io.ReadAll(out)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
	return string(data), callErr
}

func discardStderr(t *testing.T, fn func() error) error {
	t.Helper()
	out, err := os.CreateTemp(t.TempDir(), "stderr-*")
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stderr
	os.Stderr = out
	callErr := fn()
	os.Stderr = old
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
	return callErr
}

func initializeAdminData(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	db, err := store.Open(dir)
	if err != nil {
		t.Fatalf("initialize database: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestAdminDispatchAndFormattingHelpers(t *testing.T) {
	if _, err := captureStdout(t, func() error { return runAdmin("help", nil) }); err != nil {
		t.Fatalf("help command: %v", err)
	}
	if err := discardStderr(t, func() error { return runAdmin("does-not-exist", nil) }); err == nil || !strings.Contains(err.Error(), "unknown command") {
		t.Fatalf("unknown command error = %v", err)
	}
	if got := verb(false); got != "revoke" {
		t.Errorf("verb(false) = %q", got)
	}
	if got := verb(true); got != "remove" {
		t.Errorf("verb(true) = %q", got)
	}
	if got := shortID("12345678abcdefgh"); got != "12345678" {
		t.Errorf("short ID = %q", got)
	}
	if got := shortID("short"); got != "short" {
		t.Errorf("short ID unchanged = %q", got)
	}
	for _, tc := range []struct {
		input string
		want  bool
	}{{"ON", true}, {" yes ", true}, {"1", true}, {"true", true}, {"off", false}, {"NO", false}, {"0", false}, {"false", false}} {
		if got, err := parseOnOff(tc.input); err != nil || got != tc.want {
			t.Errorf("parseOnOff(%q) = (%v,%v), want %v/nil", tc.input, got, err, tc.want)
		}
	}
	if _, err := parseOnOff("sometimes"); err == nil {
		t.Error("parseOnOff accepted an unknown value")
	}
	now := time.Now()
	for _, tc := range []struct {
		at   time.Time
		want string
	}{{now, "just now"}, {now.Add(-5 * time.Minute), "5m ago"}, {now.Add(-2 * time.Hour), "2h ago"}, {now.Add(-48 * time.Hour), "2d ago"}} {
		if got := humanizeSince(tc.at); got != tc.want {
			t.Errorf("humanizeSince(%v) = %q, want %q", tc.at, got, tc.want)
		}
	}
	if got := adminDataDir("/explicit"); got != "/explicit" {
		t.Errorf("adminDataDir explicit = %q", got)
	}
	if got := adminDataDir(""); got != defaultDataDir() {
		t.Errorf("adminDataDir default = %q, want %q", got, defaultDataDir())
	}
	if _, err := openAdminStore(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Error("openAdminStore accepted a missing data directory")
	}
	data := initializeAdminData(t)
	db, err := openAdminStore(data)
	if err != nil {
		t.Fatalf("openAdminStore existing directory: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestAdminAccountDeviceAndAuditCommands(t *testing.T) {
	data := initializeAdminData(t)
	if _, err := captureStdout(t, func() error { return cmdAddUser([]string{"-data", data, "-password", "short", "owner"}) }); err == nil {
		t.Fatal("adduser accepted a short password")
	}
	output, err := captureStdout(t, func() error {
		return cmdAddUser([]string{"-data", data, "-password", "correct-horse-battery", "owner"})
	})
	if err != nil || !strings.Contains(output, `Account "owner" created`) {
		t.Fatalf("adduser = (%q, %v)", output, err)
	}
	if _, err := captureStdout(t, func() error {
		return cmdAddUser([]string{"-data", data, "-password", "correct-horse-battery", "owner"})
	}); err == nil {
		t.Fatal("adduser silently replaced an existing account")
	}
	if err := cmdAddUser([]string{"-data", data}); err == nil {
		t.Fatal("adduser accepted missing username")
	}

	db, err := store.Open(data)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.CreateDevice("device-long-id-1", "browser", "token-1"); err != nil {
		t.Fatal(err)
	}
	if err := db.SetDeviceReadOnly("device-long-id-1", true); err != nil {
		t.Fatal(err)
	}
	if err := db.TouchDevice("device-long-id-1", "192.0.2.1", "1.2.3"); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateDevice("device-long-id-2", "old phone", "token-2"); err != nil {
		t.Fatal(err)
	}
	if err := db.RevokeDevice("device-long-id-2"); err != nil {
		t.Fatal(err)
	}
	if err := db.AppendAudit(store.AuditDeviceRevoked, "owner", "device-long-id-2", "manual"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	output, err = captureStdout(t, func() error { return cmdDevices([]string{"-data", data}) })
	if err != nil || !strings.Contains(output, "LABEL") || !strings.Contains(output, "read-only") || !strings.Contains(output, "revoked") {
		t.Fatalf("devices = (%q, %v)", output, err)
	}
	output, err = captureStdout(t, func() error { return cmdAudit([]string{"-data", data, "-n", "1"}) })
	if err != nil || !strings.Contains(output, "device_revoked") || !strings.Contains(output, "manual") {
		t.Fatalf("audit = (%q, %v)", output, err)
	}
	if _, err := captureStdout(t, func() error { return cmdAudit([]string{"-data", filepath.Join(data, "missing")}) }); err == nil {
		t.Fatal("audit accepted a missing agent data directory")
	}
}

func TestAdminPairAccessAndJailCommands(t *testing.T) {
	data := initializeAdminData(t)
	root := filepath.Join(t.TempDir(), "allowed")
	if err := os.MkdirAll(filepath.Join(root, "child"), 0o700); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(data)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.CreateDevice("jail-device", "phone", "jail-token"); err != nil {
		t.Fatal(err)
	}
	if _, err := settings.Load(db, false, []string{root}, "test"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	output, err := captureStdout(t, func() error {
		return cmdPair([]string{"-data", data, "-addr", "127.0.0.1:8765", "-ttl", "1h"})
	})
	if err != nil || !strings.Contains(output, "Pairing code:") || !strings.Contains(output, "Fingerprint:") {
		t.Fatalf("pair = (%q, %v)", output, err)
	}
	fields := strings.Fields(output)
	var code string
	for i := range fields {
		if fields[i] == "code:" && i+1 < len(fields) {
			code = fields[i+1]
			break
		}
	}
	if code == "" {
		t.Fatalf("pairing code missing in output %q", output)
	}
	db, err = store.Open(data)
	if err != nil {
		t.Fatal(err)
	}
	if info := db.ConsumePairingCode(code); !info.Valid {
		db.Close()
		t.Fatal("printed pairing code was not stored in the database")
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	if _, err := captureStdout(t, func() error { return cmdJail([]string{"-data", data, "jail-device", filepath.Join(root, "child")}) }); err != nil {
		t.Fatalf("set jail: %v", err)
	}
	db, err = store.Open(data)
	if err != nil {
		t.Fatal(err)
	}
	device, err := db.GetDeviceByID("jail-device")
	if err != nil || device == nil || device.JailRoot != filepath.Join(root, "child") {
		t.Fatalf("persisted jail = (%+v, %v)", device, err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := captureStdout(t, func() error { return cmdJail([]string{"-data", data, "jail-device", ""}) }); err != nil {
		t.Fatalf("clear jail: %v", err)
	}
	if err := cmdJail([]string{"-data", data, "jail-device"}); err == nil {
		t.Fatal("jail accepted a missing path argument")
	}
	if _, err := captureStdout(t, func() error { return cmdReadonly([]string{"-data", data, "jail-device", "on"}) }); err != nil {
		t.Fatalf("set read-only: %v", err)
	}
	db, err = store.Open(data)
	if err != nil {
		t.Fatal(err)
	}
	device, err = db.GetDeviceByID("jail-device")
	if err != nil || device == nil || !device.ReadOnly {
		t.Fatalf("read-only update = (%+v, %v)", device, err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := captureStdout(t, func() error { return cmdReadonly([]string{"-data", data, "jail-device", "off"}) }); err != nil {
		t.Fatalf("clear read-only: %v", err)
	}
	if _, err := captureStdout(t, func() error { return cmdReadonly([]string{"-data", data, "jail-device", "maybe"}) }); err == nil {
		t.Fatal("readonly accepted an invalid mode")
	}
}

func TestAdminRevokeRemoveAndStatusCommands(t *testing.T) {
	data := initializeAdminData(t)
	db, err := store.Open(data)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.CreateDevice("revoke-target", "revoked phone", "token-revoke"); err != nil {
		t.Fatal(err)
	}
	if err := db.CreateDevice("remove-target", "removed phone", "token-remove"); err != nil {
		t.Fatal(err)
	}
	if err := db.SetConfig("agentName", "Desk"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := captureStdout(t, func() error { return cmdRevokeOrRemove([]string{"-data", data, "revoke"}, false) }); err != nil {
		t.Fatalf("revoke command: %v", err)
	}
	if _, err := captureStdout(t, func() error { return cmdRevokeOrRemove([]string{"-data", data, "remove"}, true) }); err != nil {
		t.Fatalf("remove command: %v", err)
	}
	if err := cmdRevokeOrRemove([]string{"-data", data}, false); err == nil {
		t.Fatal("revoke accepted missing device id")
	}
	if err := cmdRevokeOrRemove([]string{"-data", data, "no-such-device"}, false); err == nil {
		t.Fatal("revoke accepted unknown device id")
	}
	db, err = store.Open(data)
	if err != nil {
		t.Fatal(err)
	}
	revoked, err := db.DeviceByToken("token-revoke")
	if err != nil || revoked == nil || !revoked.Revoked {
		t.Fatalf("revoked device = (%+v, %v)", revoked, err)
	}
	removed, err := db.DeviceByToken("token-remove")
	if err != nil || removed != nil {
		t.Fatalf("removed device = (%+v, %v)", removed, err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	home := t.TempDir()
	t.Setenv("HOME", home)
	unitDir := filepath.Join(home, ".config", "systemd", "user")
	if err := os.MkdirAll(unitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(unitDir, linuxUnitName), []byte("[Service]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	systemctl := "#!/bin/sh\ncase \"$2\" in\n is-active) echo active;;\n is-enabled) echo enabled;;\n *) exit 0;;\nesac\n"
	if err := os.WriteFile(filepath.Join(bin, "systemctl"), []byte(systemctl), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	output, err := captureStdout(t, func() error { return cmdStatus([]string{"-data", data, "-addr", "127.0.0.1:8765"}) })
	if err != nil || !strings.Contains(output, "name:        Desk") || !strings.Contains(output, "service:     installed; enabled; active") || !strings.Contains(output, "devices:     0 active, 1 total") {
		t.Fatalf("status = (%q, %v)", output, err)
	}
	if err := cmdStatus([]string{"-data", filepath.Join(data, "missing")}); err == nil {
		t.Fatal("status accepted missing data directory")
	}
}

func TestParseAllow(t *testing.T) {
	g, err := parseAllow("Download, upload")
	if err != nil || g != [6]bool{false, true, true, false, false, false} {
		t.Fatalf("got %v %v", g, err)
	}
	if g, _ := parseAllow("all"); g != [6]bool{true, true, true, true, true, true} {
		t.Fatalf("all: %v", g)
	}
	if g, _ := parseAllow("none"); g != [6]bool{} {
		t.Fatalf("none: %v", g)
	}
	if _, err := parseAllow("download,fly"); err == nil {
		t.Fatal("unknown permission accepted")
	}
}

func TestBackupDirInRoots(t *testing.T) {
	roots := []string{"/home/u/RFE Files"}
	for dir, want := range map[string]bool{
		"/home/u/RFE Files":           true,
		"/home/u/RFE Files/Phone":     true,
		"/home/u/RFE Files2":          false,
		"/home/u":                     false,
		"/home/u/RFE Files/../Secret": false,
	} {
		if got := backupDirInRoots(filepath.Clean(dir), roots); got != want {
			t.Errorf("%s: got %v want %v", dir, got, want)
		}
	}
	if !backupDirInRoots("/anywhere", nil) {
		t.Fatal("no roots means unrestricted")
	}
}
