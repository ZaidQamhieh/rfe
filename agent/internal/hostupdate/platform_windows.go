//go:build windows

package hostupdate

import (
	"runtime"

	"golang.org/x/sys/windows"
)

// IMAGE_FILE_MACHINE_ARM64 is the machine type IsWow64Process2 reports for an ARM64 host.
const imageFileMachineARM64 = 0xAA64

// detectEmulatedHostArch returns "arm64" when this x64 process runs under emulation on an ARM64 Windows machine
// (IsWow64Process2 reports an ARM64 native machine), and "" otherwise or when the call is unavailable (Windows
// before 10 1511), in which case the binary's own architecture is used.
func detectEmulatedHostArch() string {
	if runtime.GOARCH != "amd64" {
		return ""
	}
	var process, native uint16
	if err := windows.IsWow64Process2(windows.CurrentProcess(), &process, &native); err != nil {
		return ""
	}
	if native == imageFileMachineARM64 {
		return "arm64"
	}
	return ""
}
