package server

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/ZaidQamhieh/rfe/agent/internal/fsops"
)

// liveWalkRoots is the root set for a live walk when the request names no root:
// the jail's roots, or the user's home directory when no jail is configured.
// A deny-all jail also has no roots, but must not gain access through the
// home fallback.
func liveWalkRoots(ops *fsops.Ops) []string {
	roots := ops.Roots()
	if len(roots) == 0 && !ops.IsDenyAll() {
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			roots = []string{home}
		}
	}
	return roots
}

// walkLiveRoot opens rootPath through ops and walks it, calling visit for every
// entry that is not pruned (the root itself is relPath "."). Hidden, virtual and
// dependency-cache directories are pruned, in-flight upload temps (fsops.IsPublishTemp) are skipped, permission-denied and other walk
// errors are skipped silently, and the walk stops when ctx is done. visit's
// error is returned to fs.WalkDir unchanged (fs.SkipAll ends the walk early).
func walkLiveRoot(ctx context.Context, ops *fsops.Ops, rootPath string, visit func(root *os.Root, relPath string, d fs.DirEntry) error) {
	root, err := ops.OpenDir(rootPath)
	if err != nil {
		return
	}
	defer root.Close()
	_ = fs.WalkDir(root.FS(), ".", func(relPath string, d fs.DirEntry, err error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			// Permission-denied and other errors (e.g. transient stat failures): skip this entry.
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() && relPath != "." && shouldSkipVirtualDir(filepath.Join(rootPath, filepath.FromSlash(relPath))) {
			return fs.SkipDir
		}
		// An in-flight upload's hidden temp is never listed or indexed, whatever its device's access.
		if relPath != "." && fsops.IsPublishTemp(d.Name()) {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		return visit(root, relPath, d)
	})
}
