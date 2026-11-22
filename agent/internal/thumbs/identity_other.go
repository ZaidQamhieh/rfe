//go:build !unix

package thumbs

import "os"

// fileIdentity has no device, inode or change time here (Windows keeps the path, mtime and size key it always had).
func fileIdentity(info os.FileInfo) string { return "" }
