package hostops

import (
	"os/exec"
	"runtime"
	"time"
)

// Restart settings.
const (
	// RestartDelay lets the HTTP answer reach the caller before the restart
	// command tears this process down.
	RestartDelay   = 300 * time.Millisecond
	restartOS      = "linux"
	restartCommand = "systemctl"
)

// Restarter restarts the agent's own systemd user unit. Restart-only by design:
// there is no remote stop, so a lost connection cannot leave the host stuck.
// The zero value is not usable; call NewRestarter.
type Restarter struct {
	// OS is the operating system name (runtime.GOOS values); only linux is supported.
	OS string
	// Unit is the unit to restart.
	Unit string
	// Delay is waited before the command runs.
	Delay time.Duration
	// Sleep waits for d; Run executes the command. Both are seams for tests.
	Sleep func(d time.Duration)
	Run   func(name string, args ...string) error
}

// NewRestarter returns a Restarter for this host that restarts AgentUnit after
// RestartDelay.
func NewRestarter() *Restarter {
	return &Restarter{
		OS: runtime.GOOS, Unit: AgentUnit, Delay: RestartDelay, Sleep: time.Sleep,
		Run: func(name string, args ...string) error { return exec.Command(name, args...).Run() },
	}
}

// Supported reports whether this host can restart the agent remotely.
func (r *Restarter) Supported() bool { return r.OS == restartOS }

// Restart waits Delay and then runs `systemctl --user restart <unit>`. It is
// meant to be called after the response was sent, and returns the command's
// error.
func (r *Restarter) Restart() error {
	r.Sleep(r.Delay)
	return r.Run(restartCommand, "--user", "restart", r.Unit)
}
