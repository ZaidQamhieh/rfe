//go:build unix && !linux && !darwin && !freebsd && !netbsd

package thumbs

import "syscall"

// ctimeNanos has no portable source on this OS, so the key falls back to device and inode alone.
func ctimeNanos(*syscall.Stat_t) int64 { return 0 }
