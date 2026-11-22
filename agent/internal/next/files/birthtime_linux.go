//go:build linux

package files

import (
	"io/fs"
	"syscall"
	"time"
)

// birthTime returns a best-effort creation time. Linux does not expose the
// real birth time through syscall.Stat_t, so the status-change time stands in,
// falling back to the modification time.
func birthTime(info fs.FileInfo) time.Time {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return time.Unix(st.Ctim.Sec, st.Ctim.Nsec)
	}
	return info.ModTime()
}
