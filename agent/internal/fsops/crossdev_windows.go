//go:build windows

package fsops

import (
	"errors"
	"syscall"
)

// errorNotSameDevice is the Win32 ERROR_NOT_SAME_DEVICE code (17) that
// MoveFileEx surfaces, via os.Rename, for a cross-volume move.
const errorNotSameDevice = syscall.Errno(17)

func isCrossDeviceOS(err error) bool { return errors.Is(err, errorNotSameDevice) }
