//go:build !unix

package files

import (
	"os"
	"testing"
)

// pinUmask does nothing where there is no umask.
func pinUmask(t *testing.T) { t.Helper() }

// chmodPath sets the permission bits of the file at p.
func chmodPath(p string, mode os.FileMode) error { return os.Chmod(p, mode) }
