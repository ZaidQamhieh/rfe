package upgrade

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/bundle"
	"github.com/ZaidQamhieh/rfe/agent/internal/launcher"
)

// ErrLauncherRunning means a launcher holds the root's lock, so the version pointer cannot be changed safely.
var ErrLauncherRunning = errors.New("a launcher is running for this root; stop it first")

// SchemaFunc asks the agent binary in agentDir what it thinks of the database in dataDir.
type SchemaFunc func(agentDir, dataDir string) (launcher.SchemaInfo, error)

// ExecSchema is the default SchemaFunc: it runs "<agentDir>/rfe schema-info -json -data <dataDir>".
func ExecSchema(agentName string) SchemaFunc {
	tool := launcher.ExecDataTool{AgentName: agentName}
	return func(agentDir, dataDir string) (launcher.SchemaInfo, error) {
		return tool.Schema(context.Background(), agentDir, dataDir)
	}
}

// DowngradeOptions says what Downgrade does.
type DowngradeOptions struct {
	Root      string     // install root
	DataDir   string     // agent data dir
	BackupDir string     // snapshots; default <DataDir>/backups
	Target    string     // installed version to go back to
	AgentName string     // agent file name in a bundle (default launcher.AgentFileName())
	Schema    SchemaFunc // default ExecSchema(AgentName)
	Now       func() time.Time
	Logf      func(format string, args ...any)
}

// DowngradeResult says how the downgrade was done.
type DowngradeResult struct {
	Restored bool   // false: only the version pointer moved
	Archive  string // the snapshot restored, when Restored
	OldDir   string // the data dir that was set aside, when Restored
}

// Downgrade makes Target the current version while no launcher runs. It refuses while the launcher lock is held,
// when Target is bad, missing or fails bundle verification. The target's own binary reports whether it can open the
// database: if so only the pointer flips; if not the newest premigration snapshot is restored into a new data
// directory (validated with the target's own check) and swapped in, then the pointer flips. The lock is held for the
// whole operation so no launcher can start in the middle.
func Downgrade(opt DowngradeOptions) (*DowngradeResult, error) {
	logf := opt.Logf
	if logf == nil {
		logf = func(string, ...any) {}
	}
	name := opt.AgentName
	if name == "" {
		name = launcher.AgentFileName()
	}
	schema := opt.Schema
	if schema == nil {
		schema = ExecSchema(name)
	}
	st := launcher.NewStore(opt.Root)
	lock, err := launcher.AcquireLock(st.LockPath())
	if err != nil {
		if errors.Is(err, launcher.ErrLocked) {
			return nil, fmt.Errorf("%w: %v", ErrLauncherRunning, err)
		}
		return nil, err
	}
	defer lock.Release()
	state, err := st.Read()
	if err != nil {
		return nil, err
	}
	switch {
	case state.Current == "":
		return nil, errors.New("no current version recorded: nothing to downgrade from")
	case opt.Target == state.Current:
		return nil, fmt.Errorf("%s is already the current version", opt.Target)
	case state.IsBad(opt.Target):
		return nil, fmt.Errorf("version %s is marked bad and will not be used", opt.Target)
	}
	dir, err := st.VersionDir(opt.Target)
	if err != nil {
		return nil, err
	}
	if _, err := bundle.Verify(dir); err != nil {
		return nil, fmt.Errorf("target bundle %s does not verify: %w", dir, err)
	}
	if err := bundle.VerifyFile(dir, name); err != nil {
		return nil, fmt.Errorf("target agent binary: %w", err)
	}
	info, err := schema(dir, opt.DataDir)
	if err != nil {
		return nil, fmt.Errorf("ask %s about the database: %w", opt.Target, err)
	}
	res := &DowngradeResult{}
	if info.CanOpen() {
		logf("database (min_compatible %d) is readable by %s (level %d): moving the pointer only", info.MinCompatible, opt.Target, info.SupportedLevel)
	} else {
		logf("database min_compatible %d is above %s's level %d: restoring the premigration snapshot", info.MinCompatible, opt.Target, info.SupportedLevel)
		rr, err := RestoreSnapshot(RestoreOptions{DataDir: opt.DataDir, BackupDir: opt.BackupDir, Now: opt.Now,
			Validate: func(restored string) error {
				ri, err := schema(dir, restored)
				if err != nil {
					return err
				}
				if !ri.CanOpen() {
					return fmt.Errorf("even the snapshot needs level %d but %s supports %d", ri.MinCompatible, opt.Target, ri.SupportedLevel)
				}
				return nil
			}})
		if err != nil {
			return nil, err
		}
		res.Restored, res.Archive, res.OldDir = true, rr.Archive, rr.OldDir
	}
	if _, err := st.Apply(launcher.EvDowngrade, opt.Target); err != nil {
		return res, fmt.Errorf("move the version pointer: %w", err)
	}
	return res, nil
}
