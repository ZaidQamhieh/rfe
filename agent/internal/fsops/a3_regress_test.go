package fsops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A published upload must get the documented default mode for created files, not the 0600 of its temp file.
func TestPublishReaderCreatesFileWithDefaultMode(t *testing.T) {
	if os.PathSeparator == '\\' {
		t.Skip("POSIX permission bits are not meaningful on Windows")
	}
	for _, overwrite := range []bool{false, true} {
		dir := t.TempDir()
		target := filepath.Join(dir, "up.bin")
		info, err := New([]string{dir}, false).PublishReader(target, strings.NewReader("data"), overwrite)
		if err != nil {
			t.Fatalf("overwrite=%v: %v", overwrite, err)
		}
		st, err := os.Stat(target)
		if err != nil {
			t.Fatal(err)
		}
		if st.Mode().Perm() != newFilePerm {
			t.Errorf("overwrite=%v: published mode = %v, want %v", overwrite, st.Mode().Perm(), newFilePerm)
		}
		if info.Mode().Perm() != newFilePerm {
			t.Errorf("overwrite=%v: returned info mode = %v, want %v", overwrite, info.Mode().Perm(), newFilePerm)
		}
	}
}

// Narrowing an already deny-all view further must not turn it back into an allow view.
func TestJailedOnDenyAllStaysDenyAll(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	sub := filepath.Join(root, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	denied := New([]string{root}, false).Jailed(outside)
	if !denied.IsDenyAll() {
		t.Fatal("precondition: Jailed outside the roots must be deny-all")
	}
	again := denied.Jailed(sub)
	if !again.IsDenyAll() {
		t.Fatal("Jailed on a deny-all Ops must stay deny-all")
	}
	if _, err := again.Resolve(filepath.Join(sub, "f.txt")); err == nil {
		t.Fatal("a path inside the second jail was resolved by a deny-all Ops")
	}
}
