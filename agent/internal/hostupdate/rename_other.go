//go:build !windows

package hostupdate

// defaultRenameRetryable is false off Windows: a failed rename on a POSIX filesystem is not a transient lock.
func defaultRenameRetryable(error) bool { return false }
