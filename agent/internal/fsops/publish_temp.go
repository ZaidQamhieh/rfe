package fsops

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// PublishTempPrefix starts the name of the hidden file a publish copies the verified upload into before
// it takes its final name. Names with this prefix are hidden from listings and search for every device.
const PublishTempPrefix = ".rfe-upload-"

// ErrPublishTempRefused means a publish temp was not touched because it is not what the agent recorded: a
// name that is not a bare publish-temp name, a folder or file type that is not a plain directory and
// regular file, or a folder that is no longer reached without a symlink.
var ErrPublishTempRefused = errors.New("publish temp refused")

// IsPublishTemp reports whether name is a publish temp name (the strict prefix). It is the one predicate
// listings and searches use to hide in-flight uploads.
func IsPublishTemp(name string) bool {
	return strings.HasPrefix(name, PublishTempPrefix)
}

// PublishTempName is the deterministic temp name of an upload session: the prefix plus its id. The same
// session always uses the same name, so a retried publish replaces its own leftover instead of adding another.
func PublishTempName(sessionID string) string {
	return PublishTempPrefix + sessionID
}

// PublishOptions tunes PublishReaderWith. The zero value is a plain PublishReader (random temp name, no record).
type PublishOptions struct {
	// TempName, when set, is the temp file's name (it must satisfy IsPublishTemp and be one path element).
	// A regular file already at that name, left by an earlier attempt, is replaced.
	TempName string
	// Begin, when set (it needs TempName), runs before the temp is created with the resolved folder that
	// will hold it. If it fails nothing is written and the publish fails, so no temp ever exists unrecorded.
	Begin func(destDir, tempName string) error
	// End, when set, runs once the temp name no longer exists on disk: after the final rename or link, or
	// after a failed publish removed its temp. It does not run when removal failed or the process died.
	End func()
	// MoveFrom, when set, is the absolute path of a private regular file that holds the bytes being published
	// (the source reader must be that same file, opened). When the file is on the same filesystem as the
	// destination folder it is chmod-ed, synced and renamed (or linked, without overwrite) into place, so the
	// bytes are not copied and no temp is created in the destination folder; Begin and TempName are not used
	// then. Any other case (another filesystem, an OS without the primitive, a source that is not that file)
	// takes the ordinary copy path.
	MoveFrom string
}

// createNamedTemp creates the exclusive, private (0600) temp file named base next to p. A regular file
// already at that name is removed first; anything else there (a symlink, a folder) is refused, and
// O_EXCL means a name racing back into place makes the create fail instead of being written through.
func (p *securePath) createNamedTemp(base string) (*securePath, *os.File, error) {
	tmp := &securePath{full: filepath.Join(filepath.Dir(p.full), base), root: p.root}
	if p.root != nil {
		tmp.name = filepath.Join(filepath.Dir(p.name), base)
	}
	switch fi, err := tmp.lstat(); {
	case err == nil:
		if !fi.Mode().IsRegular() {
			return nil, nil, fmt.Errorf("%w: the temp name is taken by something that is not a regular file", ErrPublishTempRefused)
		}
		if err := tmp.remove(); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, nil, err
		}
	case !errors.Is(err, fs.ErrNotExist):
		return nil, nil, err
	}
	f, err := tmp.openFile(os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, nil, err
	}
	return tmp, f, nil
}

// RemovePublishTemp deletes the one regular file tempName inside destDir, and nothing else. destDir must be
// an absolute folder that is still reached without a symlink anywhere on its path (the agent recorded the
// symlink-resolved folder, so a mismatch means it was swapped); the file is then removed through a handle
// pinned to that folder, so a symlink put at the name or in the path cannot redirect the delete. A missing
// folder or file is success. Anything else is refused with ErrPublishTempRefused and left in place.
func RemovePublishTemp(destDir, tempName string) error {
	if !IsPublishTemp(tempName) || strings.ContainsAny(tempName, "/\\*?[") || filepath.Base(tempName) != tempName {
		return fmt.Errorf("%w: not a bare publish temp name", ErrPublishTempRefused)
	}
	if !filepath.IsAbs(destDir) {
		return fmt.Errorf("%w: the folder is not an absolute path", ErrPublishTempRefused)
	}
	clean := filepath.Clean(destDir)
	before, err := os.Lstat(clean)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !before.IsDir() {
		return fmt.Errorf("%w: the folder is no longer a plain directory", ErrPublishTempRefused)
	}
	real, err := filepath.EvalSymlinks(clean)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if real != clean && !(runtime.GOOS == "windows" && strings.EqualFold(real, clean)) {
		return fmt.Errorf("%w: the folder is now reached through a symlink", ErrPublishTempRefused)
	}
	root, err := os.OpenRoot(clean)
	if err != nil {
		return err
	}
	defer root.Close()
	if after, err := root.Stat("."); err != nil || !os.SameFile(before, after) {
		return fmt.Errorf("%w: the folder changed while it was opened", ErrPublishTempRefused)
	}
	fi, err := root.Lstat(tempName)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !fi.Mode().IsRegular() {
		return fmt.Errorf("%w: the temp name is not a regular file", ErrPublishTempRefused)
	}
	if err := root.Remove(tempName); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}
