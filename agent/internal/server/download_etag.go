package server

import (
	"fmt"
	"os"
)

// downloadETag returns a strong entity tag for the file version info describes: its size, its modification time
// in nanoseconds and (where the OS has one) its inode, in hex. Unlike Last-Modified (1 s granularity) it changes
// when a file is rewritten in place within the same second or replaced by another file of the same size and mtime.
// Nothing is hashed, so it is cheap. It stays the same only while all three are unchanged.
func downloadETag(info os.FileInfo) string {
	return fmt.Sprintf("\"%x-%x-%x\"", info.Size(), info.ModTime().UnixNano(), fileInode(info))
}
