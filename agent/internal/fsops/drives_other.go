//go:build !linux && !windows

package fsops

func platformDrives() ([]Drive, error) {
	return []Drive{statDrive("/", "/")}, nil
}
