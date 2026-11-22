//go:build !linux

package transfer

import "os"

// reserveSpaceFile sets f's length to size without reserving disk (no portable fallocate here).
func reserveSpaceFile(f *os.File, size int64) error {
	if size <= 0 {
		return nil
	}
	return f.Truncate(size)
}

// syncDataFile flushes f to stable storage.
func syncDataFile(f *os.File) error { return f.Sync() }
