//go:build linux

package rootfs

import (
	"os"
	"path/filepath"
	"runtime"

	"golang.org/x/sys/unix"
)

// renameNoReplace renames src to dst inside root with the kernel's atomic
// no-replace rename. Both parent folders are opened through root (so no
// symlink can lead out of it) and only their last names go to the kernel. A
// file system without the primitive falls back to the generic version.
func renameNoReplace(root *os.Root, src, dst string) error {
	srcDir, err := root.Open(filepath.Dir(src))
	if err != nil {
		return err
	}
	defer srcDir.Close()
	dstDir, err := root.Open(filepath.Dir(dst))
	if err != nil {
		return err
	}
	defer dstDir.Close()

	err = unix.Renameat2(int(srcDir.Fd()), filepath.Base(src), int(dstDir.Fd()), filepath.Base(dst), unix.RENAME_NOREPLACE)
	runtime.KeepAlive(srcDir)
	runtime.KeepAlive(dstDir)
	switch err {
	case nil:
		return nil
	case unix.EINVAL, unix.ENOSYS:
		// NFS, some FUSE mounts and old kernels have no RENAME_NOREPLACE.
		return renameNoReplaceGeneric(root, src, dst)
	}
	return &os.LinkError{Op: "renameat2", Old: src, New: dst, Err: err}
}
