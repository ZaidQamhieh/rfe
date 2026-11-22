//go:build !windows

package launcher

import "os/exec"

// bindToLauncher is a no-op: on Unix a supervisor stops the whole process group, and the launcher forwards signals.
func bindToLauncher(*exec.Cmd) error { return nil }
