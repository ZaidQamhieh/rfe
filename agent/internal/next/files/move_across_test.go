package files

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/ZaidQamhieh/rfe/agent/internal/next/api"
	"github.com/ZaidQamhieh/rfe/agent/internal/next/rootfs"
)

// Names and contents of the small trees the cross-device move tests build.
const (
	moveTreeName      = "tree"
	moveMovedName     = "moved"
	moveFileA         = "a.txt"
	moveFileB         = "b.txt"
	moveSubDirName    = "sub"
	moveLockedDirName = "locked"
	moveUnreadable    = "z.txt"
	moveLockedFile    = "f.txt"
	moveUnreadBody    = "unreadable"
	moveContentA      = "A"
	moveContentB      = "B"
	moveContentF      = "F"
	moveContentOld    = "OLD"
	moveLockedMode    = 0o555
	moveOpenMode      = 0o755
	moveNoAccessMode  = 0o000
)

// needPOSIXPerms skips where permission bits do not stop the owner, which
// rules out Windows and root.
func needPOSIXPerms(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits")
	}
	if os.Geteuid() == 0 {
		t.Skip("root ignores permission bits")
	}
}

// unfreezeAtCleanup restores dir's mode when the test ends, so the temp dir can
// be removed. It is registered before the folder may exist (a copy made later
// inherits the frozen mode).
func unfreezeAtCleanup(t *testing.T, dir string) {
	t.Helper()
	t.Cleanup(func() { _ = os.Chmod(dir, moveOpenMode) })
}

// lockDir makes an existing dir read-only so its entries cannot be removed.
func lockDir(t *testing.T, dir string) {
	t.Helper()
	unfreezeAtCleanup(t, dir)
	if err := os.Chmod(dir, moveLockedMode); err != nil {
		t.Fatal(err)
	}
}

