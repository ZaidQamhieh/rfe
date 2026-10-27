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

// NoAtomicRenameMessage is the fixed text sent for ErrNoAtomicRename. It never
// carries OS detail or paths (PR-53).
const NoAtomicRenameMessage = "this file system cannot rename without replacing; nothing was changed"
