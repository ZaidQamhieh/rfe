package fsops

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// h6Source writes a private file holding data, the way an upload session's temp is, and opens it for reading.
func h6Source(t *testing.T, dir string, data []byte) (string, *os.File) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "session.tmp")
	if err := os.WriteFile(p, data, 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.Close() })
	return p, f
}

// A publish with MoveFrom on one filesystem renames the source into place (same inode), makes the destination
// folder durable afterwards, and leaves no hidden temp in it.
func TestPublishMoveRenamesAndSyncsTheDestinationFolder(t *testing.T) {
	root := t.TempDir()
	data := []byte("moved, not copied")
	src, f := h6Source(t, filepath.Join(root, "data"), data)
	before, _ := os.Stat(src)
	var synced []string
	old := syncDirFn
	syncDirFn = func(p *securePath) error { synced = append(synced, p.full); return nil }
	t.Cleanup(func() { syncDirFn = old })

	ops := New([]string{root}, false)
	dest := filepath.Join(root, "dest", "out.bin")
	info, err := ops.PublishReaderWith(dest, f, false, PublishOptions{MoveFrom: src})
	if err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(dest)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) {
		t.Errorf("the published file is a copy")
	}
	if after.Mode().Perm() != newFilePerm || info.Mode().Perm() != newFilePerm {
		t.Errorf("mode = %v / %v, want %v", after.Mode().Perm(), info.Mode().Perm(), newFilePerm)
	}
	if len(synced) != 1 || synced[0] != filepath.Join(root, "dest") {
		t.Errorf("destination folder syncs = %v, want exactly the destination folder", synced)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Errorf("source still has its old name: %v", err)
	}
	entries, _ := os.ReadDir(filepath.Join(root, "dest"))
	if len(entries) != 1 {
		t.Errorf("destination folder holds %d entries, want only the file", len(entries))
	}
}

// On another filesystem the move cannot happen, so the publish copies: same bytes, the source untouched and
// still private.
func TestPublishMoveFallsBackToCopyAcrossFilesystems(t *testing.T) {
	other, err := os.MkdirTemp("/dev/shm", "rfe-publish-move-")
	if err != nil {
		t.Skipf("no second filesystem to test with: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(other) })
	dir := t.TempDir()
	data := []byte("copied across filesystems")
	src, f := h6Source(t, dir, data)
	a, _ := os.Stat(src)
	b, _ := os.Stat(other)
	if sameDevice(a, b) {
		t.Skip("/dev/shm is on the same filesystem as the temp folder")
	}
	ops := New(nil, false)
	dest := filepath.Join(other, "out.bin")
	if _, err := ops.PublishReaderWith(dest, f, false, PublishOptions{MoveFrom: src}); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(dest); !bytes.Equal(got, data) {
		t.Errorf("content = %q", got)
	}
	fi, err := os.Stat(src)
	if err != nil {
		t.Fatalf("source gone after a copy publish: %v", err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("source mode = %v, want it still private", fi.Mode().Perm())
	}
	if entries, _ := os.ReadDir(other); len(entries) != 1 {
		t.Errorf("destination holds %d entries, want only the file", len(entries))
	}
}

// MoveFrom is honoured only for the very file being read: a different reader, or a symlink at MoveFrom,
// is copied and the file at MoveFrom is left alone.
func TestPublishMoveIgnoredUnlessTheSourceIsThatFile(t *testing.T) {
	root := t.TempDir()
	ops := New([]string{root}, false)
	secret := filepath.Join(root, "secret.txt")
	if err := os.WriteFile(secret, []byte("do not move"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ops.PublishReaderWith(filepath.Join(root, "a.bin"), bytes.NewReader([]byte("payload")), false, PublishOptions{MoveFrom: secret}); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(secret); string(got) != "do not move" {
		t.Errorf("a file that was not the source was moved or changed")
	}
	if got, _ := os.ReadFile(filepath.Join(root, "a.bin")); string(got) != "payload" {
		t.Errorf("published content = %q", got)
	}

	link := filepath.Join(root, "link")
	if err := os.Symlink(secret, link); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(secret)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := ops.PublishReaderWith(filepath.Join(root, "b.bin"), f, false, PublishOptions{MoveFrom: link}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(link); err != nil {
		t.Errorf("the symlink at MoveFrom was moved: %v", err)
	}
	if got, _ := os.ReadFile(secret); string(got) != "do not move" {
		t.Errorf("a symlink source led to moving its target")
	}
}
