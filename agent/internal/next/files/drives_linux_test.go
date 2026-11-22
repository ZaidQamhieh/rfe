//go:build linux

package files

import (
	"strings"
	"testing"
)

func TestRealMountPoints(t *testing.T) {
	table := strings.Join([]string{
		"/dev/sda1 / ext4 rw 0 0",
		"proc /proc proc rw 0 0",
		"tmpfs /run tmpfs rw 0 0",
		"/dev/sdb1 /mnt/with\\040space ext4 rw 0 0",
		"/dev/sdb1 /mnt/with\\040space ext4 rw 0 0",
		"/dev/sdc1 /data xfs rw 0 0",
		"short line",
	}, "\n")
	got := strings.Join(realMountPoints(strings.NewReader(table)), "|")
	if got != "/mnt/with space|/data" {
		t.Errorf("mount points = %q", got)
	}
}

func TestPlatformDrivesHasTheRootAsOS(t *testing.T) {
	all, err := platformDrives()
	if err != nil || len(all) == 0 || all[0].Path != "/" || !all[0].IsOS || all[0].TotalBytes <= 0 {
		t.Errorf("platformDrives = %+v, %v", all, err)
	}
}
