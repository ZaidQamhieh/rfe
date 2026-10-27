//go:build !linux && !windows

package fsops

import "syscall"

func platformDrives() ([]Drive, error) {
	return []Drive{statDrive("/", "/")}, nil
}

func statDrive(path, label string) Drive {
	d := Drive{Path: path, Label: label}
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err == nil {
		d.TotalBytes = int64(st.Blocks) * int64(st.Bsize)
		d.FreeBytes = int64(st.Bfree) * int64(st.Bsize)
	}
	return d
}
