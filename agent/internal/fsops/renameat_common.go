package fsops

import "fmt"

// renameNoReplaceTo renames p to dst and fails with an error that matches
// fs.ErrExist, instead of replacing the target, when dst exists or appears
// concurrently. Like renameTo it only renames inside one jail root, or between
// two unjailed paths. Whether the check is atomic depends on renameAcrossRoots.
func (p *securePath) renameNoReplaceTo(dst *securePath) error {
	if (p.root != nil && p.root == dst.root) || (p.root == nil && dst.root == nil) {
		return renameAcrossRoots(p, dst)
	}
	return fmt.Errorf("cannot atomically rename across filesystem jail roots")
}
