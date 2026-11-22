package fsops

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

// gateReader yields data, then blocks (mid-copy) until gate is closed and reports EOF.
type gateReader struct {
	data    []byte
	sent    bool
	started chan struct{}
	gate    chan struct{}
	once    sync.Once
}

func (g *gateReader) Read(p []byte) (int, error) {
	if !g.sent {
		g.sent = true
		return copy(p, g.data), nil
	}
	g.once.Do(func() { close(g.started) })
	<-g.gate
	return 0, io.EOF
}

// killedReader yields data and then panics, which unwinds PublishReader without any of its error-path
// cleanup: the closest in-process stand-in for the process being killed mid-copy.
type killedReader struct {
	data []byte
	sent bool
}

func (k *killedReader) Read(p []byte) (int, error) {
	if !k.sent {
		k.sent = true
		return copy(p, k.data), nil
	}
	panic("simulated kill during copy")
}

func listNames(t *testing.T, dir string) []string {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range ents {
		names = append(names, e.Name())
	}
	return names
}

func crash(t *testing.T, fn func()) {
	t.Helper()
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("the simulated kill did not happen")
		}
	}()
	fn()
}

// While a publish is mid-copy its hidden temp file must not show up in a folder listing (nor in the
// parent's child count), for any device.
func TestListDirHidesInFlightPublishTemp(t *testing.T) {
	root := t.TempDir()
	dest := filepath.Join(root, "dest")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dest, "keep.txt"), []byte("k"), 0o644); err != nil {
		t.Fatal(err)
	}
	ops := New([]string{root}, false)
	gr := &gateReader{data: []byte("partial"), started: make(chan struct{}), gate: make(chan struct{})}
	done := make(chan error, 1)
	go func() {
		_, err := ops.PublishReader(filepath.Join(dest, "final.bin"), gr, false)
		done <- err
	}()
	select {
	case <-gr.started:
	case <-time.After(10 * time.Second):
		t.Fatal("publish did not start copying")
	}
	if !slices.ContainsFunc(listNames(t, dest), func(n string) bool { return strings.HasPrefix(n, ".rfe-upload-") }) {
		t.Fatal("test precondition: the in-flight temp file is not on disk")
	}
	lst, err := ops.ListDir(dest, "", 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range lst.Entries {
		if strings.HasPrefix(e.Name, ".rfe-upload-") {
			t.Fatalf("listing shows the in-flight temp %q", e.Name)
		}
	}
	if len(lst.Entries) != 1 || lst.Entries[0].Name != "keep.txt" {
		t.Fatalf("listing = %+v, want only keep.txt", lst.Entries)
	}
	parent, err := ops.ListDir(root, "", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(parent.Entries) != 1 || parent.Entries[0].ChildCount == nil || *parent.Entries[0].ChildCount != 1 {
		t.Fatalf("child count of dest counts the hidden temp: %+v", parent.Entries)
	}
	close(gr.gate)
	if err := <-done; err != nil {
		t.Fatalf("publish: %v", err)
	}
}

// The name predicate is the strict prefix, nothing looser.
func TestIsPublishTemp(t *testing.T) {
	for name, want := range map[string]bool{
		".rfe-upload-abc": true, ".rfe-upload-": true, ".rfe-upload": false, "rfe-upload-x": false,
		"x.rfe-upload-y": false, ".rfe-uploader": false, "": false,
	} {
		if got := IsPublishTemp(name); got != want {
			t.Errorf("IsPublishTemp(%q) = %v, want %v", name, got, want)
		}
	}
	if got := PublishTempName("sess-1"); got != ".rfe-upload-sess-1" {
		t.Errorf("PublishTempName = %q", got)
	}
}

type intentLog struct {
	mu     sync.Mutex
	begins [][2]string
	ends   int
}

func (l *intentLog) opts(name string) PublishOptions {
	return PublishOptions{
		TempName: name,
		Begin: func(dir, temp string) error {
			l.mu.Lock()
			defer l.mu.Unlock()
			l.begins = append(l.begins, [2]string{dir, temp})
			return nil
		},
		End: func() {
			l.mu.Lock()
			defer l.mu.Unlock()
			l.ends++
		},
	}
}

// A kill during the copy leaves the deterministic temp (and no End); the retried publish reuses that name,
// publishes once, leaves nothing behind and reports End.
func TestPublishReaderWithDeterministicTempSurvivesKillAndRetryReusesIt(t *testing.T) {
	for _, tc := range []struct {
		name      string
		jailed    bool
		overwrite bool
	}{
		{"jailed-new", true, false}, {"jailed-overwrite", true, true},
		{"unjailed-new", false, false}, {"unjailed-overwrite", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			realRoot, err := filepath.EvalSymlinks(root)
			if err != nil {
				t.Fatal(err)
			}
			dest := filepath.Join(root, "dest")
			ops := New(nil, false)
			if tc.jailed {
				ops = New([]string{root}, false)
			}
			target := filepath.Join(dest, "final.bin")
			tempName := PublishTempName("sess-1")
			var lg intentLog

			crash(t, func() {
				_, _ = ops.PublishReaderWith(target, &killedReader{data: []byte("partial")}, tc.overwrite, lg.opts(tempName))
			})
			got, err := os.ReadFile(filepath.Join(dest, tempName))
			if err != nil || string(got) != "partial" {
				t.Fatalf("temp after the kill = %q, %v; want the partial bytes under the deterministic name", got, err)
			}
			if len(lg.begins) != 1 || lg.begins[0] != [2]string{filepath.Join(realRoot, "dest"), tempName} || lg.ends != 0 {
				t.Fatalf("after the kill: begins=%v ends=%d", lg.begins, lg.ends)
			}

			if _, err := ops.PublishReaderWith(target, strings.NewReader("complete bytes"), tc.overwrite, lg.opts(tempName)); err != nil {
				t.Fatalf("retried publish: %v", err)
			}
			if names := listNames(t, dest); !slices.Equal(names, []string{"final.bin"}) {
				t.Fatalf("destination folder = %v, want only final.bin", names)
			}
			if b, _ := os.ReadFile(target); string(b) != "complete bytes" {
				t.Fatalf("final content = %q", b)
			}
			if len(lg.begins) != 2 || lg.ends != 1 {
				t.Fatalf("after the retry: begins=%v ends=%d, want 2 and 1", lg.begins, lg.ends)
			}
		})
	}
}

