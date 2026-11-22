//go:build !unix

package conformance

import "testing"

// pinUmask does nothing where there is no umask.
func pinUmask(t *testing.T) { t.Helper() }
