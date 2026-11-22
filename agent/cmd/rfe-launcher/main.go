// Command rfe-launcher keeps the agent running from an install root laid out as
//
//	<root>/state.json  <root>/versions/<v>/{rfe,rfe-indexd,rfe-thumbd,manifest.json}  <root>/staging/
//
// It picks the version state.json names, checks the agent binary against its bundle manifest, runs it as a child
// with the arguments and environment it was given, forwards termination signals, restarts into a pending version
// when the agent exits with 75, and otherwise passes the agent's exit code to the OS supervisor. It never opens the
// agent's database or creates certificates, and needs no admin rights. The root is $RFE_BUNDLE_ROOT or the
// directory of this executable.
//
// "rfe-launcher bundle ..." assembles a bundle instead (see bundle.go); that first argument is reserved.
package main

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/ZaidQamhieh/rfe/agent/internal/launcher"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "bundle" {
		if err := bundleMain(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "rfe-launcher bundle:", err)
			os.Exit(2)
		}
		return
	}
	root, err := installRoot()
	if err != nil {
		fmt.Fprintln(os.Stderr, "rfe-launcher:", err)
		os.Exit(launcher.ExitLauncherError)
	}
	sigs := make(chan os.Signal, 4)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	code, err := launcher.Run(launcher.Config{
		Root:      root,
		Args:      os.Args[1:],
		Signals:   sigs,
		DataDir:   dataDir(os.Args[1:]),
		ProbeAddr: probeAddr(os.Args[1:]),
		Logf:      func(f string, a ...any) { fmt.Fprintf(os.Stderr, "rfe-launcher: "+f+"\n", a...) },
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "rfe-launcher:", err)
	}
	os.Exit(code)
}

// flagValue returns the value of -name or --name (as "-name v" or "-name=v") in args, or "".
func flagValue(args []string, name string) string {
	for i, a := range args {
		for _, p := range []string{"-" + name, "--" + name} {
			if a == p && i+1 < len(args) {
				return args[i+1]
			}
			if strings.HasPrefix(a, p+"=") {
				return strings.TrimPrefix(a, p+"=")
			}
		}
	}
	return ""
}

// dataDir resolves the agent's data dir the way the agent does: -data flag, then $RFE_DATA_DIR, then ~/.rfe. It only
// computes the path (for rollback decisions); the launcher never opens anything in it.
func dataDir(args []string) string {
	if d := flagValue(args, "data"); d != "" {
		return d
	}
	if d := os.Getenv("RFE_DATA_DIR"); d != "" {
		return d
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".rfe")
	}
	return ""
}

// probeAddr is the agent's TLS address for the launcher's certificate check: $RFE_PROBE_ADDR, else the agent's own
// -addr argument, else empty (the address the agent reports in its heartbeat is used).
func probeAddr(args []string) string {
	if a := os.Getenv("RFE_PROBE_ADDR"); a != "" {
		return a
	}
	return flagValue(args, "addr")
}

// installRoot returns $RFE_BUNDLE_ROOT, or else the directory holding the launcher executable.
func installRoot() (string, error) {
	if r := os.Getenv(launcher.RootEnv); r != "" {
		return filepath.Abs(r)
	}
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return filepath.Dir(exe), nil
}
