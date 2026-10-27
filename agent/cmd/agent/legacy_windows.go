//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

const legacyTaskName = "RFEAgent"

// retireLegacyService ends and deletes the RFEAgent scheduled task. Only `rfe install` does it: the
// daemon cannot tell whether it was itself started by that task, so it leaves the task and the old
// folder alone and reports false while the task exists.
func retireLegacyService(daemon bool) (bool, error) {
	state, err := taskState(legacyTaskName)
	if err != nil {
		return false, err
	}
	if state == "not installed" {
		return true, nil
	}
	if daemon {
		return false, nil
	}
	if strings.EqualFold(state, "Running") {
		if out, err := exec.Command("schtasks", "/end", "/tn", legacyTaskName).CombinedOutput(); err != nil {
			return false, fmt.Errorf("stop scheduled task %q (%s): %w", legacyTaskName, strings.TrimSpace(string(out)), err)
		}
	}
	if out, err := exec.Command("schtasks", "/delete", "/tn", legacyTaskName, "/f").CombinedOutput(); err != nil {
		return false, fmt.Errorf("delete scheduled task %q (%s): %w", legacyTaskName, strings.TrimSpace(string(out)), err)
	}
	fmt.Fprintf(os.Stderr, "removed the old scheduled task %q\n", legacyTaskName)
	return true, nil
}
