package backup

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Intervals of the scheduled backups and of the trial restore, and the scheduler's timing defaults.
const (
	DailyEvery       = 24 * time.Hour
	WeeklyEvery      = 7 * 24 * time.Hour
	TrialEvery       = 7 * 24 * time.Hour
	defaultDebounce  = 30 * time.Second
	defaultCheckTick = 15 * time.Minute
	// SubdirName is the folder inside the data dir that holds the scheduled backups.
	SubdirName = "backups"
)

// SchedulerConfig says what a Scheduler backs up and how. Everything but DataDir is optional; the function fields
// exist so tests can replace the clock, the archive writer and the restore trial.
type SchedulerConfig struct {
	DataDir      string
	BackupDir    string  // default <DataDir>/backups
	AgentVersion string  // recorded in each archive's manifest
	Policy       *Policy // retention; nil means DefaultPolicy
	Debounce     time.Duration
	Interval     time.Duration // how often Run looks for a due daily or weekly backup
	Now          func() time.Time
	Create       func(dataDir, destDir string, opt Options) (string, *Manifest, error)
	Verify       func(archive string) (*Manifest, error)
	Trial        func(archive, tmpDir string, check func(dataDir string) error) (*Manifest, error)
	// Check is run on the restored copy during the weekly trial restore (for example the selfcheck). Nil turns the
	// trial off.
	Check func(dataDir string) error
	// Offsite, when set, copies a finished daily or weekly archive off the device (encrypted); its error is recorded
	// in the status. OffsiteDir is only used to keep that folder out of recorded error texts.
	Offsite    func(archive string) error
	OffsiteDir string
	// Sleep waits for d or until ctx ends and reports whether the full time passed (default: a timer).
	Sleep func(ctx context.Context, d time.Duration) bool
	Logf  func(format string, args ...any)
}

// Scheduler takes the daily and weekly backups, event backups (after a pairing or a revocation) and the weekly trial
// restore of the newest archive. Run does the work on one goroutine; Trigger is safe to call from a request handler
// because it only records the request and wakes that goroutine. Every outcome is written to the status file; no error
// stops the scheduler.
type Scheduler struct {
	cfg    SchedulerConfig
	policy Policy
	mu     sync.Mutex
	pend   map[string]bool
	wake   chan struct{}
}

// NewScheduler fills the defaults in cfg and returns a Scheduler that is not running yet.
func NewScheduler(cfg SchedulerConfig) *Scheduler {
	if cfg.BackupDir == "" {
		cfg.BackupDir = filepath.Join(cfg.DataDir, SubdirName)
	}
	policy := DefaultPolicy()
	if cfg.Policy != nil {
		policy = *cfg.Policy
	}
	if cfg.Debounce <= 0 {
		cfg.Debounce = defaultDebounce
	}
	if cfg.Interval <= 0 {
		cfg.Interval = defaultCheckTick
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Create == nil {
		cfg.Create = Create
	}
	if cfg.Verify == nil {
		cfg.Verify = Verify
	}
	if cfg.Trial == nil {
		cfg.Trial = TrialRestore
	}
	if cfg.Sleep == nil {
		cfg.Sleep = func(ctx context.Context, d time.Duration) bool {
			t := time.NewTimer(d)
			defer t.Stop()
			select {
			case <-ctx.Done():
				return false
			case <-t.C:
				return true
			}
		}
	}
	if cfg.Logf == nil {
		cfg.Logf = func(string, ...any) {}
	}
	return &Scheduler{cfg: cfg, policy: policy, pend: map[string]bool{}, wake: make(chan struct{}, 1)}
}

// Trigger asks for a backup with the given label soon (after the debounce delay, so a burst of events gives one
// archive per label). It never blocks and never runs a backup itself. An invalid label is ignored.
func (s *Scheduler) Trigger(label string) {
	if !labelRE.MatchString(label) {
		return
	}
	s.mu.Lock()
	s.pend[label] = true
	s.mu.Unlock()
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// takePending returns the labels waiting for a backup, sorted, and clears them.
func (s *Scheduler) takePending() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	labels := make([]string, 0, len(s.pend))
	for l := range s.pend {
		labels = append(labels, l)
	}
	sort.Strings(labels)
	s.pend = map[string]bool{}
	return labels
}

// Run marks the scheduler enabled in the status file, does one due check at once, then serves triggers and checks
// again every Interval until ctx ends. It returns after the work in progress finished.
func (s *Scheduler) Run(ctx context.Context) {
	s.note(func(i *SchedulerInfo) { i.Enabled = true })
	defer s.note(func(i *SchedulerInfo) { i.Enabled = false })
	tick := time.NewTicker(s.cfg.Interval)
	defer tick.Stop()
	s.RunDue(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			s.RunDue(ctx)
		case <-s.wake:
			if !s.cfg.Sleep(ctx, s.cfg.Debounce) {
				return
			}
			s.RunPending(ctx)
		}
	}
}

// RunPending takes one backup for each label Trigger recorded since the last call.
func (s *Scheduler) RunPending(ctx context.Context) {
	for _, label := range s.takePending() {
		if ctx.Err() != nil {
			return
		}
		s.backupOnce(label)
	}
}

