//go:build !linux && !darwin

package fsops

import (
	"fmt"
	"path/filepath"
)

// checkJailParents is the pre-rename check shared by the platforms that rename
// by path (see renameAcrossRoots): a jailed side may not be the jail root
// itself, and its parent must still be its own canonical path.
func checkJailParents(src, dst *securePath) error {
	for _, side := range []*securePath{src, dst} {
		if side.root == nil {
			continue
		}
		if side.name == "." {
			return fmt.Errorf("cannot move the jail root")
		}
		parent := filepath.Dir(side.full)
		if real, rerr := filepath.EvalSymlinks(parent); rerr != nil || real != parent {
			return fmt.Errorf("path changed during the operation")
		}
	}
	return nil
}
