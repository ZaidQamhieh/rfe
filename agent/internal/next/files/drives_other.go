//go:build !linux && !windows

package files

// otherRootMount is the only drive reported where the mount table is not read.
const otherRootMount = "/"

// platformDrives reports just the root volume.
func platformDrives() ([]Drive, error) {
	root := statDrive(otherRootMount, otherRootMount)
	root.IsOS = true
	return []Drive{root}, nil
}
