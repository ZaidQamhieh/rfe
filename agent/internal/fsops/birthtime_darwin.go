//go:build darwin

package fsops

import (
	"os"
	"syscall"
	"time"
)

// birthTime returns the real file creation time on macOS via Stat_t.Birthtimespec.
func birthTime(info os.FileInfo) time.Time {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return time.Unix(st.Birthtimespec.Sec, st.Birthtimespec.Nsec)
	}
	return info.ModTime()
}
