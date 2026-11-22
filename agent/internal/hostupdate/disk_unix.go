//go:build linux || darwin || freebsd || netbsd || openbsd || dragonfly

package hostupdate

import "golang.org/x/sys/unix"

// FreeSpace returns the bytes a non-root user can still write on the filesystem holding dir (statfs: available
// blocks times block size).
func FreeSpace(dir string) (uint64, error) {
	var st unix.Statfs_t
	if err := unix.Statfs(dir, &st); err != nil {
		return 0, err
	}
	return uint64(st.Bavail) * uint64(st.Bsize), nil // field types differ per platform
}
