package launcher

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// deadPID starts this test binary as a short-lived child, waits for it and returns its pid, which no process holds now.
func deadPID(t *testing.T) int {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe, "-test.run=^$")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	return cmd.Process.Pid
}

func TestLockExcludesSecondHolder(t *testing.T) {
	p := filepath.Join(t.TempDir(), LockFile)
	l, err := AcquireLock(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := AcquireLock(p); !errors.Is(err, ErrLocked) {
		t.Fatalf("second acquire: %v", err)
	}
	if err := l.Release(); err != nil {
		t.Fatal(err)
	}
	l2, err := AcquireLock(p)
	if err != nil {
		t.Fatalf("acquire after release: %v", err)
	}
	l2.Release()
}

func TestStaleLockOfDeadProcessIsRecovered(t *testing.T) {
	p := filepath.Join(t.TempDir(), LockFile)
	if err := os.WriteFile(p, []byte(strconv.Itoa(deadPID(t))+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	l, err := AcquireLock(p)
	if err != nil {
		t.Fatalf("stale lock not recovered: %v", err)
	}
	data, _ := os.ReadFile(p)
	if string(data) != strconv.Itoa(os.Getpid())+"\n" {
		t.Fatalf("lock now holds %q", data)
	}
	l.Release()
}

func TestOldGarbageLockIsStaleButFreshOneIsHeld(t *testing.T) {
	p := filepath.Join(t.TempDir(), LockFile)
	os.WriteFile(p, []byte(""), 0o600)
	if _, err := AcquireLock(p); !errors.Is(err, ErrLocked) {
		t.Fatalf("fresh empty lock must count as held: %v", err)
	}
	old := time.Now().Add(-time.Hour)
	os.Chtimes(p, old, old)
	l, err := AcquireLock(p)
	if err != nil {
		t.Fatalf("old empty lock must be stale: %v", err)
	}
	l.Release()
}
