//go:build darwin

package main

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const launchdLabel = "com.rfe.agent"

func installService(execPath string) error {
	path, err := launchAgentPlistPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create LaunchAgents dir: %w", err)
	}

	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>%s</string>
	<key>ProgramArguments</key>
	<array>
		<string>%s</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<true/>
</dict>
</plist>
`, launchdLabel, plistXMLText(execPath))
	if err := os.WriteFile(path, []byte(plist), 0o644); err != nil {
		return fmt.Errorf("write plist: %w", err)
	}

	cmd := exec.Command("launchctl", "load", "-w", path)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("launchctl load: %w", err)
	}

	fmt.Printf("Installed and started %s (plist: %s)\n", launchdLabel, path)
	return nil
}

func startService() error {
	path, err := launchAgentPlistPath()
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("agent is not installed; run `rfe install` first")
		}
		return fmt.Errorf("check LaunchAgent file: %w", err)
	}
	domain := launchAgentDomain()
	bootstrap := exec.Command("launchctl", "bootstrap", domain, path)
	if output, err := bootstrap.CombinedOutput(); err == nil {
		fmt.Printf("Started %s\n", launchdLabel)
		return nil
	} else {
		// The job may already be loaded after a prior start or login. In that
		// case, kickstart it; if it is not loaded, preserve the bootstrap error.
		kickstart := exec.Command("launchctl", "kickstart", "-k", domain+"/"+launchdLabel)
		if kickOutput, kickErr := kickstart.CombinedOutput(); kickErr == nil {
			fmt.Printf("Started %s\n", launchdLabel)
			return nil
		} else {
			return fmt.Errorf("launchctl bootstrap failed (%s): %v; kickstart failed (%s): %v",
				strings.TrimSpace(string(output)), err, strings.TrimSpace(string(kickOutput)), kickErr)
		}
	}
}

func stopService() error {
	path, err := launchAgentPlistPath()
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("agent is not installed; run `rfe install` first")
		}
		return fmt.Errorf("check LaunchAgent file: %w", err)
	}
	cmd := exec.Command("launchctl", "bootout", launchAgentDomain()+"/"+launchdLabel)
	if output, err := cmd.CombinedOutput(); err != nil {
		// A job that is not loaded is already stopped (bootout reports "No such process" / "Could not find service").
		if text := strings.ToLower(string(output)); strings.Contains(text, "no such process") || strings.Contains(text, "could not find service") {
			fmt.Printf("%s was not running; it remains configured to start at login\n", launchdLabel)
			return nil
		}
		return fmt.Errorf("launchctl bootout failed (%s): %w", strings.TrimSpace(string(output)), err)
	}
	fmt.Printf("Stopped %s; it remains configured to start at login\n", launchdLabel)
	return nil
}

func serviceStatus() (string, error) {
	path, err := launchAgentPlistPath()
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return "not installed", nil
		}
		return "", fmt.Errorf("check LaunchAgent file: %w", err)
	}
	cmd := exec.Command("launchctl", "print", launchAgentDomain()+"/"+launchdLabel)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "installed; state unavailable (service not loaded or GUI session unavailable)", nil
	}
	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "state = ") {
			return "installed; state=" + strings.TrimSpace(strings.TrimPrefix(line, "state = ")), nil
		}
	}
	return "installed; loaded (launchd state not reported)", nil
}

func launchAgentDomain() string {
	return fmt.Sprintf("gui/%d", os.Getuid())
}

func plistXMLText(value string) string {
	var encoded bytes.Buffer
	_ = xml.EscapeText(&encoded, []byte(value))
	return encoded.String()
}

func uninstallService() error {
	path, err := launchAgentPlistPath()
	if err != nil {
		return err
	}

	// Best-effort: unload even if it is already stopped, so a half-installed
	// state still cleans up.
	var stopErr error
	if _, statErr := os.Stat(path); statErr == nil {
		stopErr = stopService()
	}

	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove plist: %w", err)
	}

	if stopErr != nil {
		// Do not claim it stopped: a KeepAlive job may still be running (audit F-125).
		return fmt.Errorf("removed %s, but it could not be stopped (%w); the agent may keep running until you log out or run `launchctl bootout gui/$(id -u)/%s`", launchdLabel, stopErr, launchdLabel)
	}
	fmt.Printf("Stopped and removed %s\n", launchdLabel)
	return nil
}

func launchAgentPlistPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home dir: %w", err)
	}
	return filepath.Join(home, "Library", "LaunchAgents", launchdLabel+".plist"), nil
}
