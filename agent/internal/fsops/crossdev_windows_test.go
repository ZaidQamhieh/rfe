//go:build windows

package fsops

import (
	"os"
	"syscall"
	"testing"
)

// Go's os.Rename across volumes on Windows fails with ERROR_NOT_SAME_DEVICE
// (17), not EXDEV; the trash move must still fall back to copy+remove.
func TestIsCrossDevice_WindowsNotSameDevice(t *testing.T) {
	if !isCrossDevice(&os.LinkError{Op: "rename", Err: syscall.Errno(17)}) {
		t.Fatal("ERROR_NOT_SAME_DEVICE must be cross-device")
	}
}