// RunDue takes the daily and weekly backups that are due (no archive of that label younger than its interval), then the
// trial restore when it is due.
func (s *Scheduler) RunDue(ctx context.Context) {
	for _, d := range []struct {
		label string
		every time.Duration
	}{{LabelDaily, DailyEvery}, {LabelWeekly, WeeklyEvery}} {
		if ctx.Err() != nil {
			return
		}
		if s.due(d.label, d.every) {
			s.backupOnce(d.label)
		}
	}
	if ctx.Err() == nil {
		s.maybeTrial()
	}
}

// due reports whether the newest archive with this label is missing or at least every old.
func (s *Scheduler) due(label string, every time.Duration) bool {
	files, err := listArchives(s.cfg.BackupDir)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return true
	}
	var newest time.Time
	for _, f := range files {
		if f.label == label && f.taken.After(newest) {
			newest = f.taken
		}
	}
	return newest.IsZero() || s.cfg.Now().Sub(newest) >= every
}

// note updates the scheduler part of the status file, logging a failure to write it.
func (s *Scheduler) note(fn func(*SchedulerInfo)) {
	if err := UpdateStatus(s.cfg.DataDir, func(r *StatusReport) { fn(&r.Scheduler) }); err != nil {
		s.cfg.Logf("backup status: %v", err)
	}
}

// scrub removes host paths from an error text before it is stored or logged.
func (s *Scheduler) scrub(err error, extra ...string) string {
	return ScrubError(err, s.cfg.DataDir, append([]string{s.cfg.OffsiteDir}, extra...)...)
}

// backupOnce writes and verifies one archive with the given label, prunes old ones, copies it off the device when
// that is configured, and records every outcome. An error is recorded and logged, never returned or raised.
func (s *Scheduler) backupOnce(label string) {
	now := s.cfg.Now().UTC()
	info := BackupInfo{Time: now, Label: label}
	archive, _, err := s.cfg.Create(s.cfg.DataDir, s.cfg.BackupDir, Options{Label: label, AgentVersion: s.cfg.AgentVersion, Now: s.cfg.Now})
	if err == nil {
		info.Path = RelativeArchivePath(s.cfg.DataDir, archive)
		if st, serr := os.Stat(archive); serr == nil {
			info.Size = st.Size()
		}
		if _, verr := s.cfg.Verify(archive); verr != nil {
			err = fmt.Errorf("archive failed verification: %w", verr)
		}
	}
	info.OK = err == nil
	if err != nil {
		info.Error = s.scrub(err)
		s.cfg.Logf("backup %s: %s", label, info.Error)
	}
	if rerr := RecordBackup(s.cfg.DataDir, info); rerr != nil {
		s.cfg.Logf("backup status: %v", rerr)
	}
	runErr := err
	if err == nil {
		if removed, perr := Prune(s.cfg.BackupDir, s.policy); perr != nil {
			runErr = fmt.Errorf("pruning old backups: %w", perr)
		} else if len(removed) > 0 {
			s.cfg.Logf("backup: pruned %d old archives", len(removed))
		}
		if s.cfg.Offsite != nil && (label == LabelDaily || label == LabelWeekly) {
			oerr := s.cfg.Offsite(archive)
			if rerr := RecordOffsite(s.cfg.DataDir, s.cfg.Now(), oerr, s.cfg.OffsiteDir); rerr != nil {
				s.cfg.Logf("backup status: %v", rerr)
			}
			if oerr != nil {
				runErr = fmt.Errorf("off-device copy: %w", oerr)
			}
		}
	}
	text := ""
	if runErr != nil {
		text = s.scrub(runErr)
		s.cfg.Logf("backup: %s", text)
	}
	s.note(func(i *SchedulerInfo) { i.LastRun = now; i.LastError = text })
}

// maybeTrial restores the newest archive into a scratch folder under the backup dir and runs Check on it when the
// last recorded trial is missing or older than TrialEvery. The scratch folder is always removed. The result is stored
// as last_verification.
func (s *Scheduler) maybeTrial() {
	if s.cfg.Check == nil {
		return
	}
	if rep, _ := Status(s.cfg.DataDir); rep.LastVerification != nil && s.cfg.Now().Sub(rep.LastVerification.Time) < TrialEvery {
		return
	}
	files, err := listArchives(s.cfg.BackupDir)
	if err != nil || len(files) == 0 {
		return
	}
	sort.Slice(files, func(i, j int) bool { return files[i].taken.After(files[j].taken) })
	newest := files[0].path
	scratch, err := os.MkdirTemp(s.cfg.BackupDir, ".trial-*")
	if err != nil {
		s.cfg.Logf("backup trial restore: %s", s.scrub(err))
		return
	}
	defer os.RemoveAll(scratch)
	_, err = s.cfg.Trial(newest, scratch, s.cfg.Check)
	v := VerificationInfo{Time: s.cfg.Now().UTC(), OK: err == nil, Archive: filepath.Base(newest)}
	if err != nil {
		v.Error = s.scrub(err, scratch)
		s.cfg.Logf("backup trial restore: %s", v.Error)
	}
	if rerr := UpdateStatus(s.cfg.DataDir, func(r *StatusReport) { r.LastVerification = &v }); rerr != nil {
		s.cfg.Logf("backup status: %v", rerr)
	}
}
