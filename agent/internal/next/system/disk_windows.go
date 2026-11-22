//go:build windows

package system

import "golang.org/x/sys/windows"

// statDisk returns the bytes available to the calling user and the total size
// of the volume holding path.
func statDisk(path string) (free, total uint64, err error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, 0, err
	}
	var avail, tot, totalFree uint64
	if err := windows.GetDiskFreeSpaceEx(p, &avail, &tot, &totalFree); err != nil {
		return 0, 0, err
	}
	return avail, tot, nil
}
