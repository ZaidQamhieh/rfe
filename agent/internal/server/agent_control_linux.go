//go:build linux

package server

import "os/exec"

func restartSupported() bool { return true }

// restartAgent shells out to the same command already used manually to
// restart the agent (`systemctl --user restart rfe.service`). The
// process inherits XDG_RUNTIME_DIR/DBUS_SESSION_BUS_ADDRESS from its own
// systemd user-session environment, so no extra env setup is needed here.
func restartAgent() error {
	return exec.Command("systemctl", "--user", "restart", "rfe.service").Run()
}
