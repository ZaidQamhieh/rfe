package fsops

// syncDirOf does nothing: Windows cannot fsync a folder handle, and NTFS journals the rename itself.
func syncDirOf(*securePath) error { return nil }
