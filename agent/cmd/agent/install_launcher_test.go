package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestAdoptLauncherBuildsABundleAndKeepsIt(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("builds helper binaries with a unix layout")
	}
	src := t.TempDir()
	build := func(pkg, out string) {
		cmd := exec.Command("go", "build", "-o", filepath.Join(src, out), pkg)
		if b, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("go build %s: %v: %s", pkg, err, b)
		}
	}
	build("github.com/ZaidQamhieh/rfe/agent/cmd/rfe-launcher", "rfe-launcher")
	if err := os.WriteFile(filepath.Join(src, "rfe"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(t.TempDir(), ".rfe-app")
	got, err := adoptLauncher(src, root, "1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Join(root, "rfe-launcher") {
		t.Fatalf("launcher path %q", got)
	}
	for _, p := range []string{"state.json", "versions/1.2.3/rfe", "versions/1.2.3/manifest.json"} {
		if _, err := os.Stat(filepath.Join(root, p)); err != nil {
			t.Errorf("missing %s: %v", p, err)
		}
	}
	state, _ := os.ReadFile(filepath.Join(root, "state.json"))
	if _, err := adoptLauncher(src, root, "1.2.3"); err != nil { // second run: nothing rebuilt, no error
		t.Fatal(err)
	}
	if again, _ := os.ReadFile(filepath.Join(root, "state.json")); string(again) != string(state) {
		t.Error("state.json changed on a repeat install")
	}
}

func TestAdoptLauncherNeedsTheLauncherBesideTheAgent(t *testing.T) {
	if _, err := adoptLauncher(t.TempDir(), filepath.Join(t.TempDir(), "r"), "1.0.0"); err == nil {
		t.Fatal("no error without rfe-launcher")
	}
}
