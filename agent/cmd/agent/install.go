package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// cmdInstall sets up the agent to auto-start at login, per-user and with no
// root/admin required: a systemd --user unit on Linux, a launchd
// LaunchAgent on macOS, or a Scheduled Task on Windows. See
// install_linux.go / install_darwin.go / install_windows.go for the
// platform-specific installService/uninstallService implementations.
func cmdInstall(args []string) error {
	// The service always runs against the standard data folder; accepting -data (or a different
	// $RFE_DATA_DIR) and silently dropping it would leave the service and the admin CLI on different
	// databases (audit F-91).
	useLauncher := false
	if len(args) == 1 && args[0] == "-launcher" {
		useLauncher = true
	} else if len(args) > 0 {
		return fmt.Errorf("install takes only -launcher (supervised install with automatic rollback): the service uses the standard data folder (~/.rfe)")
	}
	moveLegacyInstall(false)
	if home, herr := os.UserHomeDir(); herr == nil && filepath.Clean(defaultDataDir()) != filepath.Join(home, dataDirName) {
		return fmt.Errorf("the service uses the standard data folder %q; clear RFE_DATA_DIR before installing", filepath.Join(home, dataDirName))
	}
	exe, err := resolveExecutable()
	if err != nil {
		return err
	}
	if useLauncher {
		home, herr := os.UserHomeDir()
		if herr != nil {
			return herr
		}
		if exe, err = adoptLauncher(filepath.Dir(exe), filepath.Join(home, launcherRootName), version); err != nil {
			return err
		}
	}
	return installService(exe)
}

// cmdUninstall removes whatever cmdInstall set up.
func cmdUninstall(args []string) error {
	return uninstallService()
}

// cmdStart starts the already-installed user-level service.
func cmdStart(args []string) error {
	return startService()
}

// cmdStop stops the service while keeping its start-at-login registration.
func cmdStop(args []string) error {
	return stopService()
}

// resolveExecutable returns the real, symlink-resolved path to the running
// binary, so the installed auto-start entry keeps working even if the
// process was launched via a symlink (e.g. some package managers, `go
// install`).
func resolveExecutable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("resolve own executable path: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return exe, nil
}
