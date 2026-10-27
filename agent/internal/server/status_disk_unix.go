//go:build !windows

package server

import "golang.org/x/sys/unix"

func diskStats(path string) (free, total uint64, err error) {
	var st unix.Statfs_t
	if err = unix.Statfs(path, &st); err != nil {
		return
	}
	return st.Bavail * uint64(st.Bsize), st.Blocks * uint64(st.Bsize), nil
}
