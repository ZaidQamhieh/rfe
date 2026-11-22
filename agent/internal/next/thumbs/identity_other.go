//go:build !unix

package thumbs

import "io/fs"

// fileIdentity has no device/inode to use here; the path, size and
// modification time identify the version.
func fileIdentity(info fs.FileInfo) string { return "" }
