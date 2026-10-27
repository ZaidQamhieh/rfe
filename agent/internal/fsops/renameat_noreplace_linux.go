//go:build linux

package fsops

import "golang.org/x/sys/unix"

// noReplacePrimitive is the kernel's atomic no-replace rename: it fails with
// EEXIST if the target exists or appeared (audit F-148). It is a variable only
// so tests can force the unsupported case.
var noReplacePrimitive = func(sfd int, sbase string, dfd int, dbase string) error {
	return unix.Renameat2(sfd, sbase, dfd, dbase, unix.RENAME_NOREPLACE)
}

// noReplaceUnsupported reports whether err means the file system or kernel has
// no RENAME_NOREPLACE (NFS, some FUSE, old kernels), as opposed to a real
// failure of the rename.
func noReplaceUnsupported(err error) bool {
	return err == unix.EINVAL || err == unix.ENOSYS
}
