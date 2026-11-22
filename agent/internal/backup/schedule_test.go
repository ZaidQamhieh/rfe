package backup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeClock is a settable clock for the scheduler.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *fakeClock) Add(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

func newClock() *fakeClock { return &fakeClock{t: time.Date(2026, 3, 1, 3, 0, 0, 0, time.UTC)} }

// stubScheduler is a scheduler whose Create only writes an empty file named like a real archive.
func stubScheduler(t *testing.T, clock *fakeClock, created *[]string) (*Scheduler, string) {
	t.Helper()
	data := t.TempDir()
	var mu sync.Mutex
	s := NewScheduler(SchedulerConfig{
		DataDir: data, Now: clock.Now,
		Create: func(dataDir, dest string, opt Options) (string, *Manifest, error) {
			if err := os.MkdirAll(dest, 0o700); err != nil {
				return "", nil, err
			}
			p := filepath.Join(dest, ArchiveName(opt.Label, opt.Now()))
			mu.Lock()
			*created = append(*created, opt.Label)
			mu.Unlock()
			return p, &Manifest{}, os.WriteFile(p, []byte("x"), 0o600)
		},
		Verify: func(string) (*Manifest, error) { return &Manifest{}, nil },
	})
	return s, data
}

func TestRunDueTakesDailyAndWeeklyOnlyWhenDue(t *testing.T) {
	clock := newClock()
	var created []string
	s, data := stubScheduler(t, clock, &created)
	ctx := context.Background()

	s.RunDue(ctx)
	if got := strings.Join(created, ","); got != "daily,weekly" {
		t.Fatalf("first run created %q", got)
	}
	clock.Add(time.Hour)
	s.RunDue(ctx)
	if len(created) != 2 {
		t.Fatalf("nothing is due after an hour, created %v", created)
	}
	clock.Add(24 * time.Hour)
	s.RunDue(ctx)
	if got := strings.Join(created[2:], ","); got != "daily" {
		t.Fatalf("after a day only daily is due, created %q", got)
	}
	clock.Add(7 * 24 * time.Hour)
	s.RunDue(ctx)
	if got := strings.Join(created[3:], ","); got != "daily,weekly" {
		t.Fatalf("after a week both are due, created %q", got)
	}
	rep, err := Status(data)
	if err != nil || rep.LastBackup == nil || !rep.LastBackup.OK || rep.LastBackup.Label != "weekly" || rep.LastBackup.Size != 1 {
		t.Fatalf("status = %+v, %v", rep.LastBackup, err)
	}
	if !strings.HasPrefix(rep.LastBackup.Path, "backups/rfe-backup-weekly-") {
		t.Errorf("path %q should be relative to the data dir", rep.LastBackup.Path)
	}
}

func TestRetentionKeepsSevenDailyAndFourWeekly(t *testing.T) {
	clock := newClock()
	var created []string
	s, data := stubScheduler(t, clock, &created)
	for i := 0; i < 60; i++ {
		s.RunDue(context.Background())
		clock.Add(DailyEvery)
	}
	dir := filepath.Join(data, SubdirName)
	if n := count(t, dir, LabelDaily); n != 7 {
		t.Errorf("daily kept %d, want 7", n)
	}
	if n := count(t, dir, LabelWeekly); n != 4 {
		t.Errorf("weekly kept %d, want 4", n)
	}
}

func TestTriggerNeverRunsInlineAndCoalescesPerLabel(t *testing.T) {
	clock := newClock()
	var created []string
	s, data := stubScheduler(t, clock, &created)
	for _, l := range []string{LabelPair, LabelPair, LabelRevoke, LabelPair, "Bad Label!", ""} {
		s.Trigger(l)
	}
	if len(created) != 0 {
		t.Fatalf("Trigger must not create a backup itself, created %v", created)
	}
	s.RunPending(context.Background())
	if got := strings.Join(created, ","); got != "pair,revoke" {
		t.Fatalf("created %q, want one backup per distinct valid label", got)
	}
	s.RunPending(context.Background())
	if len(created) != 2 {
		t.Fatalf("pending was not cleared: %v", created)
	}
	if rep, _ := Status(data); rep.LastBackup == nil || rep.LastBackup.Label != LabelRevoke {
		t.Errorf("last backup = %+v", rep.LastBackup)
	}
}

func TestRunServesTriggersUntilCancelled(t *testing.T) {
	clock := newClock()
	var created []string
	s, data := stubScheduler(t, clock, &created)
	got := make(chan string, 8)
	inner := s.cfg.Create
	s.cfg.Create = func(d, dest string, opt Options) (string, *Manifest, error) {
		got <- opt.Label
		return inner(d, dest, opt)
	}
	s.cfg.Sleep = func(ctx context.Context, d time.Duration) bool { return ctx.Err() == nil } // no real wait
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { s.Run(ctx); close(done) }()
	<-got // daily
	<-got // weekly
	s.Trigger(LabelPair)
	select {
	case l := <-got:
		if l != LabelPair {
			t.Fatalf("got %q", l)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("trigger was not served")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
	if rep, _ := Status(data); rep.Scheduler.Enabled {
		t.Errorf("scheduler still marked enabled after Run returned")
	}
}

func TestErrorsAreRecordedScrubbedAndNeverRaised(t *testing.T) {
	clock := newClock()
	data := t.TempDir()
	s := NewScheduler(SchedulerConfig{DataDir: data, Now: clock.Now,
		Create: func(dataDir, dest string, opt Options) (string, *Manifest, error) {
			return "", nil, &os.PathError{Op: "open", Path: filepath.Join(dataDir, "agent.db"), Err: errors.New("disk on fire")}
		}})
	s.RunDue(context.Background())
	rep, err := Status(data)
	if err != nil {
		t.Fatal(err)
	}
	if rep.LastBackup == nil || rep.LastBackup.OK || !strings.Contains(rep.LastBackup.Error, "disk on fire") {
		t.Fatalf("last_backup = %+v", rep.LastBackup)
	}
	if strings.Contains(rep.LastBackup.Error, data) || strings.Contains(rep.Scheduler.LastError, data) {
		t.Errorf("status leaks the data dir path: %+v", rep)
	}
	if rep.Scheduler.LastError == "" {
		t.Errorf("scheduler last_error not set")
	}
}

func TestVerifyFailureMarksBackupNotOK(t *testing.T) {
	clock := newClock()
	var created []string
	s, data := stubScheduler(t, clock, &created)
	s.cfg.Verify = func(string) (*Manifest, error) { return nil, errors.New("hash mismatch") }
	s.RunPending(context.Background())
	s.Trigger(LabelPair)
	s.RunPending(context.Background())
	if rep, _ := Status(data); rep.LastBackup == nil || rep.LastBackup.OK || !strings.Contains(rep.LastBackup.Error, "hash mismatch") {
		t.Fatalf("last_backup = %+v", rep.LastBackup)
	}
}

func TestWeeklyTrialRestoreOfNewestArchive(t *testing.T) {
	dir, _ := newDataDir(t)
	clock := newClock()
	var checked []string
	var failCheck bool
	s := NewScheduler(SchedulerConfig{DataDir: dir, Now: clock.Now, AgentVersion: "t",
		Check: func(restored string) error {
			if _, err := os.Stat(filepath.Join(restored, "agent.db")); err != nil {
				return err
			}
			checked = append(checked, restored)
			if failCheck {
				return errors.New("selfcheck: schema drift")
			}
			return nil
		}})
	ctx := context.Background()
	s.RunDue(ctx)
	rep, _ := Status(dir)
	if len(checked) != 1 || rep.LastVerification == nil || !rep.LastVerification.OK || !strings.HasPrefix(rep.LastVerification.Archive, "rfe-backup-") {
		t.Fatalf("checked=%v verification=%+v", checked, rep.LastVerification)
	}
	if _, err := os.Stat(checked[0]); err == nil {
		t.Errorf("the restored copy %s was not cleaned up", checked[0])
	}
	if rep.LastBackup == nil || !rep.LastBackup.OK {
		t.Fatalf("real backup failed: %+v", rep.LastBackup)
	}

	clock.Add(DailyEvery)
	s.RunDue(ctx)
	if len(checked) != 1 {
		t.Fatalf("trial ran again within a week: %v", checked)
	}
	clock.Add(TrialEvery)
	failCheck = true
	s.RunDue(ctx)
	rep, _ = Status(dir)
	if len(checked) != 2 || rep.LastVerification.OK || !strings.Contains(rep.LastVerification.Error, "schema drift") {
		t.Fatalf("checked=%d verification=%+v", len(checked), rep.LastVerification)
	}
	if strings.Contains(rep.LastVerification.Error, dir) {
		t.Errorf("verification error leaks the data dir: %s", rep.LastVerification.Error)
	}
	left, _ := filepath.Glob(filepath.Join(dir, SubdirName, ".trial-*"))
	if len(left) != 0 {
		t.Errorf("scratch folders left behind: %v", left)
	}
}

func TestOffsiteRunsForDailyAndWeeklyOnlyAndFailureIsRecorded(t *testing.T) {
	clock := newClock()
	var created, copied []string
	s, data := stubScheduler(t, clock, &created)
	s.cfg.OffsiteDir = filepath.Join(t.TempDir(), "usb")
	var fail bool
	s.cfg.Offsite = func(archive string) error {
		copied = append(copied, filepath.Base(archive))
		if fail {
			return errors.New("write " + s.cfg.OffsiteDir + ": no space left")
		}
		return nil
	}
	s.RunDue(context.Background())
	s.Trigger(LabelPair)
	s.RunPending(context.Background())
	if len(copied) != 2 {
		t.Fatalf("copied %v, want daily and weekly only", copied)
	}
	if rep, _ := Status(data); !rep.Offsite.Configured || !rep.Offsite.OK || rep.Offsite.LastTime.IsZero() {
		t.Fatalf("offsite = %+v", rep.Offsite)
	}
	fail = true
	clock.Add(DailyEvery)
	s.RunDue(context.Background())
	rep, _ := Status(data)
	if rep.Offsite.OK || !strings.Contains(rep.Offsite.Error, "no space left") || strings.Contains(rep.Offsite.Error, s.cfg.OffsiteDir) {
		t.Fatalf("offsite = %+v", rep.Offsite)
	}
}
