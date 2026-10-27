//go:build windows

package server

import (
	"syscall"
	"unsafe"
)

func diskStats(path string) (free, total uint64, err error) {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	proc := kernel32.NewProc("GetDiskFreeSpaceExW")
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return
	}
	var avail, tot, totalFree uint64
	proc.Call(
		uintptr(unsafe.Pointer(p)),
		uintptr(unsafe.Pointer(&avail)),
		uintptr(unsafe.Pointer(&tot)),
		uintptr(unsafe.Pointer(&totalFree)),
	)
	return totalFree, tot, nil
}
