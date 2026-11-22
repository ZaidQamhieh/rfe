package backup

import (
	"errors"
	"io/fs"
)

// Newest returns the path of the most recent archive in dir whose label is label (by the time in its file name),
// or an error wrapping fs.ErrNotExist when there is none or the directory is missing.
func Newest(dir, label string) (string, error) {
	files, err := listArchives(dir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	best := -1
	for i, f := range files {
		if f.label == label && (best < 0 || f.taken.After(files[best].taken)) {
			best = i
		}
	}
	if best < 0 {
		return "", &fs.PathError{Op: "find " + label + " backup", Path: dir, Err: fs.ErrNotExist}
	}
	return files[best].path, nil
}
