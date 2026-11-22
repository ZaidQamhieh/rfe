//go:build windows

package transfer

// syncDir does nothing on Windows: a directory cannot be opened and flushed
// there, and NTFS journals directory entries itself.
func syncDir(string) error { return nil }
