//go:build !windows

package system

import "golang.org/x/sys/unix"

// statDisk returns the bytes available to an unprivileged user and the total
// size of the filesystem holding path.
func statDisk(path string) (free, total uint64, err error) {
	var st unix.Statfs_t
	if err = unix.Statfs(path, &st); err != nil {
		return 0, 0, err
	}
	bsize := uint64(st.Bsize)
	return st.Bavail * bsize, st.Blocks * bsize, nil
}
