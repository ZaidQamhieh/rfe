package sidecar

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// swapSuffix is the extra bytes appended to a copy of the test binary: it still runs as the fake sidecar, but its
// hash differs from the one recorded before the swap.
const swapSuffix = "\nswapped\n"

// copyTestBinary copies the running test binary (which doubles as the fake sidecar) to a new file in dir and
// returns its path and sha256.
func copyTestBinary(t *testing.T, dir string) (path, sum string) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	path = filepath.Join(dir, "fake-sidecar")
	if runtime.GOOS == "windows" {
		path += windowsExeSuffix
	}
	if err := os.WriteFile(path, body, 0o755); err != nil {
		t.Fatal(err)
	}
	sum, err = fileSHA256(path)
	if err != nil {
		t.Fatal(err)
	}
	return path, sum
}

// swapBinary replaces the file at path by renaming a changed copy over it (a running executable cannot be
// written in place on every OS), as an attacker with write access to the install directory would.
func swapBinary(t *testing.T, path string) {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	tmp := path + ".new"
	if err := os.WriteFile(tmp, append(body, swapSuffix...), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, path); err != nil {
		t.Fatal(err)
	}
}

func TestStartRefusesFileChangedSinceVerification(t *testing.T) {
	path, sum := copyTestBinary(t, t.TempDir())
	cfg := fakeCfg("ok")
	cfg.Path, cfg.SHA256 = path, sum

	c, err := Start(cfg)
	if err != nil {
		t.Fatalf("an unchanged file must start: %v", err)
	}
	_ = c.Close()

	swapBinary(t, path)
	if c, err := Start(cfg); !errors.Is(err, ErrHashChanged) {
		if c != nil {
			_ = c.Close()
		}
		t.Fatalf("swapped file started or wrong error: %v", err)
	}
}

func TestSupervisorRefusesBinarySwappedAfterFirstStart(t *testing.T) {
	path, sum := copyTestBinary(t, t.TempDir())
	cfg := fakeCfg("ok")
	cfg.Path, cfg.SHA256 = path, sum
	s := NewSupervisor(cfg, nil)
	s.minBackoff, s.maxBackoff, s.pingEvery = 10*time.Millisecond, 20*time.Millisecond, time.Hour
	s.Run()
	defer s.Stop()
	waitUntil(t, func() bool { _, err := s.Client(); return err == nil })

	swapBinary(t, path)
	_ = s.Call(context.Background(), "crash", struct{}{}, nil)
	waitUntil(t, func() bool { return s.Stats().StartFailures >= 1 })

	st := s.Stats()
	if st.Starts != 1 || st.Running {
		t.Fatalf("the swapped binary was restarted: %+v", st)
	}
	if _, err := s.Client(); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("Client() = %v, want ErrUnavailable", err)
	}
}

// writeUnmanifested puts a sidecar file named for short into dir with no manifest and removes it afterwards.
func writeUnmanifested(t *testing.T, dir, short string) {
	t.Helper()
	name := FileName(short)
	if runtime.GOOS == "windows" {
		name += windowsExeSuffix
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("fake sidecar"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(p) })
}

// exeDir is the directory Locate searches after $RFE_SIDECAR_DIR: the one holding the running test binary.
func exeDir(t *testing.T) string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return filepath.Dir(exe)
}

func TestLocateToRunUnverifiedOnlyAsExplicitDevBuild(t *testing.T) {
	devDir := t.TempDir()
	writeUnmanifested(t, devDir, "indexd")
	t.Setenv(DirEnv, devDir)

	if loc, ok, err := LocateToRun("indexd", true); err != nil || !ok || loc.Verified || !loc.DevDir {
		t.Fatalf("explicit dev build: %+v ok=%v err=%v", loc, ok, err)
	}
	if _, ok, err := LocateToRun("indexd", false); ok || !errors.Is(err, ErrUnverifiedNotAllowed) {
		t.Fatalf("unverified without RFE_SIDECARS: ok=%v err=%v", ok, err)
	}

	// The same file beside the agent executable, as in a packaged install whose manifest is gone, is refused
	// even though RFE_SIDECARS names it.
	t.Setenv(DirEnv, "")
	writeUnmanifested(t, exeDir(t), "thumbd")
	if _, ok, err := LocateToRun("thumbd", true); ok || !errors.Is(err, ErrUnverifiedNotAllowed) {
		t.Fatalf("manifest-less sidecar beside the executable: ok=%v err=%v", ok, err)
	}
}

func TestLocateToRunAllowsVerifiedAndReportsHash(t *testing.T) {
	dir := installFake(t, func(sum string) string { return "version 1.4.0\nsha256 " + sum + " rfe-indexd\n" })
	want, err := fileSHA256(filepath.Join(dir, exeNameForTest("indexd")))
	if err != nil {
		t.Fatal(err)
	}
	for _, explicit := range []bool{false, true} {
		loc, ok, err := LocateToRun("indexd", explicit)
		if err != nil || !ok || !loc.Verified || loc.SHA256 != want {
			t.Fatalf("explicit=%v: %+v ok=%v err=%v", explicit, loc, ok, err)
		}
	}
}

// exeNameForTest is the file name Locate looks for given a short name.
func exeNameForTest(short string) string {
	name := FileName(short)
	if runtime.GOOS == "windows" {
		name += windowsExeSuffix
	}
	return name
}
