//go:build linux

package files

import (
	"bufio"
	"io"
	"os"
	"strings"
)

// Mount table file and the first drive, the root mount, which holds the OS.
const (
	procMounts     = "/proc/mounts"
	linuxRootMount = "/"
	// minMountFields is the least a mount line has: device, mount point, type.
	minMountFields = 3
)

// virtualFS names file system types that are not drives (kernel interfaces,
// memory and overlay file systems).
var virtualFS = map[string]bool{
	"proc": true, "sysfs": true, "devtmpfs": true, "devpts": true, "tmpfs": true,
	"cgroup": true, "cgroup2": true, "pstore": true, "bpf": true, "tracefs": true,
	"securityfs": true, "debugfs": true, "mqueue": true, "hugetlbfs": true,
	"fusectl": true, "efivarfs": true, "overlay": true, "squashfs": true,
}

// mountEscapes decodes the octal escapes the kernel writes in mount points
// (a space is "\040").
var mountEscapes = strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`)

// platformDrives lists the root mount and every real file system mounted
// elsewhere, as read from the kernel's mount table.
func platformDrives() ([]Drive, error) {
	root := statDrive(linuxRootMount, linuxRootMount)
	root.IsOS = true
	drives := []Drive{root}
	f, err := os.Open(procMounts)
	if err != nil {
		return drives, nil // no mount table: the root is all we know
	}
	defer f.Close()
	for _, point := range realMountPoints(f) {
		drives = append(drives, statDrive(point, point))
	}
	return drives, nil
}

// realMountPoints returns the mount points of r (the format of /proc/mounts)
// that are real file systems, once each, leaving out the root mount.
func realMountPoints(r io.Reader) []string {
	seen := map[string]bool{linuxRootMount: true}
	var out []string
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < minMountFields {
			continue
		}
		point := mountEscapes.Replace(fields[1])
		if seen[point] || virtualFS[fields[2]] {
			continue
		}
		seen[point] = true
		out = append(out, point)
	}
	return out
}
