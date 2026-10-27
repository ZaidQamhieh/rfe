package fsops

import (
	"errors"
	"syscall"
)

// isCrossDevice reports whether err means a rename failed because source and
// destination are on different filesystems or volumes, so the caller should
// fall back to copy+remove. EXDEV is the POSIX errno; Windows reports
// ERROR_NOT_SAME_DEVICE instead (see isCrossDeviceOS).
func isCrossDevice(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, syscall.EXDEV) || isCrossDeviceOS(err)
}
