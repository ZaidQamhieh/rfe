//go:build !windows

package fsops

// isCrossDeviceOS has nothing to add off Windows: there EXDEV is the only
// cross-device errno (errno 17 is EEXIST on Linux and must not match).
func isCrossDeviceOS(error) bool { return false }
