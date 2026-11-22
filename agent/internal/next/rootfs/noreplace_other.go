//go:build !linux

package rootfs

import "os"

// renameNoReplace has no atomic kernel primitive on this platform, so it uses
// the portable check-then-rename.
func renameNoReplace(root *os.Root, src, dst string) error {
	return renameNoReplaceGeneric(root, src, dst)
}
