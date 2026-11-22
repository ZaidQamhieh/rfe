//go:build darwin || freebsd || netbsd

package thumbs

import "syscall"

// ctimeNanos returns the inode change time, which moves whenever the file's content or metadata is written,
// even if the writer restores the modification time.
func ctimeNanos(st *syscall.Stat_t) int64 { return st.Ctimespec.Nano() }
