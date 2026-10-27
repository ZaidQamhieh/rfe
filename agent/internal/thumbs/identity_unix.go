//go:build unix

package thumbs

import (
	"fmt"
	"os"
	"syscall"
)

// fileIdentity names the file itself (device and inode), so a replaced file or the same relative path
// under another jail never shares a cached thumbnail (audit F-54).
func fileIdentity(info os.FileInfo) string {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return fmt.Sprintf("%d:%d", uint64(st.Dev), uint64(st.Ino))
	}
	return ""
}
