package transfer

import (
	"fmt"
	"os"
	"path/filepath"
)

// applyDefaultFileMode gives path the mode a plain new file gets from this process's umask (0666 minus the
// mask). The umask cannot be read without changing it for the whole process, so it creates and removes a probe
// file in the manager's private temp folder and copies that file's mode.
func (m *Manager) applyDefaultFileMode(path string) error {
	probe := filepath.Join(m.tempDir, fmt.Sprintf(".mode-probe-%d", os.Getpid()))
	_ = os.Remove(probe)
	f, err := os.OpenFile(probe, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o666)
	if err != nil {
		return err
	}
	defer os.Remove(probe)
	info, err := f.Stat()
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	return os.Chmod(path, info.Mode().Perm())
}
