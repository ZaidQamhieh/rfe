//go:build windows

package server

import "os"

// fileInode returns 0: Windows exposes no inode through os.FileInfo, so the ETag rests on size and mtime_ns there.
func fileInode(info os.FileInfo) uint64 { return 0 }
