//go:build windows

package hostupdate

import (
	"errors"
	"syscall"
)

// Windows error numbers that mean another process (an indexer, an antivirus scan) briefly holds a file.
const (
	errorAccessDenied   syscall.Errno = 5
	errorSharingViolatn syscall.Errno = 32
	errorLockViolation  syscall.Errno = 33
)

// defaultRenameRetryable reports whether a rename failed because of a transient Windows file lock.
func defaultRenameRetryable(err error) bool {
	var en syscall.Errno
	if !errors.As(err, &en) {
		return false
	}
	return en == errorAccessDenied || en == errorSharingViolatn || en == errorLockViolation
}
