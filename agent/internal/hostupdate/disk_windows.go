//go:build windows

package hostupdate

import "golang.org/x/sys/windows"

// FreeSpace returns the bytes available to the calling user on the volume holding dir (GetDiskFreeSpaceEx).
func FreeSpace(dir string) (uint64, error) {
	p, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return 0, err
	}
	var avail, total, free uint64
	if err := windows.GetDiskFreeSpaceEx(p, &avail, &total, &free); err != nil {
		return 0, err
	}
	return avail, nil
}
