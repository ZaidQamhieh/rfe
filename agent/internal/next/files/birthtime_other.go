//go:build !linux && !windows && !darwin

package files

import (
	"io/fs"
	"time"
)

// birthTime has no creation time to read on this platform, so it returns the
// modification time.
func birthTime(info fs.FileInfo) time.Time {
	return info.ModTime()
}