// openAt opens path through the service, failing the test on error.
func openAt(t *testing.T, svc *Service, path string) *rootfs.Handle {
	t.Helper()
	h, err := svc.open(path)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

// handleAt opens an existing path and returns its handle and own information.
func handleAt(t *testing.T, svc *Service, path string) (*rootfs.Handle, fs.FileInfo) {
	t.Helper()
	h := openAt(t, svc, path)
	info, err := h.Lstat()
	if err != nil {
		t.Fatal(err)
	}
	return h, info
}

// makeTree writes the tree root/tree: a.txt, sub/b.txt and locked/f.txt, the
// last in a folder that lockDir can freeze.
func makeTree(t *testing.T, root string) string {
	t.Helper()
	tree := filepath.Join(root, moveTreeName)
	write(t, filepath.Join(tree, moveFileA), moveContentA)
	write(t, filepath.Join(tree, moveSubDirName, moveFileB), moveContentB)
	write(t, filepath.Join(tree, moveLockedDirName, moveLockedFile), moveContentF)
	return tree
}

// checkWholeTree fails unless dir holds the tree made by makeTree.
func checkWholeTree(t *testing.T, dir string) {
	t.Helper()
	if got := read(t, filepath.Join(dir, moveFileA)); got != moveContentA {
		t.Errorf("a.txt = %q", got)
	}
	if got := read(t, filepath.Join(dir, moveSubDirName, moveFileB)); got != moveContentB {
		t.Errorf("sub/b.txt = %q", got)
	}
	if got := read(t, filepath.Join(dir, moveLockedDirName, moveLockedFile)); got != moveContentF {
		t.Errorf("locked/f.txt = %q", got)
	}
}

func TestMoveAcrossCopyFailsMidway(t *testing.T) {
	needPOSIXPerms(t)
	e := newEnv(t, false)
	src := makeTree(t, e.root)
	unreadable := filepath.Join(src, moveUnreadable)
	write(t, unreadable, moveUnreadBody)
	if err := os.Chmod(unreadable, moveNoAccessMode); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(e.other, moveMovedName)
	hs, info := handleAt(t, e.svc, src)
	hd := openAt(t, e.svc, dst)

	err := e.svc.moveAcross(hs, info, hd)
	if err == nil {
		t.Fatal("moveAcross succeeded with an unreadable file")
	}
	if errors.Is(err, ErrMoveIncomplete) {
		t.Errorf("copy failure reported as ErrMoveIncomplete: %v", err)
	}
	checkWholeTree(t, src)
	if !exists(unreadable) {
		t.Error("source file is gone")
	}
	if exists(dst) {
		t.Error("destination exists after a failed copy")
	}
	noHidden(t, e.other)
	noHidden(t, e.root)
}

func TestMoveAcrossDestinationExists(t *testing.T) {
	e := newEnv(t, false)
	src := makeTree(t, e.root)
	dst := write(t, filepath.Join(e.other, moveMovedName), moveContentOld)
	hs, info := handleAt(t, e.svc, src)
	hd := openAt(t, e.svc, dst)

	wantCode(t, e.svc.moveAcross(hs, info, hd), api.CodeConflict)
	checkWholeTree(t, src)
	if got := read(t, dst); got != moveContentOld {
		t.Errorf("existing destination changed to %q", got)
	}
	noHidden(t, e.other)
	noHidden(t, e.root)
}

func TestMoveAcrossSourceRemovalFails(t *testing.T) {
	needPOSIXPerms(t)
	e := newEnv(t, false)
	src := makeTree(t, e.root)
	lockDir(t, filepath.Join(src, moveLockedDirName))
	dst := filepath.Join(e.other, moveMovedName)
	unfreezeAtCleanup(t, filepath.Join(dst, moveLockedDirName))
	hs, info := handleAt(t, e.svc, src)
	hd := openAt(t, e.svc, dst)

	err := e.svc.moveAcross(hs, info, hd)
	if !errors.Is(err, ErrMoveIncomplete) {
		t.Fatalf("want ErrMoveIncomplete, got %v", err)
	}
	wantCode(t, err, api.CodeConflict)
	checkWholeTree(t, dst)
	if !exists(filepath.Join(src, moveLockedDirName, moveLockedFile)) {
		t.Error("source was partly removed but its locked entry is gone")
	}
	noHidden(t, e.other)
	noHidden(t, e.root)
}

func TestMoveAcrossSuccessLeavesOnlyDestination(t *testing.T) {
	e := newEnv(t, false)
	src := makeTree(t, e.root)
	dst := filepath.Join(e.other, moveMovedName)
	hs, info := handleAt(t, e.svc, src)
	hd := openAt(t, e.svc, dst)

	if err := e.svc.moveAcross(hs, info, hd); err != nil {
		t.Fatal(err)
	}
	checkWholeTree(t, dst)
	if exists(src) {
		t.Error("source still exists after a successful move")
	}
	if got := names(t, e.root); len(got) != 0 {
		t.Errorf("source folder not empty: %v", got)
	}
	if got := names(t, e.other); len(got) != 1 || got[0] != moveMovedName {
		t.Errorf("destination folder = %v, want only %s", got, moveMovedName)
	}
}

func TestMoveIntoTrashCopyFailsMidway(t *testing.T) {
	needPOSIXPerms(t)
	e, _ := trashEnv(t, false)
	src := makeTree(t, e.root)
	unreadable := filepath.Join(src, moveUnreadable)
	write(t, unreadable, moveUnreadBody)
	if err := os.Chmod(unreadable, moveNoAccessMode); err != nil {
		t.Fatal(err)
	}
	hs, info := handleAt(t, e.svc, src)
	id, infoH, err := e.svc.trash.reserveID(moveTreeName, hs.Path())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = infoH.Remove() }()

	err = e.svc.moveIntoTrash(e.svc.trash, hs, info, id)
	if err == nil || errors.Is(err, ErrMoveIncomplete) {
		t.Fatalf("want a copy failure, got %v", err)
	}
	checkWholeTree(t, src)
	if exists(e.svc.trash.filesPath(id)) {
		t.Error("trash payload exists after a failed copy")
	}
	noHidden(t, filepath.Join(e.svc.trash.dir, trashFilesDir))
}

