package backup

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// StatusFile is the name, inside the data dir, of the file that remembers what the backup scheduler last did.
const StatusFile = "backup-status.json"

// maxStatusBytes bounds the status file read from disk.
const maxStatusBytes = 1 << 20

// BackupInfo describes the most recent backup attempt. Path is relative to the data dir (slash separated) when the
// archive lives inside it, otherwise just the file name, so no absolute path is ever recorded.
type BackupInfo struct {
	Time  time.Time `json:"time"`
	Label string    `json:"label"`
	Path  string    `json:"path"`
	Size  int64     `json:"size"`
	OK    bool      `json:"ok"`
	Error string    `json:"error,omitempty"`
}

// VerificationInfo describes the most recent trial restore of a stored archive.
type VerificationInfo struct {
	Time    time.Time `json:"time"`
	OK      bool      `json:"ok"`
	Archive string    `json:"archive,omitempty"` // file name of the archive that was tried
	Error   string    `json:"error,omitempty"`
}

// OffsiteInfo describes the optional encrypted off-device copy. The folder itself is never recorded.
type OffsiteInfo struct {
	Configured bool      `json:"configured"`
	LastTime   time.Time `json:"last_time,omitzero"`
	OK         bool      `json:"ok"`
	Error      string    `json:"error,omitempty"`
}

// SchedulerInfo describes the in-daemon backup scheduler.
type SchedulerInfo struct {
	Enabled   bool      `json:"enabled"`
	LastRun   time.Time `json:"last_run,omitzero"`
	LastError string    `json:"last_error,omitempty"`
	Pending   []string  `json:"pending,omitempty"` // event labels waiting out the debounce delay
}

// StatusReport is the content of backup-status.json and of GET /v1/system/backups (its backup part).
type StatusReport struct {
	LastBackup       *BackupInfo       `json:"last_backup"`
	LastVerification *VerificationInfo `json:"last_verification"`
	Offsite          OffsiteInfo       `json:"offsite"`
	Scheduler        SchedulerInfo     `json:"scheduler"`
}

// statusMu serialises read-modify-write cycles of the status file inside this process.
var statusMu sync.Mutex

// Status reads dataDir/backup-status.json. A missing file is an empty report and no error; an unreadable or corrupt
// one gives an empty report together with the error, so a caller can still show something.
func Status(dataDir string) (StatusReport, error) {
	data, err := readLimited(filepath.Join(dataDir, StatusFile))
	if errors.Is(err, fs.ErrNotExist) {
		return StatusReport{}, nil
	}
	if err != nil {
		return StatusReport{}, err
	}
	var rep StatusReport
	if err := json.Unmarshal(data, &rep); err != nil {
		return StatusReport{}, fmt.Errorf("%s is not valid JSON: %w", StatusFile, err)
	}
	return rep, nil
}

// readLimited reads a small regular file, refusing one larger than maxStatusBytes.
func readLimited(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxStatusBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxStatusBytes {
		return nil, fmt.Errorf("%s is larger than %d bytes", filepath.Base(path), maxStatusBytes)
	}
	return data, nil
}

// UpdateStatus applies fn to the current report and writes it back atomically (0600). A corrupt existing file is
// replaced rather than blocking every later update.
func UpdateStatus(dataDir string, fn func(*StatusReport)) error {
	statusMu.Lock()
	defer statusMu.Unlock()
	rep, _ := Status(dataDir)
	fn(&rep)
	data, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(dataDir, StatusFile), append(data, '\n'), 0o600)
}

// RecordBackup stores info as the last backup in the status file.
func RecordBackup(dataDir string, info BackupInfo) error {
	return UpdateStatus(dataDir, func(r *StatusReport) { r.LastBackup = &info })
}

// RecordOffsite stores the outcome of an off-device copy; err nil means success.
func RecordOffsite(dataDir string, at time.Time, err error, scrub ...string) error {
	return UpdateStatus(dataDir, func(r *StatusReport) {
		r.Offsite.Configured = true
		r.Offsite.LastTime = at.UTC()
		r.Offsite.OK = err == nil
		r.Offsite.Error = ""
		if err != nil {
			r.Offsite.Error = ScrubError(err, dataDir, scrub...)
		}
	})
}

// RelativeArchivePath returns archive as a slash-separated path under dataDir, or only its file name when it is
// not inside dataDir, so the status never carries an absolute path.
func RelativeArchivePath(dataDir, archive string) string {
	if rel, err := filepath.Rel(dataDir, archive); err == nil && rel != "." && !strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel) {
		return filepath.ToSlash(rel)
	}
	return filepath.Base(archive)
}

// absPathRE finds absolute file system paths (Unix or Windows) inside an error text.
var absPathRE = regexp.MustCompile("[\x01\x02]?(?:[A-Za-z]:)?[\\\\/](?:[^\\s:\"'<>|?*\\\\/]+[\\\\/])*[^\\s:\"'<>|?*\\\\/]+")

// ScrubError returns err's text with dataDir replaced by "<data>", each extra directory by "<dir>" and any other
// absolute path reduced to its last element, so a status shown to a remote admin names no host path.
func ScrubError(err error, dataDir string, extra ...string) string {
	if err == nil {
		return ""
	}
	s := err.Error()
	// The data dir and the extra directories become one-byte markers first, so a path inside them keeps its
	// relative part while every other absolute path shrinks to its last element.
	for _, d := range extra {
		if len(d) > 1 {
			s = strings.ReplaceAll(s, d, "\x02")
		}
	}
	if len(dataDir) > 1 {
		s = strings.ReplaceAll(s, dataDir, "\x01")
	}
	s = absPathRE.ReplaceAllStringFunc(s, func(p string) string {
		switch p[0] {
		case 1:
			return "<data>" + p[1:]
		case 2:
			return "<dir>" + p[1:]
		}
		return filepath.Base(strings.ReplaceAll(p, "\\", "/"))
	})
	return strings.NewReplacer("\x01", "<data>", "\x02", "<dir>").Replace(s)
}
