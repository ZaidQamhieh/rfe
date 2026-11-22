package rootfs

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// maxSymlinkHops bounds symlink expansion in resolvePhysical, like the
// kernel's own limit, so a link loop cannot spin forever.
const maxSymlinkHops = 255

const (
	pathSep = string(filepath.Separator)
	curDir  = "."
	parDir  = ".."
	// uncPrefix starts the volume name of a Windows network or device path.
	uncPrefix = `\\`
)

// cleanAbs validates and cleans a caller path: it must be absolute, free of
// NUL bytes and not a Windows network path. The path is not touched on disk.
func cleanAbs(p string) (string, error) {
	if p == "" || strings.IndexByte(p, 0) >= 0 || !filepath.IsAbs(p) {
		return "", fmt.Errorf("%w: %q", ErrInvalidPath, p)
	}
	clean := filepath.Clean(p)
	if isUNCVolume(filepath.VolumeName(clean)) {
		return "", fmt.Errorf("%w: network path", ErrOutsideRoots)
	}
	return clean, nil
}

// isUNCVolume reports whether a volume name (the part filepath.VolumeName
// returns) is a Windows network share or device namespace (\\host\share,
// \\?\, \\.\). Other platforms never return such a volume name.
func isUNCVolume(vol string) bool {
	return strings.HasPrefix(vol, uncPrefix)
}

// samePath compares two paths, ignoring case on Windows.
func samePath(a, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// underRel reports whether p is base or inside it, and if so returns p
// relative to base ("." for base itself). Both must already be clean.
func underRel(p, base string) (string, bool) {
	if samePath(p, base) {
		return curDir, true
	}
	prefix := base
	if !strings.HasSuffix(prefix, pathSep) {
		prefix += pathSep
	}
	if len(p) > len(prefix) && samePath(p[:len(prefix)], prefix) {
		return p[len(prefix):], true
	}
	return "", false
}

// isUnder reports whether p is base or inside it.
func isUnder(p, base string) bool {
	_, ok := underRel(p, base)
	return ok
}

// splitComponents splits a path (without its volume) into its non-empty
// elements.
func splitComponents(p string) []string {
	return strings.FieldsFunc(p, func(r rune) bool { return r == '/' || r == filepath.Separator })
}

// resolvePhysical returns where a clean absolute path really lives: every
// symlink on the way, including dangling ones, is expanded by hand, and the
// part of the path that does not exist yet is appended unchanged. It is used
// only to decide whether a path reaches a protected folder; the actual
// operation still goes through os.Root. With followLast false a symlink in the
// final element is kept as the entry itself rather than expanded.
func resolvePhysical(clean string, followLast bool) string {
	vol := filepath.VolumeName(clean)
	cur := vol + pathSep
	pending := splitComponents(clean[len(vol):])
	hops := 0
	missing := false
	for len(pending) > 0 {
		comp := pending[0]
		pending = pending[1:]
		last := len(pending) == 0
		switch comp {
		case curDir:
			continue
		case parDir:
			cur = filepath.Dir(cur)
			continue
		}
		next := filepath.Join(cur, comp)
		if missing || (last && !followLast) {
			cur = next
			continue
		}
		info, err := os.Lstat(next)
		if err != nil {
			missing = true
			cur = next
			continue
		}
		if info.Mode()&fs.ModeSymlink == 0 {
			cur = next
			continue
		}
		target, err := os.Readlink(next)
		hops++
		if err != nil || hops > maxSymlinkHops {
			cur = next
			missing = true
			continue
		}
		targetVol := filepath.VolumeName(target)
		if filepath.IsAbs(target) || strings.HasPrefix(target[len(targetVol):], pathSep) {
			if targetVol == "" {
				targetVol = filepath.VolumeName(cur)
			}
			cur = targetVol + pathSep
		}
		pending = append(splitComponents(target[len(targetVol):]), pending...)
	}
	return cur
}
