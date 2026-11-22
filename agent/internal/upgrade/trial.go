package upgrade

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/backup"
	"github.com/ZaidQamhieh/rfe/agent/internal/launcher"
	"github.com/ZaidQamhieh/rfe/agent/internal/store"
)

// DefaultTrialTimeout bounds one trial run of a staged agent's selfcheck.
const DefaultTrialTimeout = 10 * time.Minute

const trialOutputLimit = 4 << 10

// TrialFunc returns the Trial callback for hostupdate.Config: it snapshots the live data with backup.Create (which
// reads the database through SQLite and never writes it), restores that snapshot into a temporary directory and runs
// "<bundle>/rfe selfcheck -data <tmp>" there, so the staged version's migrations are proven on a copy of the real
// data. The temporary directory is always removed. With no database in dataDir yet there is nothing to migrate
// and the trial passes.
func TrialFunc(dataDir, agentName string, timeout time.Duration, logf func(string, ...any)) func(bundleDir string) error {
	if agentName == "" {
		agentName = launcher.AgentFileName()
	}
	if timeout <= 0 {
		timeout = DefaultTrialTimeout
	}
	return func(bundleDir string) error {
		if _, err := os.Stat(filepath.Join(dataDir, store.DBFile)); errors.Is(err, os.ErrNotExist) {
			if logf != nil {
				logf("no database in %s: the trial has no data to migrate", dataDir)
			}
			return nil
		} else if err != nil {
			return err
		}
		tmp, err := os.MkdirTemp("", "rfe-trial-*")
		if err != nil {
			return err
		}
		defer os.RemoveAll(tmp)
		archive, _, err := backup.Create(dataDir, filepath.Join(tmp, "snapshot"), backup.Options{Label: "trial", AgentVersion: "trial"})
		if err != nil {
			return fmt.Errorf("snapshot live data: %w", err)
		}
		agent, err := filepath.Abs(filepath.Join(bundleDir, agentName))
		if err != nil {
			return err
		}
		_, err = backup.TrialRestore(archive, filepath.Join(tmp, "restore"), func(copyDir string) error {
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			defer cancel()
			var out bytes.Buffer
			cmd := exec.CommandContext(ctx, agent, "selfcheck", "-data", copyDir)
			cmd.Stdout, cmd.Stderr = &capWriter{b: &out}, &capWriter{b: &out}
			if err := cmd.Run(); err != nil {
				return fmt.Errorf("%s selfcheck: %w: %s", agentName, err, bytes.TrimSpace(out.Bytes()))
			}
			return nil
		})
		return err
	}
}

// capWriter keeps the first trialOutputLimit bytes written to it.
type capWriter struct{ b *bytes.Buffer }

func (w *capWriter) Write(p []byte) (int, error) {
	if room := trialOutputLimit - w.b.Len(); room > 0 {
		if len(p) < room {
			room = len(p)
		}
		w.b.Write(p[:room])
	}
	return len(p), nil
}
