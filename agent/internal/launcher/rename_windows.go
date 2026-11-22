package launcher

import (
	"errors"
	"os"
	"syscall"
	"time"
)

// renameReplace renames over an existing file. Windows refuses (access denied or a sharing violation) while another
// handle, such as a concurrent state read or a virus scanner, has the target open without share-delete, so a short
// retry rides out the brief window instead of failing a state write.
func renameReplace(from, to string) error {
	var err error
	for i := 0; i < 50; i++ {
		if err = os.Rename(from, to); err == nil {
			return nil
		}
		if !errors.Is(err, syscall.ERROR_ACCESS_DENIED) && !errors.Is(err, syscall.Errno(32)) {
			return err
		}
		time.Sleep(10 * time.Millisecond)
	}
	return err
}
