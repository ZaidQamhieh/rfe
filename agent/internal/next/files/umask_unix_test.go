//go:build unix

package files

import (
	"os"
	"syscall"
	"testing"
)

// fixedUmask is the umask the mode tests run under.
const fixedUmask = 0o022

// pinUmask sets the process umask to fixedUmask for the rest of the test. The
// umask is process-wide, so a test that calls it must not run in parallel.
func pinUmask(t *testing.T) {
	t.Helper()
	old := syscall.Umask(fixedUmask)
	t.Cleanup(func() { syscall.Umask(old) })
}

// chmodPath sets the permission bits of the file at p.
func chmodPath(p string, mode os.FileMode) error { return os.Chmod(p, mode) }
