package launcher

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strconv"
	"strings"
	"time"
)

// ErrLocked means another live launcher holds the lock of this install root.
var ErrLocked = errors.New("another launcher is running for this root")

// lockGrace is how long a lock file with no readable pid is trusted: its creator may still be about to write it.
const lockGrace = 2 * time.Second

// Lock is a held single-instance lock: a file created with O_EXCL that holds the owner's pid.
type Lock struct{ path string }

// AcquireLock creates the lock file exclusively. If the file exists, its owner's pid is checked: a live owner
// means ErrLocked, a dead owner (or an unreadable file older than a couple of seconds) makes the lock stale, and
// it is removed and taken over. A reused pid can make a stale lock look live; the file is then refused until removed.
func AcquireLock(path string) (*Lock, error) {
	for attempt := 0; attempt < 3; attempt++ {
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			_, werr := f.WriteString(strconv.Itoa(os.Getpid()) + "\n")
			if werr == nil {
				werr = f.Sync()
			}
			if cerr := f.Close(); werr == nil {
				werr = cerr
			}
			if werr != nil {
				os.Remove(path)
				return nil, werr
			}
			return &Lock{path: path}, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return nil, err
		}
		if live, pid := lockOwnerAlive(path); live {
			return nil, fmt.Errorf("%w (pid %d, %s)", ErrLocked, pid, path)
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
	}
	return nil, fmt.Errorf("%w (%s kept reappearing)", ErrLocked, path)
}

// lockOwnerAlive reads the lock file and reports whether its owner may still be running.
func lockOwnerAlive(path string) (bool, int) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, 0
	}
	pid, perr := strconv.Atoi(strings.TrimSpace(string(data)))
	if perr != nil || pid <= 0 {
		if st, err := os.Stat(path); err == nil && time.Since(st.ModTime()) < lockGrace {
			return true, 0
		}
		return false, 0
	}
	return pidAlive(pid), pid
}

// Release removes the lock file.
func (l *Lock) Release() error {
	if l == nil {
		return nil
	}
	err := os.Remove(l.path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}
