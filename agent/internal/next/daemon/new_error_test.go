package daemon

import (
	"path/filepath"
	"testing"
)

// TestNewReturnsAnErrorForAMissingAllowedFolder checks that a failure while
// building the services is reported as an error and cleaned up, not a nil-pointer panic.
func TestNewReturnsAnErrorForAMissingAllowedFolder(t *testing.T) {
	t.Setenv(envSidecars, sidecarOffWords[0])
	missing := filepath.Join(t.TempDir(), "no-such-folder")
	d, err := New(Options{
		Addr: "127.0.0.1:0", Name: "test-agent", DataDir: t.TempDir(), Roots: []string{missing}, RootsExplicit: true,
		Version: "test", TrashDir: filepath.Join(t.TempDir(), "trash"), Logf: t.Logf,
	})
	if err == nil {
		d.Close()
		t.Fatal("New succeeded with an allowed folder that does not exist")
	}
	if d != nil {
		t.Errorf("New returned a daemon together with the error %v", err)
	}
}
