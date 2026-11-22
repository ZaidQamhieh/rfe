package fsops

import (
	"errors"
	"fmt"
)

// ErrNoAtomicRename is returned when the file system has no atomic no-replace
// rename and no safe substitute either, so nothing was renamed or replaced.
// It matches errors.ErrUnsupported and is never fs.ErrExist: a caller must not
// read it as "destination exists". The API maps exactly this sentinel to
// NOT_IMPLEMENTED; a raw ENOTSUP/ENOSYS from any other operation also matches
// errors.ErrUnsupported and must not be mislabelled.
var ErrNoAtomicRename = fmt.Errorf("atomic no-replace rename is not supported on this file system: %w", errors.ErrUnsupported)

// nothingChanged is the clause that says a failed rename left the tree as it
// was. It is the only part of NoAtomicRenameMessage a note can make false.
const nothingChanged = "nothing was changed"

// NothingChangedSuffix is the clause NoAtomicRenameMessage ends with. batchMessage
// strips it when a replaced entry is left behind, so the message stops claiming
// nothing changed.
const NothingChangedSuffix = "; " + nothingChanged

// noAtomicRenamePrefix is the first part of NoAtomicRenameMessage.
const noAtomicRenamePrefix = "this file system cannot rename without replacing"

// NoAtomicRenameMessage is the fixed text sent for ErrNoAtomicRename. It never
// carries OS detail or paths.
const NoAtomicRenameMessage = noAtomicRenamePrefix + NothingChangedSuffix
