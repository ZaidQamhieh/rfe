//go:build !(linux || darwin || freebsd || netbsd || openbsd || dragonfly || windows)

package hostupdate

import "errors"

// FreeSpace is not implemented on this platform; Stage then refuses to download unless Config.FreeSpace is set.
func FreeSpace(dir string) (uint64, error) {
	return 0, errors.New("free space is not available on this platform")
}
