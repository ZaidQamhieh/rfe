package fsops

import (
	"archive/tar"
	"archive/zip"
	"compress/flate"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// Pass 3, workstream 11.1: the real extraction boundaries. These tests use the
// real maxArchiveEntries and maxArchiveTotalBytes (no overrides) and go through
// the exported Ops.Extract that /fs/extract calls.
//
// The byte tests write up to 2 GiB; they run only with RFE_LARGE_ARCHIVE_TESTS=1
// (and not under -short). RFE_LARGE_TMP, when set, names the directory (on a
// real disk) that holds the archive and the destination; otherwise t.TempDir().
//
//	RFE_LARGE_ARCHIVE_TESTS=1 RFE_LARGE_TMP=/path/on/disk \
//	  go test -count=1 -timeout 3000s -run 'Pass3ArchiveBytes' -v ./internal/fsops/

const pass3ArchBytesEnv = "RFE_LARGE_ARCHIVE_TESTS"

// pass3ArchLargeGate skips unless the owner opted into the large tests.
func pass3ArchLargeGate(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("large archive boundary test; run without -short")
	}
	if os.Getenv(pass3ArchBytesEnv) != "1" {
		t.Skip("set " + pass3ArchBytesEnv + "=1 to run (writes up to 2 GiB; RFE_LARGE_TMP picks the disk)")
	}
}

// pass3ArchSetup returns an Ops jailed to a fresh root (under RFE_LARGE_TMP
// when set) and an empty, pre-created destination directory inside it.
func pass3ArchSetup(t *testing.T) (ops *Ops, root, out string) {
	t.Helper()
	base := os.Getenv("RFE_LARGE_TMP")
	if base == "" {
		root = t.TempDir()
	} else {
		var err error
		root, err = os.MkdirTemp(base, "rfe-pass3-arch-*")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.RemoveAll(root) })
	}
	// The jail resolves symlinks; use the resolved root so paths agree.
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	out = filepath.Join(root, "out")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	return New([]string{root}, false), root, out
}

type pass3ArchZeros struct{}

func (pass3ArchZeros) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}

// pass3ArchEntryName spreads names over a few subdirectories (every entry still
// a regular file), or keeps them flat when dirs == 0.
func pass3ArchEntryName(i, dirs int) string {
	if dirs == 0 {
		return fmt.Sprintf("f%06d", i)
	}
	return fmt.Sprintf("d%d/f%06d", i%dirs, i)
}

