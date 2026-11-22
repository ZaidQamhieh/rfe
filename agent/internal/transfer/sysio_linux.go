package transfer

import (
	"errors"
	"os"
	"syscall"
)

// reserveSpaceFile allocates size bytes of disk for f with fallocate and sets its length, so a full disk fails
// the open instead of a later chunk. A file system without fallocate gets the sparse Truncate it always had.
func reserveSpaceFile(f *os.File, size int64) error {
	if size <= 0 {
		return nil
	}
	for {
		err := syscall.Fallocate(int(f.Fd()), 0, 0, size)
		switch {
		case err == nil:
			return nil
		case errors.Is(err, syscall.EINTR):
			continue
		case errors.Is(err, syscall.EOPNOTSUPP), errors.Is(err, syscall.ENOSYS), errors.Is(err, syscall.EINVAL):
			return f.Truncate(size)
		}
		return err
	}
}

// syncDataFile flushes f's data (and the metadata needed to read it back) to stable storage with fdatasync.
func syncDataFile(f *os.File) error {
	for {
		err := syscall.Fdatasync(int(f.Fd()))
		if !errors.Is(err, syscall.EINTR) {
			return err
		}
	}
}
