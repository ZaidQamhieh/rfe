package hostupdate

import "runtime"

// NativePlatform describes the machine the running binary belongs to: runtime.GOOS and GOARCH, plus, on Windows,
// whether an x64 binary is being emulated on an ARM64 machine (see detectEmulatedHostArch).
func NativePlatform() Platform {
	return Platform{GOOS: runtime.GOOS, GOARCH: runtime.GOARCH, EmulatedHostArch: detectEmulatedHostArch()}
}
