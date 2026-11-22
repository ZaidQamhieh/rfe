//go:build !windows

package fsops

import (
	"errors"
	"syscall"
)

// syncDirOf opens the folder p and fsyncs it. A file system that cannot sync a folder (EINVAL or ENOTSUP, for
// example some network and FUSE mounts) is not an error: there is nothing more to ask of it.
func syncDirOf(p *securePath) error {
	d, err := p.open()
	if err != nil {
		return err
	}
	err = d.Sync()
	if cerr := d.Close(); err == nil {
		err = cerr
	}
	if errors.Is(err, syscall.EINVAL) || errors.Is(err, syscall.ENOTSUP) {
		return nil
	}
	return err
}
