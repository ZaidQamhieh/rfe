package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// RevocationsLogFile is the append-only log, kept beside agent.db but outside it, of devices that were
// revoked or removed. A restored backup replays it so that a revoked phone does not come back to life.
const RevocationsLogFile = "revocations.log"

// Revocation kinds written to the log.
const (
	RevocationRevoke = "revoke" // the device row stays, marked revoked
	RevocationRemove = "remove" // the device row was deleted
)

// Revocation is one line of the revocation log.
type Revocation struct {
	DeviceID string    `json:"device_id"`
	Time     time.Time `json:"time"`
	Kind     string    `json:"kind"`
}

// revocationLogMu serialises appends inside this process; O_APPEND keeps appends from other processes
// (the daemon and the admin CLI) from interleaving within a line.
var revocationLogMu sync.Mutex

// AppendRevocation adds one JSON line to dir/revocations.log and fsyncs it before returning. The file
// is created 0600 and only ever appended to, except that a torn final line left by a crash mid-append is
// first repaired (see repairTornTail) so the new line starts on a line of its own.
func AppendRevocation(dir, deviceID, kind string, at time.Time) error {
	line, err := json.Marshal(Revocation{DeviceID: deviceID, Time: at.UTC(), Kind: kind})
	if err != nil {
		return err
	}
	line = append(line, '\n')
	revocationLogMu.Lock()
	defer revocationLogMu.Unlock()
	f, err := os.OpenFile(filepath.Join(dir, RevocationsLogFile), os.O_APPEND|os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	if err := repairTornTail(f); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(line); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// repairTornTail makes sure f ends on a line boundary before an append. A file that does not end in a
// newline was cut short mid-write: if its last line is still a complete entry it is kept and given its
// newline; otherwise the fragment is truncated away, since a never-completed entry was never reported as
// logged and gluing a new line onto it would corrupt the middle of the file for every later replay.
func repairTornTail(f *os.File) error {
	raw, err := io.ReadAll(f)
	if err != nil {
		return err
	}
	if len(raw) == 0 || raw[len(raw)-1] == '\n' {
		return nil
	}
	start := bytes.LastIndexByte(raw, '\n') + 1
	if validRevocationLine(raw[start:]) {
		_, err := f.Write([]byte{'\n'})
		return err
	}
	return f.Truncate(int64(start))
}

// validRevocationLine reports whether line is one complete, well-formed log entry.
func validRevocationLine(line []byte) bool {
	var r Revocation
	if err := json.Unmarshal(bytes.TrimSpace(line), &r); err != nil {
		return false
	}
	return r.DeviceID != "" && (r.Kind == RevocationRevoke || r.Kind == RevocationRemove)
}

// ReadRevocations parses the log at path. A missing file is an empty log. A torn final line (a crash
// mid-append) is ignored; a malformed line anywhere else is an error rather than being skipped.
func ReadRevocations(path string) ([]Revocation, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	lines := bytes.Split(raw, []byte("\n"))
	var out []Revocation
	for i, line := range lines {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var r Revocation
		if err := json.Unmarshal(line, &r); err != nil || r.DeviceID == "" || (r.Kind != RevocationRevoke && r.Kind != RevocationRemove) {
			if i == len(lines)-1 { // no trailing newline: the append was cut short
				break
			}
			return nil, fmt.Errorf("%s line %d is malformed", path, i+1)
		}
		out = append(out, r)
	}
	return out, nil
}

// ReplayRevocation re-applies one logged revocation. It runs the same transaction as RevokeDevice or
// DeleteDevice but writes no new log line; a device that is already revoked or gone is left as it is,
// so replaying the same entry twice changes nothing more.
func (s *DB) ReplayRevocation(deviceID, kind string) error {
	switch kind {
	case RevocationRevoke:
		return s.endDevice(deviceID, `UPDATE devices SET revoked=1, revoked_by_owner=1 WHERE id=?`, "")
	case RevocationRemove:
		return s.endDevice(deviceID, `DELETE FROM devices WHERE id=?`, "")
	}
	return fmt.Errorf("unknown revocation kind %q", kind)
}

// logRevocations appends one log line per device id; a failure is reported but not returned, because
// the revocation itself is already committed and must not look like it failed.
func (s *DB) logRevocations(kind string, ids ...string) {
	now := time.Now()
	for _, id := range ids {
		if err := AppendRevocation(s.dir, id, kind, now); err != nil {
			log.Printf("store: device %s was ended but the revocation log %s could not be written: %v", id, RevocationsLogFile, err)
		}
	}
}
