//go:build darwin

package files

import (
	"io/fs"
	"syscall"
	"time"
)

// birthTime returns the real creation time macOS keeps in the stat record,
// falling back to the modification time.
func birthTime(info fs.FileInfo) time.Time {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return time.Unix(st.Birthtimespec.Sec, st.Birthtimespec.Nsec)
	}
	return info.ModTime()
}
