//go:build !windows

package server

import (
	"os"
	"syscall"
)

// fileInode returns the inode number of the file info describes, or 0 when the platform does not provide it.
func fileInode(info os.FileInfo) uint64 {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return uint64(st.Ino)
	}
	return 0
}
