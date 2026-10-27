//go:build windows

package fsops

import (
	"os"
	"syscall"
	"time"
)

// birthTime returns the real file creation time on Windows, which is exposed
// directly via Win32FileAttributeData.CreationTime.
func birthTime(info os.FileInfo) time.Time {
	if d, ok := info.Sys().(*syscall.Win32FileAttributeData); ok {
		return time.Unix(0, d.CreationTime.Nanoseconds())
	}
	return info.ModTime()
}
