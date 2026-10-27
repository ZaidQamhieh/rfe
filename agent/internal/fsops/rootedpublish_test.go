package fsops

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type swappingReader struct {
	reader *strings.Reader
	onRead func()
	done   bool
}

func (r *swappingReader) Read(p []byte) (int, error) {
	if !r.done {
		r.done = true
		r.onRead()
	}
	return r.reader.Read(p)
}

func TestPublishReaderRejectsFinalSymlinkOutsideJail(t *testing.T) {
	parent := t.TempDir()
	jail := filepath.Join(parent, "jail")
	outside := filepath.Join(parent, "outside")
	if err := os.Mkdir(jail, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(outside, "secret.bin")
	if err := os.WriteFile(secret, []byte("outside-original"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(jail, "target.bin")
	if err := os.Symlink(secret, link); err != nil {
		t.Skipf("cannot create symlink on this platform: %v", err)
	}

	if _, err := New([]string{jail}, false).PublishReader(link, strings.NewReader("replacement"), true); !errors.Is(err, ErrForbidden) {
		t.Fatalf("PublishReader error = %v, want ErrForbidden", err)
	}
	got, err := os.ReadFile(secret)
	if err != nil || string(got) != "outside-original" {
		t.Fatalf("outside file after rejected publish = (%q, %v)", got, err)
	}
}

func TestPublishReaderNoReplaceAndAtomicOverwrite(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.bin")
	fresh := filepath.Join(dir, "fresh.bin")
	createdInfo, err := New([]string{dir}, false).PublishReader(fresh, strings.NewReader("created"), false)
	if err != nil || createdInfo == nil || createdInfo.Size() != int64(len("created")) {
		t.Fatalf("no-replace create = (%v, %v)", createdInfo, err)
	}
	created, err := os.ReadFile(fresh)
	if err != nil || string(created) != "created" {
		t.Fatalf("no-replace created data = (%q, %v)", created, err)
	}
	if err := os.WriteFile(target, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	ops := New([]string{dir}, false)
	if _, err := ops.PublishReader(target, strings.NewReader("new"), false); !errors.Is(err, ErrConflict) {
		t.Fatalf("no-replace error = %v, want ErrConflict", err)
	}
	got, err := os.ReadFile(target)
	if err != nil || string(got) != "old" {
		t.Fatalf("destination after no-replace = (%q, %v)", got, err)
	}
	if _, err := ops.PublishReader(target, strings.NewReader("new"), true); err != nil {
		t.Fatalf("overwrite publish: %v", err)
	}
	got, err = os.ReadFile(target)
	if err != nil || string(got) != "new" {
		t.Fatalf("destination after overwrite = (%q, %v)", got, err)
	}
}

func TestPublishReaderFinalSymlinkSwapCannotRedirectOutside(t *testing.T) {
	for _, overwrite := range []bool{false, true} {
		name := "no-replace"
		if overwrite {
			name = "overwrite"
		}
		t.Run(name, func(t *testing.T) {
			parent := t.TempDir()
			jail := filepath.Join(parent, "jail")
			outside := filepath.Join(parent, "outside.bin")
			if err := os.Mkdir(jail, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(outside, []byte("outside-original"), 0o600); err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(jail, "target.bin")
			var symlinkErr error
			src := &swappingReader{
				reader: strings.NewReader("inside-payload"),
				onRead: func() {
					symlinkErr = os.Symlink(outside, target)
				},
			}
			_, err := New([]string{jail}, false).PublishReader(target, src, overwrite)
			if symlinkErr != nil {
				t.Skipf("cannot create symlink during publish on this platform: %v", symlinkErr)
			}
			gotOutside, readErr := os.ReadFile(outside)
			if readErr != nil || string(gotOutside) != "outside-original" {
				t.Fatalf("outside file after publish = (%q, %v)", gotOutside, readErr)
			}
			if overwrite {
				if err != nil {
					t.Fatalf("overwrite should atomically replace the symlink itself: %v", err)
				}
				info, err := os.Lstat(target)
				if err != nil || info.Mode()&os.ModeSymlink != 0 {
					t.Fatalf("target after overwrite = (%v, %v), want regular file", info, err)
				}
				got, err := os.ReadFile(target)
				if err != nil || string(got) != "inside-payload" {
					t.Fatalf("published data = (%q, %v)", got, err)
				}
			} else if !errors.Is(err, ErrConflict) {
				t.Fatalf("no-replace error = %v, want ErrConflict", err)
			}
		})
	}
}

func TestPublishReaderParentSwapDuringStreamCannotEscapeRoot(t *testing.T) {
	parent := t.TempDir()
	jail := filepath.Join(parent, "jail")
	targetDir := filepath.Join(jail, "destination")
	outside := filepath.Join(parent, "outside")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(targetDir, "upload.bin")
	movedDir := filepath.Join(jail, "destination-original")
	var renameErr, symlinkErr error
	src := &swappingReader{
		reader: strings.NewReader("complete payload"),
		onRead: func() {
			if renameErr = os.Rename(targetDir, movedDir); renameErr != nil {
				return
			}
			symlinkErr = os.Symlink(outside, targetDir)
		},
	}

	_, err := New([]string{jail}, false).PublishReader(target, src, true)
	if renameErr != nil && windowsHandleBlocksRename(renameErr) {
		// Windows refuses to rename a folder while the publish holds a handle
		// inside it, so the swap cannot happen; the upload must land in the jail.
		if err != nil {
			t.Fatalf("publish with the swap refused: %v", err)
		}
		if got, rerr := os.ReadFile(target); rerr != nil || string(got) != "complete payload" {
			t.Fatalf("published data = (%q, %v)", got, rerr)
		}
		return
	}
	if renameErr != nil {
		t.Fatalf("rename parent during publish: %v", renameErr)
	}
	if symlinkErr != nil {
		t.Skipf("cannot create directory symlink on this platform: %v", symlinkErr)
	}
	if err == nil {
		t.Fatal("publish succeeded after its parent was swapped to an outside symlink")
	}
	if _, err := os.Lstat(filepath.Join(outside, "upload.bin")); !os.IsNotExist(err) {
		t.Fatalf("outside destination was created or stat failed: %v", err)
	}
}
