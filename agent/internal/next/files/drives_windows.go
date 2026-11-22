//go:build windows

package files

import (
	"os"
	"strings"
	"syscall"
	"unsafe"
)

// Windows drive letters: one bit of GetLogicalDrives per letter, and the system
// drive assumed when %SystemDrive% is not set.
const (
	driveLetterCount   = 26
	defaultSystemDrive = "C:"
	systemDriveEnv     = "SystemDrive"
)

// platformDrives lists the logical drives, with sizes from GetDiskFreeSpaceEx.
// The drive named by %SystemDrive% is marked as the OS drive.
func platformDrives() ([]Drive, error) {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	getLogicalDrives := kernel32.NewProc("GetLogicalDrives")
	getDiskFreeSpaceEx := kernel32.NewProc("GetDiskFreeSpaceExW")

	mask, _, _ := getLogicalDrives.Call()
	systemDrive := strings.ToUpper(os.Getenv(systemDriveEnv))
	if systemDrive == "" {
		systemDrive = defaultSystemDrive
	}
	drives := []Drive{}
	for i := 0; i < driveLetterCount; i++ {
		if uint32(mask)&(1<<uint(i)) == 0 {
			continue
		}
		letter := string(rune('A'+i)) + ":"
		root := letter + `\`
		d := Drive{Path: root, Label: root, IsOS: letter == systemDrive}
		var free, total, totalFree uint64
		p, err := syscall.UTF16PtrFromString(root)
		if err == nil {
			getDiskFreeSpaceEx.Call(
				uintptr(unsafe.Pointer(p)),
				uintptr(unsafe.Pointer(&free)),
				uintptr(unsafe.Pointer(&total)),
				uintptr(unsafe.Pointer(&totalFree)),
			)
		}
		d.TotalBytes = int64(total)
		d.FreeBytes = int64(totalFree)
		drives = append(drives, d)
	}
	return drives, nil
}