// A refused publish (destination exists, no overwrite) removes its temp and still reports End.
func TestPublishReaderWithConflictRemovesTempAndEnds(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "final.bin")
	if err := os.WriteFile(target, []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	var lg intentLog
	_, err := New([]string{root}, false).PublishReaderWith(target, strings.NewReader("new"), false, lg.opts(PublishTempName("s")))
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("err = %v, want ErrConflict", err)
	}
	if names := listNames(t, root); !slices.Equal(names, []string{"final.bin"}) || lg.ends != 1 {
		t.Fatalf("folder = %v, ends = %d", names, lg.ends)
	}
}

// If the intent cannot be recorded nothing is written: an untracked temp is what the sweep could never find.
func TestPublishReaderWithBeginFailureWritesNothing(t *testing.T) {
	root := t.TempDir()
	boom := errors.New("db down")
	_, err := New([]string{root}, false).PublishReaderWith(filepath.Join(root, "f"), strings.NewReader("x"), false, PublishOptions{
		TempName: PublishTempName("s"),
		Begin:    func(string, string) error { return boom },
	})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the Begin error", err)
	}
	if names := listNames(t, root); len(names) != 0 {
		t.Fatalf("folder = %v, want empty", names)
	}
}

// A symlink planted at the deterministic temp name is never written through and its target is never touched.
func TestPublishReaderWithSymlinkAtTempNameIsNotFollowed(t *testing.T) {
	root := t.TempDir()
	victim := filepath.Join(t.TempDir(), "victim.txt")
	if err := os.WriteFile(victim, []byte("precious"), 0o644); err != nil {
		t.Fatal(err)
	}
	name := PublishTempName("s")
	if err := os.Symlink(victim, filepath.Join(root, name)); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	for _, ops := range []*Ops{New([]string{root}, false), New(nil, false)} {
		_, err := ops.PublishReaderWith(filepath.Join(root, "f"), strings.NewReader("new"), true, PublishOptions{TempName: name})
		if err == nil {
			t.Fatal("publish through a planted symlink succeeded")
		}
		if b, _ := os.ReadFile(victim); string(b) != "precious" {
			t.Fatalf("victim overwritten: %q", b)
		}
	}
}

