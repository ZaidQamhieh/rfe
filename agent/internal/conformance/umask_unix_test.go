//go:build unix

package conformance

import (
	"syscall"
	"testing"
)

// fixedUmask is the umask the agent under test inherits, so the permission bits
// it gives new files do not depend on the shell that ran the tests.
const fixedUmask = 0o022

// pinUmask sets the process umask to fixedUmask for the rest of the test.
func pinUmask(t *testing.T) {
	t.Helper()
	old := syscall.Umask(fixedUmask)
	t.Cleanup(func() { syscall.Umask(old) })
}
