package backup

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestStatusMissingFileIsEmptyAndCorruptFileIsReported(t *testing.T) {
	dir := t.TempDir()
	rep, err := Status(dir)
	if err != nil || rep.LastBackup != nil || rep.LastVerification != nil {
		t.Fatalf("missing file: %+v %v", rep, err)
	}
	if err := os.WriteFile(filepath.Join(dir, StatusFile), []byte("{oops"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Status(dir); err == nil {
		t.Fatal("corrupt status must be reported")
	}
	// A corrupt file never blocks the next update.
	if err := RecordBackup(dir, BackupInfo{Label: "daily", OK: true}); err != nil {
		t.Fatalf("update over corrupt file: %v", err)
	}
	if rep, err := Status(dir); err != nil || rep.LastBackup == nil || rep.LastBackup.Label != "daily" {
		t.Fatalf("after update: %+v %v", rep, err)
	}
}

func TestStatusRoundTripAndFilePermissions(t *testing.T) {
	dir := t.TempDir()
	at := time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC)
	if err := RecordBackup(dir, BackupInfo{Time: at, Label: "weekly", Path: "backups/x", Size: 9, OK: true}); err != nil {
		t.Fatal(err)
	}
	if err := RecordOffsite(dir, at, errors.New("boom"), "/mnt/usb"); err != nil {
		t.Fatal(err)
	}
	rep, err := Status(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !rep.LastBackup.Time.Equal(at) || rep.LastBackup.Size != 9 || !rep.Offsite.Configured || rep.Offsite.OK || rep.Offsite.Error != "boom" {
		t.Fatalf("round trip: %+v", rep)
	}
	if runtime.GOOS != "windows" {
		if st, _ := os.Stat(filepath.Join(dir, StatusFile)); st.Mode().Perm() != 0o600 {
			t.Errorf("status file mode %v, want 0600", st.Mode().Perm())
		}
	}
	leftovers, _ := filepath.Glob(filepath.Join(dir, "*.tmp"))
	if len(leftovers) != 0 {
		t.Errorf("temp files left: %v", leftovers)
	}
}

func TestScrubErrorShortensHostPaths(t *testing.T) {
	data := "/srv/rfe data"
	cases := []struct{ in, want string }{
		{"open " + data + "/backups/a.tar.gz: denied", "open <data>/backups/a.tar.gz: denied"},
		{"copy /mnt/usb/offsite/x.enc: full", "copy x.enc: full"},
		{"restore to /tmp/rfe-trial-123/data: exists", "restore to data: exists"},
		{"plain message", "plain message"},
	}
	for _, c := range cases {
		if got := ScrubError(errors.New(c.in), data); got != c.want {
			t.Errorf("ScrubError(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	if got := ScrubError(errors.New("write /mnt/usb/offsite: x"), data, "/mnt/usb/offsite"); strings.Contains(got, "/mnt") {
		t.Errorf("extra dir not scrubbed: %q", got)
	}
	if got := RelativeArchivePath("/d", "/elsewhere/z.tar.gz"); got != "z.tar.gz" {
		t.Errorf("outside archive path = %q", got)
	}
}
