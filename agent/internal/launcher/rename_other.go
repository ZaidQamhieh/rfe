//go:build !windows

package launcher

import "os"

func renameReplace(from, to string) error { return os.Rename(from, to) }
