//go:build linux

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const legacyUnitName = "rfe-agent.service"

// retireLegacyService disables and stops rfe-agent.service and removes its user unit file. It
// returns true when no old daemon can hold the data folder any more: there is no such unit, it was
// stopped, or this daemon is that unit's process (it then keeps running until `rfe install`).
func retireLegacyService(daemon bool) (bool, error) {
	if _, err := exec.LookPath("systemctl"); err != nil {
		return true, nil
	}
	if daemon && runningInUnit(legacyUnitName) {
		return true, nil
	}
	userUnit := ""
	if dir, err := userSystemdUnitDir(); err == nil {
		userUnit = filepath.Join(dir, legacyUnitName)
	}
	active := strings.TrimSpace(runSystemctlOutput("is-active", legacyUnitName))
	if strings.HasPrefix(active, "unavailable") {
		// systemd --user did not answer: an old unit may be running, so leave its folder alone.
		return !legacyUnitFileExists(userUnit), nil
	}
	enabled := strings.TrimSpace(runSystemctlOutput("is-enabled", legacyUnitName))
	if active == "active" || active == "activating" || active == "reloading" || strings.HasPrefix(enabled, "enabled") || enabled == "linked" {
		if out, err := exec.Command("systemctl", "--user", "disable", "--now", legacyUnitName).CombinedOutput(); err != nil {
			return false, fmt.Errorf("systemctl --user disable --now %s: %s: %w", legacyUnitName, strings.TrimSpace(string(out)), err)
		}
		if state := strings.TrimSpace(runSystemctlOutput("is-active", legacyUnitName)); state == "active" || state == "deactivating" {
			return false, fmt.Errorf("%s is still %s", legacyUnitName, state)
		}
		fmt.Fprintf(os.Stderr, "stopped and disabled the old %s\n", legacyUnitName)
	}
	if userUnit != "" {
		if err := os.Remove(userUnit); err == nil {
			_ = exec.Command("systemctl", "--user", "daemon-reload").Run()
		} else if !os.IsNotExist(err) {
			return true, fmt.Errorf("remove %s: %w", userUnit, err)
		}
	}
	return true, nil
}

func legacyUnitFileExists(userUnit string) bool {
	for _, p := range []string{userUnit, "/usr/lib/systemd/user/" + legacyUnitName, "/etc/systemd/user/" + legacyUnitName} {
		if p == "" {
			continue
		}
		if _, err := os.Stat(p); err == nil {
			return true
		}
	}
	return false
}

// runningInUnit reports whether this process belongs to the given systemd unit (its cgroup path).
func runningInUnit(unit string) bool {
	b, err := os.ReadFile("/proc/self/cgroup")
	return err == nil && strings.Contains(string(b), "/"+unit)
}
