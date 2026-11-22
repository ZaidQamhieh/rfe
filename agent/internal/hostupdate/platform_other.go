//go:build !windows

package hostupdate

// detectEmulatedHostArch is Windows-only (an x64 binary on ARM64); elsewhere the binary's own GOARCH is the answer.
func detectEmulatedHostArch() string { return "" }
