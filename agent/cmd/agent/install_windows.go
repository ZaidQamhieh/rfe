//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

const scheduledTaskName = "RFE"

// installService registers a per-user Scheduled Task that starts at logon.
// /rl limited keeps it a standard-user task (no admin elevation prompt),
// matching the no-root Linux/macOS install.
func installService(execPath string) error {
	cmd := exec.Command("schtasks", "/create", "/tn", scheduledTaskName,
		"/tr", `"`+execPath+`"`, "/sc", "onlogon", "/rl", "limited", "/f")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("schtasks create: %w", err)
	}

	// Start it now too rather than making the user log out/in to see it
	// running. Non-fatal: the task is installed either way.
	runCmd := exec.Command("schtasks", "/run", "/tn", scheduledTaskName)
	runCmd.Stdout = os.Stdout
	runCmd.Stderr = os.Stderr
	if err := runCmd.Run(); err != nil {
		fmt.Printf("Installed scheduled task %q, but couldn't start it now (%v) — it will start at next logon\n", scheduledTaskName, err)
		return nil
	}

	fmt.Printf("Installed and started scheduled task %q (runs at logon)\n", scheduledTaskName)
	return nil
}

func startService() error {
	state, err := taskState(scheduledTaskName)
	if err != nil {
		return err
	}
	if state == "not installed" {
		return fmt.Errorf("agent is not installed; run `rfe install` first")
	}
	cmd := exec.Command("schtasks", "/run", "/tn", scheduledTaskName)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("start scheduled task failed (%s): %w", strings.TrimSpace(string(output)), err)
	}
	fmt.Printf("Started scheduled task %q\n", scheduledTaskName)
	return nil
}

func stopService() error {
	state, err := taskState(scheduledTaskName)
	if err != nil {
		return err
	}
	if state == "not installed" {
		return fmt.Errorf("agent is not installed; run `rfe install` first")
	}
	if !strings.EqualFold(state, "Running") {
		fmt.Printf("Scheduled task %q is not running (state: %s)\n", scheduledTaskName, state)
		return nil
	}
	cmd := exec.Command("schtasks", "/end", "/tn", scheduledTaskName)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("stop scheduled task failed (%s): %w", strings.TrimSpace(string(output)), err)
	}
	fmt.Printf("Stopped scheduled task %q; it remains configured to start at login\n", scheduledTaskName)
	return nil
}

func serviceStatus() (string, error) {
	state, err := taskState(scheduledTaskName)
	if err != nil {
		return "", err
	}
	if state == "not installed" {
		return state, nil
	}
	return "installed; state=" + state, nil
}

func taskState(name string) (string, error) {
	powershell, err := exec.LookPath("powershell.exe")
	if err != nil {
		return "", fmt.Errorf("find PowerShell to inspect scheduled task: %w", err)
	}
	// Only a task that does not exist is "not installed"; any other failure (access denied, the
	// Task Scheduler service down) is an error, not a silent "not installed" (audit F-125).
	script := "try { $task = Get-ScheduledTask -TaskName '" + name + "' -ErrorAction Stop; [string]$task.State } " +
		"catch { if ($_.CategoryInfo.Category -eq 'ObjectNotFound') { 'not installed' } else { [Console]::Error.WriteLine($_.Exception.Message); exit 1 } }"
	cmd := exec.Command(powershell, "-NoLogo", "-NoProfile", "-NonInteractive", "-Command", script)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("query scheduled task failed (%s): %w", strings.TrimSpace(string(output)), err)
	}
	state := strings.TrimSpace(string(output))
	if state == "" {
		return "", fmt.Errorf("query scheduled task returned no state")
	}
	return state, nil
}

func uninstallService() error {
	// Deleting a task does not stop a running instance, so the agent would keep running with no
	// task to manage it: an unconfirmed stop stops the uninstall (audit F-125).
	state, err := taskState(scheduledTaskName)
	if err != nil {
		return err
	}
	if state == "not installed" {
		fmt.Printf("Scheduled task %q is not installed\n", scheduledTaskName)
		return nil
	}
	if strings.EqualFold(state, "Running") {
		if output, err := exec.Command("schtasks", "/end", "/tn", scheduledTaskName).CombinedOutput(); err != nil {
			return fmt.Errorf("stop scheduled task failed (%s): %w", strings.TrimSpace(string(output)), err)
		}
	}
	cmd := exec.Command("schtasks", "/delete", "/tn", scheduledTaskName, "/f")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("schtasks delete: %w", err)
	}

	fmt.Printf("Removed scheduled task %q\n", scheduledTaskName)
	return nil
}
