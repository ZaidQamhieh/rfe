package fsops

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// swapSyncDir replaces the directory-sync seam for one test.
func swapSyncDir(t *testing.T, fn func(*securePath) error) {
	t.Helper()
	old := syncDirFn
	syncDirFn = fn
	t.Cleanup(func() { syncDirFn = old })
}

// TestPublishReaderSyncsDestinationFolder: once the final name exists (rename or link), the folder holding it is
// synced exactly once before PublishReader returns, so a completed upload cannot vanish after a power cut.
func TestPublishReaderSyncsDestinationFolder(t *testing.T) {
	for _, overwrite := range []bool{false, true} {
		name := "no-replace"
		if overwrite {
			name = "overwrite"
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			target := filepath.Join(dir, "target.bin")
			if overwrite {
				if err := os.WriteFile(target, []byte("old"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			var folders []string
			var contentAtSync string
			swapSyncDir(t, func(p *securePath) error {
				folders = append(folders, p.full)
				b, _ := os.ReadFile(target)
				contentAtSync = string(b)
				return nil
			})
			if _, err := New([]string{dir}, false).PublishReader(target, strings.NewReader("payload"), overwrite); err != nil {
				t.Fatal(err)
			}
			if len(folders) != 1 || folders[0] != dir {
				t.Fatalf("folder syncs = %v, want exactly [%s]", folders, dir)
			}
			if contentAtSync != "payload" {
				t.Fatalf("folder was synced before the final name held the new bytes (saw %q)", contentAtSync)
			}
		})
	}
}

// TestPublishReaderFailsWhenFolderSyncFails: if the folder cannot be made durable the publish is not reported
// as done, so the caller never records the upload as completed.
func TestPublishReaderFailsWhenFolderSyncFails(t *testing.T) {
	dir := t.TempDir()
	swapSyncDir(t, func(*securePath) error { return syscall.EIO })
	_, err := New([]string{dir}, false).PublishReader(filepath.Join(dir, "t.bin"), strings.NewReader("x"), true)
	if !errors.Is(err, syscall.EIO) {
		t.Fatalf("PublishReader err = %v, want the folder-sync error", err)
	}
}
