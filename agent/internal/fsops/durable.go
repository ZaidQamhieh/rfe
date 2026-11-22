package fsops

// syncDirFn makes the directory entries of p's folder durable (see durable_*.go). It is a variable so tests can
// observe the call; production code never replaces it.
var syncDirFn = syncDirOf

// SyncDir forces the entries of the folder dir (names created, renamed or removed in it) to stable storage, so a
// file published there is still present after a power cut. It is a no-op where the OS has no folder sync.
func SyncDir(dir string) error {
	return syncDirFn(&securePath{full: dir})
}

// syncParent makes the folder holding p durable, through p's own root handle when one is set.
func (p *securePath) syncParent() error {
	return syncDirFn(p.parent())
}
