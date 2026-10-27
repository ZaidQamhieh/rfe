//go:build linux

package fsops

import (
	"os"
	"syscall"
	"time"
)

// birthTime returns a best-effort creation time on Unix. True birth time is not
// reliably exposed via syscall.Stat_t on Linux, so we use Ctim (status-change
// time) as a proxy, falling back to ModTime.
func birthTime(info os.FileInfo) time.Time {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return time.Unix(st.Ctim.Sec, st.Ctim.Nsec)
	}
	return info.ModTime()
}
