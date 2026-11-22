//go:build !linux && !darwin

package fsops

import (
	"errors"
	"os"
)

// sameDevice is always false here, so a publish copies: the move needs descriptor-relative renames.
func sameDevice(a, b os.FileInfo) bool { return false }

func errUnsupportedMove(err error) bool { return errors.Is(err, errors.ErrUnsupported) }

func moveFileAcrossRoots(src, dst *securePath, overwrite bool) error { return errors.ErrUnsupported }
