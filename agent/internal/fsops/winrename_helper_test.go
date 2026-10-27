package fsops

import (
	"errors"
	"runtime"
	"syscall"
)

// windowsHandleBlocksRename reports whether err is Windows refusing to rename
// a folder because a handle inside it is open (ERROR_ACCESS_DENIED or
// ERROR_SHARING_VIOLATION). On Windows that refusal is what stops a swap of an
// opened folder, so tests that need the swap check that outcome instead.
func windowsHandleBlocksRename(err error) bool {
	var errno syscall.Errno
	return runtime.GOOS == "windows" && errors.As(err, &errno) && (errno == 5 || errno == 32)
}
