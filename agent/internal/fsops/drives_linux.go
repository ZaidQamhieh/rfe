//go:build linux

package fsops

import (
	"bufio"
	"os"
	"strings"
	"syscall"
)

func platformDrives() ([]Drive, error) {
	drives := []Drive{}

	// Always include root. The root mount point is the OS drive on Linux.
	root := statDrive("/", "/")
	root.IsOS = true
	drives = append(drives, root)

	// Parse /proc/mounts for additional interesting mount points.
	f, err := os.Open("/proc/mounts")
	if err != nil {
		return drives, nil
	}
	defer f.Close()

	seen := map[string]bool{"/": true}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		mountPoint := fields[1]
		fsType := fields[2]
		// Skip virtual / proc / sys filesystems and duplicates.
		if seen[mountPoint] {
			continue
		}
		switch fsType {
		case "proc", "sysfs", "devtmpfs", "devpts", "tmpfs",
			"cgroup", "cgroup2", "pstore", "bpf", "tracefs",
			"securityfs", "debugfs", "mqueue", "hugetlbfs",
			"fusectl", "efivarfs", "overlay", "squashfs":
			continue
		}
		seen[mountPoint] = true
		drives = append(drives, statDrive(mountPoint, mountPoint))
	}
	return drives, nil
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
