package fsops

// Break tests (qa2) for the cross-device restore path. Written by qa-break; not
// run by it.
//
// Run: cd /home/zaid/Projects/rfe-rn/agent && go test ./internal/fsops/ -run TestQA2Break -count=1 -v -timeout 300s
//
// The interleaving is forced, not timed: renameAcrossRootsFn is replaced so that
// the restore destination gets an occupant after RestoreFromTrash has checked it
// free, and then returns EXDEV, which sends the restore down the copy fallback
// (trash.go:400-431). The hook is restored by t.Cleanup and the test is not
// parallel. This test does not prove the race absent in production; it shows the
// copy fallback's behaviour when the destination appears in that window.

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// TestQA2Break_CrossDeviceRestoreNeverOverwritesLateOccupant attacks FR-8 (a
// restore whose recorded path is occupied keeps both, and the occupant is left
// unchanged) on the cross-device restore path. Scenario: a jailed device restores
// trash item "late-item" (payload "restored-bytes") to root/back/a.txt. The
// occupant "late-occupant" appears at the destination after the free check, and
// the rename reports EXDEV. copySecureRecursive (fsops.go:1170-1171) then opens
// the existing regular file with O_TRUNC and writes the payload over it.
// Expected: the occupant still holds "late-occupant". The trashed item must not
// be lost: if the restore reports OK, its path holds "restored-bytes"; otherwise
// the payload is still at files/late-item with "restored-bytes". Label: code
// failure. Basis: FR-8 Accept ("the entry at P is unchanged") and UC-TRASH-RESTORE
// alt ("occupant unchanged"). Inference: that a destination created after the
// free check counts as "occupied". Open question 6 covers rename and move only,
// not restore. Confidence: medium.
func TestQA2Break_CrossDeviceRestoreNeverOverwritesLateOccupant(t *testing.T) {
	ops, root, trashDir := setupTrash(t)
	orig := filepath.Join(root, "back", "a.txt")
	const id = "late-item"
	qa2BreakWriteFile(t, filepath.Join(trashFilesDir(trashDir), id), "restored-bytes")
	if err := os.MkdirAll(trashInfoDir(trashDir), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := writeTrashInfo(trashDir, id, orig); err != nil {
		t.Fatal(err)
	}

	prev := renameAcrossRootsFn
	t.Cleanup(func() { renameAcrossRootsFn = prev })
	hookRan := false
	renameAcrossRootsFn = func(src, dst *securePath) error {
		hookRan = true
		// The occupant appears after the restore checked the destination free.
		f, err := os.OpenFile(dst.full, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err != nil {
			t.Errorf("could not plant the late occupant at %s: %v", dst.full, err)
		} else {
			if _, werr := f.WriteString("late-occupant"); werr != nil {
				t.Errorf("writing the late occupant: %v", werr)
			}
			_ = f.Close()
		}
		return &os.LinkError{Op: "rename", Old: src.full, New: dst.full, Err: syscall.EXDEV}
	}

	res := ops.RestoreFromTrash([]string{id}, trashDir)
	if !hookRan {
		t.Fatal("the cross-device hook was not reached; the restore did not take the copy path under test")
	}
	if len(res) != 1 {
		t.Fatalf("RestoreFromTrash returned %d results, want 1", len(res))
	}

	if got := qa2BreakRead(t, orig); got != "late-occupant" {
		t.Fatalf("FR-8: the cross-device restore overwrote the occupant %s (now %q, was \"late-occupant\")", orig, got)
	}
	payload := filepath.Join(trashFilesDir(trashDir), id)
	if res[0].OK {
		if got := qa2BreakRead(t, res[0].Path); got != "restored-bytes" {
			t.Fatalf("restored item at %s holds %q, want \"restored-bytes\"", res[0].Path, got)
		}
	} else {
		if got := qa2BreakRead(t, payload); got != "restored-bytes" {
			t.Fatalf("trashed item lost after a failed restore: payload %s holds %q", payload, got)
		}
	}
}
