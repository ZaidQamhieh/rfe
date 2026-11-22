//go:build windows

package files

import (
	"io/fs"
	"syscall"
	"time"
)

// birthTime returns the real creation time, which Windows exposes in
// Win32FileAttributeData, falling back to the modification time.
func birthTime(info fs.FileInfo) time.Time {
	if d, ok := info.Sys().(*syscall.Win32FileAttributeData); ok {
		return time.Unix(0, d.CreationTime.Nanoseconds())
	}
	return info.ModTime()
}
