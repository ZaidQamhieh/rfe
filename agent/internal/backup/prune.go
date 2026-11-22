package backup

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Policy is the retention rule Prune applies to a backup directory.
type Policy struct {
	KeepPremigration int   // newest archives labelled "premigration" kept
	KeepDaily        int   // newest "daily" kept
	KeepWeekly       int   // newest "weekly" kept
	MaxTotalBytes    int64 // total size cap over all archives; 0 means no cap
	// KeepOther limits further labels by count, for example the event backups taken after a pairing or a
	// revocation. A label listed here is pruned like the three above; a label in no list is never pruned by count.
	KeepOther map[string]int
}

// DefaultMaxTotalBytes is the size cap in DefaultPolicy.
const DefaultMaxTotalBytes int64 = 2 << 30

// Labels of the backups the daemon takes after a security-relevant event.
const (
	LabelPair   = "pair"
	LabelRevoke = "revoke"
)

// DefaultPolicy keeps 3 premigration, 7 daily and 4 weekly backups, plus the 3 newest taken after a pairing and
// 3 after a revocation, within 2 GiB.
func DefaultPolicy() Policy {
	return Policy{KeepPremigration: 3, KeepDaily: 7, KeepWeekly: 4, MaxTotalBytes: DefaultMaxTotalBytes,
		KeepOther: map[string]int{LabelPair: 3, LabelRevoke: 3}}
}

// archiveFile is one backup file found in a directory.
type archiveFile struct {
	path  string
	label string
	taken time.Time // from the file name
	size  int64
}

// listArchives returns the regular files in dir whose names follow the backup naming scheme.
// Everything else in the directory is ignored and never deleted.
func listArchives(dir string) ([]archiveFile, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []archiveFile
	for _, e := range entries {
		sub := archiveNameRE.FindStringSubmatch(e.Name())
		if sub == nil || !e.Type().IsRegular() {
			continue
		}
		taken, err := time.Parse(archiveTimeLayout, sub[2])
		if err != nil {
			continue
		}
		info, err := e.Info()
		if err != nil {
			return nil, err
		}
		out = append(out, archiveFile{path: filepath.Join(dir, e.Name()), label: sub[1], taken: taken, size: info.Size()})
	}
	return out, nil
}

// Prune deletes old backups in dir according to p and returns the paths it removed. For each of
// premigration, daily and weekly only the newest N are kept; other labels (manual, ...) are kept by
// count. Then, while the remaining archives exceed MaxTotalBytes, the oldest is removed, but the newest
// archive overall is never removed. Files that do not match the naming scheme are left alone.
func Prune(dir string, p Policy) ([]string, error) {
	files, err := listArchives(dir)
	if err != nil {
		return nil, err
	}
	sort.Slice(files, func(i, j int) bool { return files[i].taken.After(files[j].taken) }) // newest first
	limits := map[string]int{
		LabelPremigration: p.KeepPremigration,
		LabelDaily:        p.KeepDaily,
		LabelWeekly:       p.KeepWeekly,
	}
	for label, n := range p.KeepOther {
		limits[label] = n
	}
	seen := map[string]int{}
	var keep, drop []archiveFile
	for _, f := range files {
		limit, limited := limits[f.label]
		seen[f.label]++
		if limited && seen[f.label] > limit {
			drop = append(drop, f)
			continue
		}
		keep = append(keep, f)
	}
	if p.MaxTotalBytes > 0 {
		var total int64
		for _, f := range keep {
			total += f.size
		}
		for len(keep) > 1 && total > p.MaxTotalBytes {
			oldest := keep[len(keep)-1]
			keep = keep[:len(keep)-1]
			total -= oldest.size
			drop = append(drop, oldest)
		}
	}
	var removed []string
	var errs []error
	for _, f := range drop {
		if err := os.Remove(f.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, fmt.Errorf("remove %s: %w", f.path, err))
			continue
		}
		removed = append(removed, f.path)
	}
	return removed, errors.Join(errs...)
}
