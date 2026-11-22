//go:build !windows

package transfer

import "os"

// syncDir flushes a directory's entries to stable storage, so a file created
// in it survives a crash.
func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	if err := d.Sync(); err != nil {
		_ = d.Close()
		return err
	}
	return d.Close()
}
