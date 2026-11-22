package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// launcherRootName is the per-user folder "rfe install -launcher" builds the supervised install in:
// <home>/.rfe-app/{rfe-launcher,state.json,versions/<v>/...}. It is separate from the data folder (~/.rfe).
const launcherRootName = ".rfe-app"

// adoptLauncher builds a launcher-supervised install of the running agent and returns the launcher path the OS
// supervisor must run. srcDir holds the agent (rfe), rfe-launcher and, when present, the helpers and
// rfe-sidecars.txt, as in a release archive. It never touches the data folder and never starts anything: a root
// that already has a state.json is kept, and a version that is already installed is not rebuilt.
func adoptLauncher(srcDir, root, ver string) (string, error) {
	ext := ""
	if runtime.GOOS == "windows" {
		ext = ".exe"
	}
	src := filepath.Join(srcDir, "rfe-launcher"+ext)
	if _, err := os.Stat(src); err != nil {
		return "", fmt.Errorf("rfe-launcher not found next to the agent (%s): install from a release archive", srcDir)
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", err
	}
	dst := filepath.Join(root, "rfe-launcher"+ext)
	if err := copyExecutable(src, dst); err != nil {
		return "", err
	}
	if _, err := os.Stat(filepath.Join(root, "versions", ver)); err != nil {
		out, err := exec.Command(dst, "bundle", "-version", ver, srcDir, root).CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("build bundle: %w: %s", err, out)
		}
	}
	return dst, nil
}

// copyExecutable copies src over dst through a temp file and a rename, so a running launcher is replaced safely.
func copyExecutable(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".rfe-launcher-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := io.Copy(tmp, in); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o755); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), dst)
}
