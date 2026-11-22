package backup

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewestPicksLatestOfLabelAndMissingIsNotExist(t *testing.T) {
	dir := t.TempDir()
	if _, err := Newest(filepath.Join(dir, "none"), LabelPremigration); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("missing dir: %v", err)
	}
	if _, err := Newest(dir, LabelPremigration); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("empty dir: %v", err)
	}
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	want := ArchiveName(LabelPremigration, base.Add(2*time.Hour))
	for _, n := range []string{ArchiveName(LabelPremigration, base), want, ArchiveName(LabelManual, base.Add(5*time.Hour))} {
		if err := os.WriteFile(filepath.Join(dir, n), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	got, err := Newest(dir, LabelPremigration)
	if err != nil || filepath.Base(got) != want {
		t.Fatalf("%q %v, want %s", got, err, want)
	}
}
