// Package rootfs is the symlink-safe filesystem layer of the rewritten agent.
//
// A Set holds the folders a caller may reach. Every folder is resolved once
// (symlinks followed) when the Set is built and then kept open as an os.Root,
// so each later operation is performed relative to that handle and the kernel
// refuses to follow a symlink out of it. On top of that a Set can protect
// folders (the agent's own data directory) that no operation may touch, narrow
// itself to a sub-folder (a per-device jail) or turn read-only.
//
// Callers map the typed errors below to API answers; the package itself knows
// nothing about HTTP.
package rootfs

import (
	"errors"
	"io/fs"
	"os"
	"strings"
)

var (
	// ErrOutsideRoots: the path is not inside any folder the Set allows
	// (including paths that reach out of a root through a symlink or "..",
	// and Windows network paths). The API maps it to 403.
	ErrOutsideRoots = errors.New("rootfs: path is outside the allowed folders")

	// ErrInvalidPath: the path is relative, empty or contains a NUL byte.
	ErrInvalidPath = errors.New("rootfs: invalid path")

	// ErrProtected: the operation would touch a protected folder, either as
	// its target or because the target contains it.
	ErrProtected = errors.New("rootfs: path is protected")

	// ErrReadOnly: a changing operation was tried on a read-only Set.
	ErrReadOnly = errors.New("rootfs: file system is read-only")

	// ErrRootTarget: the operation would remove or move an allowed folder
	// itself rather than something inside it.
	ErrRootTarget = errors.New("rootfs: operation targets an allowed folder itself")

	// ErrCrossRoot: a rename between two different allowed folders cannot be
	// done atomically; the caller must copy and delete instead.
	ErrCrossRoot = errors.New("rootfs: rename crosses allowed folders")

	// ErrNotRegular: Open refuses anything that is neither a regular file nor
	// a folder (named pipe, socket, device), because reading it can block.
	ErrNotRegular = errors.New("rootfs: not a regular file or folder")
)

// escapeMessage is the text os.Root puts in the error it returns when a path
// or a symlink leaves the root. The error value is not exported by the
// standard library, so it is recognised by its text; a test pins this.
const escapeMessage = "path escapes from parent"

// Permission bits callers should pass to Mkdir, MkdirAll and OpenFile when
// they have no stricter need.
const (
	DirMode  fs.FileMode = 0o755
	FileMode fs.FileMode = 0o644
)

// writeFlags are the open flags that make OpenFile a changing operation.
const writeFlags = os.O_WRONLY | os.O_RDWR | os.O_CREATE | os.O_TRUNC | os.O_APPEND

// mapErr turns the os.Root "escapes from parent" error into one that also
// matches ErrOutsideRoots, keeping the original error for logging. Other
// errors pass through unchanged.
func mapErr(err error) error {
	if err != nil && !errors.Is(err, ErrOutsideRoots) && strings.Contains(err.Error(), escapeMessage) {
		return &multiError{typed: ErrOutsideRoots, cause: err}
	}
	return err
}

// multiError matches both a typed rootfs error and the OS error behind it.
type multiError struct {
	typed error
	cause error
}

func (e *multiError) Error() string   { return e.typed.Error() + ": " + e.cause.Error() }
func (e *multiError) Unwrap() []error { return []error{e.typed, e.cause} }
