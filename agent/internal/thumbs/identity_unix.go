//go:build unix

package thumbs

import (
	"fmt"
	"os"
	"syscall"
)

// fileIdentity names the file itself (device, inode and change time), so a replaced or rewritten file or the
// same relative path under another jail never shares a cached thumbnail (audit F-54). The change time catches a
// rewrite that keeps the size and restores the modification time.
func fileIdentity(info os.FileInfo) string {
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		return fmt.Sprintf("%d:%d:%d", uint64(st.Dev), uint64(st.Ino), ctimeNanos(st))
	}
	return ""
}
