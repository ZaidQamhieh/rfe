//go:build linux

package fsops

import "testing"

// TestDrives_RootIsOS verifies that on Linux, the "/" mount point is reported
// as the OS drive via Drives().
func TestDrives_RootIsOS(t *testing.T) {
	drives, err := Drives()
	if err != nil {
		t.Fatalf("Drives: %v", err)
	}
	if len(drives) == 0 {
		t.Fatal("expected at least one drive")
	}
	if drives[0].Path != "/" {
		t.Fatalf("expected first drive to be \"/\", got %q", drives[0].Path)
	}
	if !drives[0].IsOS {
		t.Fatalf("expected \"/\" drive to have IsOS=true, got: %+v", drives[0])
	}

	// Exactly one drive should be marked as the OS drive.
	count := 0
	for _, d := range drives {
		if d.IsOS {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 IsOS drive, got %d", count)
	}
}
