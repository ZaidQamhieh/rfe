package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// Before 1.0.0 the agent was called rfe-agent: its data lived in ~/.rfe-agent and it ran as the
// systemd --user unit rfe-agent.service (Linux) or the scheduled task RFEAgent (Windows). The
// daemon and `rfe install` move such an install over; the admin commands only read the old
// folder until then (defaultDataDir).
const (
	dataDirName       = ".rfe"
	legacyDataDirName = ".rfe-agent"
)

// moveLegacyInstall retires the old service, then moves ~/.rfe-agent to ~/.rfe. It does nothing
// unless the default data dir is in use and still is ~/.rfe-agent, so a daemon run with -data or
// $RFE_DATA_DIR (tests included) never touches a service. Problems are reported, never fatal: an
// agent that cannot move the old folder keeps using it.
func moveLegacyInstall(daemon bool) {
	if os.Getenv("RFE_DATA_DIR") != "" {
		return
	}
	home, err := os.UserHomeDir()
	if err != nil || !legacyDataDirInUse(filepath.Join(home, legacyDataDirName), filepath.Join(home, dataDirName)) {
		return
	}
	free, err := retireLegacyService(daemon)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not stop the old rfe-agent service: %v\n", err)
	}
	if !free {
		return
	}
	if err := migrateLegacyDataDir(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: %v; the agent keeps using the old folder\n", err)
	}
}

// migrateLegacyDataDir renames ~/.rfe-agent to ~/.rfe when the default data dir is in use, ~/.rfe
// does not exist and ~/.rfe-agent is a real directory. Callers make sure no old daemon has the
// database open (retireLegacyService).
func migrateLegacyDataDir() error {
	if os.Getenv("RFE_DATA_DIR") != "" {
		return nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	oldDir, newDir := filepath.Join(home, legacyDataDirName), filepath.Join(home, dataDirName)
	if !legacyDataDirInUse(oldDir, newDir) {
		return nil
	}
	if err := os.Rename(oldDir, newDir); err != nil {
		return fmt.Errorf("move %s to %s: %w", oldDir, newDir, err)
	}
	fmt.Fprintf(os.Stderr, "moved the agent data from %s to %s\n", oldDir, newDir)
	return nil
}

// legacyDataDirInUse reports whether oldDir is a real directory and newDir does not exist yet.
func legacyDataDirInUse(oldDir, newDir string) bool {
	if _, err := os.Lstat(newDir); !os.IsNotExist(err) {
		return false
	}
	fi, err := os.Lstat(oldDir)
	return err == nil && fi.IsDir()
}