func TestMoveIntoTrashSourceRemovalFails(t *testing.T) {
	needPOSIXPerms(t)
	e, _ := trashEnv(t, false)
	src := makeTree(t, e.root)
	lockDir(t, filepath.Join(src, moveLockedDirName))
	hs, info := handleAt(t, e.svc, src)
	id, infoH, err := e.svc.trash.reserveID(moveTreeName, hs.Path())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = infoH.Remove() }()
	// The copy inherits the locked mode; let cleanup reach it too.
	unfreezeAtCleanup(t, filepath.Join(e.svc.trash.filesPath(id), moveLockedDirName))

	err = e.svc.moveIntoTrash(e.svc.trash, hs, info, id)
	if !errors.Is(err, ErrMoveIncomplete) {
		t.Fatalf("want ErrMoveIncomplete, got %v", err)
	}
	checkWholeTree(t, e.svc.trash.filesPath(id))
	if !exists(filepath.Join(src, moveLockedDirName, moveLockedFile)) {
		t.Error("source was partly removed but its locked entry is gone")
	}
	noHidden(t, filepath.Join(e.svc.trash.dir, trashFilesDir))
}

func TestMoveIntoTrashSuccess(t *testing.T) {
	e, _ := trashEnv(t, false)
	src := makeTree(t, e.root)
	hs, info := handleAt(t, e.svc, src)
	id, infoH, err := e.svc.trash.reserveID(moveTreeName, hs.Path())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = infoH.Remove() }()

	if err := e.svc.moveIntoTrash(e.svc.trash, hs, info, id); err != nil {
		t.Fatal(err)
	}
	checkWholeTree(t, e.svc.trash.filesPath(id))
	if exists(src) {
		t.Error("source still exists after moving to the trash")
	}
	noHidden(t, filepath.Join(e.svc.trash.dir, trashFilesDir))
}

// trashTree trashes a fresh tree and returns its trash id (the name, as no
// other entry has it).
func trashTree(t *testing.T, e *env) string {
	t.Helper()
	src := makeTree(t, e.root)
	if res := e.svc.Trash([]string{src}); !res[0].OK {
		t.Fatalf("Trash = %+v", res[0])
	}
	return moveTreeName
}

func TestRestoreOneTrashCopyRemains(t *testing.T) {
	needPOSIXPerms(t)
	e, _ := trashEnv(t, false)
	id := trashTree(t, e)
	// Freeze a folder inside the trashed copy so its removal fails after the
	// restore has copied it.
	payloadLocked := filepath.Join(e.svc.trash.filesPath(id), moveLockedDirName)
	lockDir(t, payloadLocked)
	restored := filepath.Join(e.root, moveTreeName)
	unfreezeAtCleanup(t, filepath.Join(restored, moveLockedDirName))

	dest, err := e.svc.restoreOne(id)
	if !errors.Is(err, ErrMoveIncomplete) {
		t.Fatalf("want ErrMoveIncomplete, got dest=%q err=%v", dest, err)
	}
	checkWholeTree(t, restored)
	if !exists(filepath.Join(payloadLocked, moveLockedFile)) {
		t.Error("trash copy was removed although its removal should fail")
	}
	if !exists(e.svc.trash.infoPath(id)) {
		t.Error("trash record was removed before the restore finished")
	}
}

func TestRestoreOneTrashRecordRemains(t *testing.T) {
	needPOSIXPerms(t)
	e, dir := trashEnv(t, false)
	id := trashTree(t, e)
	// Freeze the info folder so the sidecar cannot be removed after restore.
	lockDir(t, filepath.Join(dir, trashInfoDir))

	dest, err := e.svc.restoreOne(id)
	if !errors.Is(err, ErrMoveIncomplete) {
		t.Fatalf("want ErrMoveIncomplete, got dest=%q err=%v", dest, err)
	}
	checkWholeTree(t, filepath.Join(e.root, moveTreeName))
	if exists(e.svc.trash.filesPath(id)) {
		t.Error("trash copy remains although its removal should succeed")
	}
	if !exists(e.svc.trash.infoPath(id)) {
		t.Error("trash record was removed although its folder is frozen")
	}
}
