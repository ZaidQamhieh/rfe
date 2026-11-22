// Package files is the file service of the rewritten agent: listing, metadata,
// checksums, creating and writing files, rename, copy, move, permanent delete,
// compress and extract, the trash and the drive list. It works only through a rootfs.Set, so every path is checked against
// the allowed folders, the protected data folder and the read-only flag by the
// layer below, and no operation can follow a symlink out of a root.
//
// Every failure is returned as an error that matches one of the domain errors
// of package api (or authz.ErrReadOnly), so the api layer can map it to a
// status and code. The text of such an error never carries a host path; the OS
// error behind it stays reachable through errors.Is and errors.As for logging.
package files

import (
	"errors"
	"fmt"
	"io/fs"
	"syscall"

	"github.com/ZaidQamhieh/rfe/agent/internal/next/api"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/authz"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/rootfs"
)

// ErrSymlinkCopy is returned when a copy, a cross-device move or a trash move
// meets a symbolic link it cannot recreate safely: the link is recreated only
// when its text is relative and its target stays inside the allowed folders
// (rootfs.Handle.Symlink), and it is never followed, which could copy data from
// outside them. The whole item fails and nothing is left behind.
var ErrSymlinkCopy = fmt.Errorf("%w: symbolic links inside copied folders", api.ErrNotImplemented)

// ErrMoveIncomplete is returned when a move that had to copy (across devices
// or allowed folders) finished the copy but could not remove the source: both
// the source and the destination exist.
var ErrMoveIncomplete = fmt.Errorf("%w: copied, but the source could not be removed", api.ErrConflict)

// knownKinds are the errors classify leaves untouched: the error already
// carries a status the api layer understands.
var knownKinds = []error{
	api.ErrForbidden, authz.ErrReadOnly, api.ErrBadRequest, api.ErrPathNotFound,
	api.ErrNotAFile, api.ErrConflict, api.ErrStale, api.ErrNotImplemented,
}

// opError pairs a domain error (kind) with the error that caused it. Its text
// is the kind's alone, so it is safe to show a client; errors.Is matches both.
type opError struct {
	kind  error
	cause error
}

func (e *opError) Error() string   { return e.kind.Error() }
func (e *opError) Unwrap() []error { return []error{e.kind, e.cause} }

// classify turns a rootfs or OS error into one that matches a domain error. An
// error that already matches one, or that has no domain meaning (an unexpected
// failure), is returned unchanged: the api layer reports the latter as internal.
func classify(err error) error {
	if err == nil {
		return nil
	}
	for _, k := range knownKinds {
		if errors.Is(err, k) {
			return err
		}
	}
	var kind error
	switch {
	case errors.Is(err, rootfs.ErrOutsideRoots), errors.Is(err, rootfs.ErrProtected), errors.Is(err, rootfs.ErrRootTarget):
		kind = api.ErrForbidden
	case errors.Is(err, rootfs.ErrReadOnly):
		kind = authz.ErrReadOnly
	case errors.Is(err, rootfs.ErrInvalidPath):
		kind = api.ErrBadRequest
	case errors.Is(err, rootfs.ErrNotRegular):
		kind = api.ErrNotAFile
	case isMissing(err):
		kind = api.ErrPathNotFound
	case errors.Is(err, fs.ErrExist):
		kind = api.ErrConflict
	default:
		return err
	}
	return &opError{kind: kind, cause: err}
}

// isMissing reports whether err means "no such path": the name is absent, or a
// parent of it is a regular file (which the OS reports as ENOTDIR).
func isMissing(err error) bool {
	return errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR)
}

// withCause records extra (a failed cleanup) behind err without changing the
// text or the domain kind of err.
func withCause(err, extra error) error {
	var oe *opError
	if errors.As(err, &oe) {
		return &opError{kind: oe.kind, cause: errors.Join(oe.cause, extra)}
	}
	return errors.Join(err, extra)
}
