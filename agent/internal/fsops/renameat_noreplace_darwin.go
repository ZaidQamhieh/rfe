//go:build darwin

package fsops

import "golang.org/x/sys/unix"

// noReplacePrimitive is the atomic no-replace rename: it fails with EEXIST if
// the target exists or appeared (audit F-148). It is a variable only so tests
// can force the unsupported case.
var noReplacePrimitive = func(sfd int, sbase string, dfd int, dbase string) error {
	return unix.RenameatxNp(sfd, sbase, dfd, dbase, unix.RENAME_EXCL)
}

// noReplaceUnsupported reports whether err means the file system has no
// RENAME_EXCL, as opposed to a real failure of the rename.
func noReplaceUnsupported(err error) bool {
	return err == unix.ENOTSUP || err == unix.EINVAL
}