func TestRemovePublishTemp(t *testing.T) {
	t.Run("removes a regular temp and tolerates a missing one", func(t *testing.T) {
		dir := t.TempDir()
		name := PublishTempName("s")
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := RemovePublishTemp(dir, name); err != nil {
			t.Fatalf("remove: %v", err)
		}
		if names := listNames(t, dir); len(names) != 0 {
			t.Fatalf("left %v", names)
		}
		if err := RemovePublishTemp(dir, name); err != nil {
			t.Fatalf("a missing temp must not be an error: %v", err)
		}
		if err := RemovePublishTemp(filepath.Join(dir, "gone"), name); err != nil {
			t.Fatalf("a missing folder must not be an error: %v", err)
		}
	})
	t.Run("refuses names that are not a bare publish temp", func(t *testing.T) {
		dir := t.TempDir()
		for _, bad := range []string{"keep.txt", "sub/.rfe-upload-x", "../.rfe-upload-x", ".rfe-upload-*", ""} {
			if err := os.WriteFile(filepath.Join(dir, "keep.txt"), []byte("k"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := RemovePublishTemp(dir, bad); !errors.Is(err, ErrPublishTempRefused) {
				t.Errorf("RemovePublishTemp(%q) = %v, want ErrPublishTempRefused", bad, err)
			}
		}
		if names := listNames(t, dir); !slices.Equal(names, []string{"keep.txt"}) {
			t.Fatalf("folder = %v", names)
		}
	})
	t.Run("a relative folder is refused", func(t *testing.T) {
		if err := RemovePublishTemp("rel/dir", PublishTempName("s")); !errors.Is(err, ErrPublishTempRefused) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("a symlink swapped in at the temp name is left alone and its target survives", func(t *testing.T) {
		dir := t.TempDir()
		victim := filepath.Join(t.TempDir(), "victim.txt")
		if err := os.WriteFile(victim, []byte("precious"), 0o644); err != nil {
			t.Fatal(err)
		}
		name := PublishTempName("s")
		if err := os.Symlink(victim, filepath.Join(dir, name)); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		if err := RemovePublishTemp(dir, name); !errors.Is(err, ErrPublishTempRefused) {
			t.Fatalf("err = %v, want ErrPublishTempRefused", err)
		}
		if b, err := os.ReadFile(victim); err != nil || string(b) != "precious" {
			t.Fatalf("victim = %q, %v", b, err)
		}
	})
	t.Run("a folder swapped for a symlink cannot redirect the delete", func(t *testing.T) {
		parent := t.TempDir()
		dir := filepath.Join(parent, "dest")
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		victimDir := filepath.Join(t.TempDir(), "elsewhere")
		if err := os.Mkdir(victimDir, 0o755); err != nil {
			t.Fatal(err)
		}
		name := PublishTempName("s")
		victim := filepath.Join(victimDir, name)
		if err := os.WriteFile(victim, []byte("precious"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(dir, filepath.Join(parent, "dest.moved")); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(victimDir, dir); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		if err := RemovePublishTemp(dir, name); !errors.Is(err, ErrPublishTempRefused) {
			t.Fatalf("err = %v, want ErrPublishTempRefused", err)
		}
		if b, err := os.ReadFile(victim); err != nil || string(b) != "precious" {
			t.Fatalf("victim = %q, %v", b, err)
		}
	})
	t.Run("a folder in the way is refused", func(t *testing.T) {
		dir := t.TempDir()
		name := PublishTempName("s")
		if err := os.Mkdir(filepath.Join(dir, name), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := RemovePublishTemp(dir, name); !errors.Is(err, ErrPublishTempRefused) {
			t.Fatalf("err = %v", err)
		}
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("the folder was removed: %v", err)
		}
	})
}