func pass3ArchWriteZipEmpty(t *testing.T, path string, n, dirs int) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for i := 0; i < n; i++ {
		hdr := &zip.FileHeader{Name: pass3ArchEntryName(i, dirs), Method: zip.Store}
		hdr.SetMode(0o644)
		if _, err := zw.CreateHeader(hdr); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func pass3ArchWriteTarGzEmpty(t *testing.T, path string, n, dirs int) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	gz, err := gzip.NewWriterLevel(f, gzip.BestSpeed)
	if err != nil {
		t.Fatal(err)
	}
	tw := tar.NewWriter(gz)
	for i := 0; i < n; i++ {
		hdr := &tar.Header{Name: pass3ArchEntryName(i, dirs), Typeflag: tar.TypeReg, Mode: 0o644, Size: 0}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

// pass3ArchWriteZipSizes writes one deflated all-zero entry per size.
func pass3ArchWriteZipSizes(t *testing.T, path string, sizes []int64) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	zw.RegisterCompressor(zip.Deflate, func(w io.Writer) (io.WriteCloser, error) {
		return flate.NewWriter(w, flate.BestSpeed)
	})
	for i, size := range sizes {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: fmt.Sprintf("big%d.bin", i), Method: zip.Deflate})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.CopyN(w, pass3ArchZeros{}, size); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

// pass3ArchWriteTarGzSizes writes one all-zero entry per size (tar needs the
// size up front, which the loop knows).
func pass3ArchWriteTarGzSizes(t *testing.T, path string, sizes []int64) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	gz, err := gzip.NewWriterLevel(f, gzip.BestSpeed)
	if err != nil {
		t.Fatal(err)
	}
	tw := tar.NewWriter(gz)
	for i, size := range sizes {
		hdr := &tar.Header{Name: fmt.Sprintf("big%d.bin", i), Typeflag: tar.TypeReg, Mode: 0o644, Size: size}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := io.CopyN(tw, pass3ArchZeros{}, size); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func pass3ArchCountFiles(t *testing.T, dir string) int {
	t.Helper()
	n := 0
	err := filepath.WalkDir(dir, func(_ string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type().IsRegular() {
			n++
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// pass3ArchAssertEmpty fails when anything is left in dir (the destination
// existed and was empty before the extraction, so rollback must restore that).
func pass3ArchAssertEmpty(t *testing.T, dir string) {
	t.Helper()
	left, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 0 {
		t.Fatalf("rolled-back extraction left %d top-level names in %s (first %q)", len(left), dir, left[0].Name())
	}
}

type pass3ArchFormat struct {
	name      string
	ext       string
	writeN    func(t *testing.T, path string, n, dirs int)
	writeSize func(t *testing.T, path string, sizes []int64)
}

var pass3ArchFormats = []pass3ArchFormat{
	{"zip", ".zip", pass3ArchWriteZipEmpty, pass3ArchWriteZipSizes},
	{"targz", ".tar.gz", pass3ArchWriteTarGzEmpty, pass3ArchWriteTarGzSizes},
}

// TestPass3ArchiveEntries_ExactlyMaxExtracts: exactly maxArchiveEntries empty
// regular files, flat or spread over a few subdirectories, extract in full.
func TestPass3ArchiveEntries_ExactlyMaxExtracts(t *testing.T) {
	pass3ArchLargeGate(t)
	for _, fm := range pass3ArchFormats {
		for _, dirs := range []int{0, 4} {
			t.Run(fmt.Sprintf("%s/dirs%d", fm.name, dirs), func(t *testing.T) {
				ops, root, out := pass3ArchSetup(t)
				archive := filepath.Join(root, "at-limit"+fm.ext)
				fm.writeN(t, archive, maxArchiveEntries, dirs)
				if _, err := ops.Extract(archive, out); err != nil {
					t.Fatalf("Extract(%d entries) = %v, want nil", maxArchiveEntries, err)
				}
				if got := pass3ArchCountFiles(t, out); got != maxArchiveEntries {
					t.Fatalf("extracted %d files, want %d", got, maxArchiveEntries)
				}
			})
		}
	}
}

// TestPass3ArchiveEntries_OneOverMaxRollsBack: maxArchiveEntries+1 entries fail
// with ErrArchiveTooLarge and leave the destination as it was (empty).
func TestPass3ArchiveEntries_OneOverMaxRollsBack(t *testing.T) {
	pass3ArchLargeGate(t)
	for _, fm := range pass3ArchFormats {
		for _, dirs := range []int{0, 4} {
			t.Run(fmt.Sprintf("%s/dirs%d", fm.name, dirs), func(t *testing.T) {
				ops, root, out := pass3ArchSetup(t)
				archive := filepath.Join(root, "over-limit"+fm.ext)
				fm.writeN(t, archive, maxArchiveEntries+1, dirs)
				_, err := ops.Extract(archive, out)
				if !errors.Is(err, ErrArchiveTooLarge) {
					t.Fatalf("Extract(%d entries) = %v, want ErrArchiveTooLarge", maxArchiveEntries+1, err)
				}
				pass3ArchAssertEmpty(t, out)
			})
		}
	}
}

// TestPass3ArchiveBytes_ExactlyMaxExtracts: one entry of exactly
// maxArchiveTotalBytes extracts in full (2 GiB on disk).
func TestPass3ArchiveBytes_ExactlyMaxExtracts(t *testing.T) {
	pass3ArchLargeGate(t)
	for _, fm := range pass3ArchFormats {
		t.Run(fm.name, func(t *testing.T) {
			ops, root, out := pass3ArchSetup(t)
			archive := filepath.Join(root, "at-limit"+fm.ext)
			fm.writeSize(t, archive, []int64{maxArchiveTotalBytes})
			if _, err := ops.Extract(archive, out); err != nil {
				t.Fatalf("Extract(%d bytes) = %v, want nil", maxArchiveTotalBytes, err)
			}
			info, err := os.Lstat(filepath.Join(out, "big0.bin"))
			if err != nil {
				t.Fatal(err)
			}
			if info.Size() != maxArchiveTotalBytes {
				t.Fatalf("extracted size %d, want %d", info.Size(), maxArchiveTotalBytes)
			}
		})
	}
}

// TestPass3ArchiveBytes_OneOverMaxRollsBack: a single entry of
// maxArchiveTotalBytes+1 fails with ErrArchiveTooLarge and leaves nothing.
func TestPass3ArchiveBytes_OneOverMaxRollsBack(t *testing.T) {
	pass3ArchLargeGate(t)
	for _, fm := range pass3ArchFormats {
		t.Run(fm.name, func(t *testing.T) {
			ops, root, out := pass3ArchSetup(t)
			archive := filepath.Join(root, "over-limit"+fm.ext)
			fm.writeSize(t, archive, []int64{maxArchiveTotalBytes + 1})
			_, err := ops.Extract(archive, out)
			if !errors.Is(err, ErrArchiveTooLarge) {
				t.Fatalf("Extract(%d bytes) = %v, want ErrArchiveTooLarge", maxArchiveTotalBytes+1, err)
			}
			pass3ArchAssertEmpty(t, out)
		})
	}
}

// TestPass3ArchiveBytes_TwoEntriesOneOverMaxRollsBack: two entries whose sizes
// sum to maxArchiveTotalBytes+1 fail (the budget is per archive, not per entry)
// and the first, fully written entry is rolled back too.
func TestPass3ArchiveBytes_TwoEntriesOneOverMaxRollsBack(t *testing.T) {
	pass3ArchLargeGate(t)
	half := maxArchiveTotalBytes / 2
	for _, fm := range pass3ArchFormats {
		t.Run(fm.name, func(t *testing.T) {
			ops, root, out := pass3ArchSetup(t)
			archive := filepath.Join(root, "two-over"+fm.ext)
			fm.writeSize(t, archive, []int64{half, half + 1})
			_, err := ops.Extract(archive, out)
			if !errors.Is(err, ErrArchiveTooLarge) {
				t.Fatalf("Extract(%d+%d bytes) = %v, want ErrArchiveTooLarge", half, half+1, err)
			}
			pass3ArchAssertEmpty(t, out)
		})
	}
}
