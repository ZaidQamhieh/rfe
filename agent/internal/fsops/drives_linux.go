//go:build linux

package fsops

import (
	"bufio"
	"os"
	"strings"
)

// unescapeMountField decodes the octal escapes /proc/mounts uses for the
// characters that would break its space-separated fields: space (\040), tab
// (\011), newline (\012) and backslash (\134). Anything else is left as is.
func unescapeMountField(s string) string {
	if !strings.Contains(s, "\\") {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+4 <= len(s) {
			switch s[i+1 : i+4] {
			case "040":
				b.WriteByte(' ')
				i += 3
				continue
			case "011":
				b.WriteByte('\t')
				i += 3
				continue
			case "012":
				b.WriteByte('\n')
				i += 3
				continue
			case "134":
				b.WriteByte('\\')
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

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
		mountPoint := unescapeMountField(fields[1])
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
