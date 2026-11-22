//go:build !windows

package files

import "syscall"

// statDrive returns the drive at path with its size and free space (both 0 when
// the file system cannot be queried).
func statDrive(path, label string) Drive {
	d := Drive{Path: path, Label: label}
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err == nil {
		d.TotalBytes = int64(st.Blocks) * int64(st.Bsize)
		d.FreeBytes = int64(st.Bfree) * int64(st.Bsize)
	}
	return d
}
