package files

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestUsageSumsPerChildLargestFirst(t *testing.T) {
	e := newEnv(t, false)
	if err := os.MkdirAll(filepath.Join(e.root, "big", "deep"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(e.root, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(e.root, "big", "a.bin"), string(make([]byte, 300)))
	write(t, filepath.Join(e.root, "big", "deep", "b.bin"), string(make([]byte, 200)))
	write(t, filepath.Join(e.root, "top.txt"), string(make([]byte, 100)))
	res, err := e.svc.Usage(context.Background(), e.root)
	if err != nil {
		t.Fatal(err)
	}
	want := []UsageEntry{{"big", true, 500}, {"top.txt", false, 100}, {"empty", true, 0}}
	if res.TotalBytes != 600 || res.Partial || len(res.Entries) != len(want) {
		t.Fatalf("got %+v", res)
	}
	for i := range want {
		if res.Entries[i] != want[i] {
			t.Fatalf("entry %d = %+v, want %+v", i, res.Entries[i], want[i])
		}
	}
}

func TestUsageRefusesAFileAndAnOutsidePath(t *testing.T) {
	e := newEnv(t, false)
	f := filepath.Join(e.root, "f.txt")
	write(t, f, "x")
	if _, err := e.svc.Usage(context.Background(), f); err == nil {
		t.Fatal("a file is not a folder")
	}
	if _, err := e.svc.Usage(context.Background(), t.TempDir()); err == nil {
		t.Fatal("a folder outside the roots must be refused")
	}
}

func TestUsageCancelledIsPartial(t *testing.T) {
	e := newEnv(t, false)
	write(t, filepath.Join(e.root, "a.txt"), "x")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res, err := e.svc.Usage(ctx, e.root)
	if err != nil || !res.Partial {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}

func TestUsageTooDeepATreeIsPartial(t *testing.T) {
	e := newEnv(t, false)
	dir := filepath.Join(e.root, "d")
	for i := 0; i < recentMaxDepth+3; i++ {
		dir = filepath.Join(dir, "n")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Skip("path too long on this system")
	}
	write(t, filepath.Join(dir, "x.bin"), "abc")
	res, err := e.svc.Usage(context.Background(), e.root)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Partial {
		t.Fatalf("a tree deeper than %d folders must be partial: %+v", recentMaxDepth, res)
	}
}
