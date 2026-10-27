//go:build unix

package server

import (
	"time"

	"golang.org/x/sys/unix"
)

// setLinkTime stamps a symlink itself (not its target) so recents have no ties.
func setLinkTime(path string, t time.Time) error {
	ts := []unix.Timespec{unix.NsecToTimespec(t.UnixNano()), unix.NsecToTimespec(t.UnixNano())}
	return unix.UtimesNanoAt(unix.AT_FDCWD, path, ts, unix.AT_SYMLINK_NOFOLLOW)
}
