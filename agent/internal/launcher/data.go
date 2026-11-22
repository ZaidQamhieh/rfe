package launcher

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"time"
)

// SchemaInfo is what an agent binary reports about a database with "rfe schema-info": the database's
// user_version and min_compatible, and the highest min_compatible this binary can still open (its supported
// level). Only the agent knows the schema, so the launcher asks it instead of opening the database itself.
type SchemaInfo struct {
	UserVersion    int `json:"user_version"`
	MinCompatible  int `json:"min_compatible"`
	SupportedLevel int `json:"supported_level"`
}

// CanOpen reports whether the binary that produced the info can open the database it described.
func (i SchemaInfo) CanOpen() bool { return i.MinCompatible <= i.SupportedLevel }

// DataTool does the database work a rollback needs, by running the agent binary of the version to roll back TO.
// The launcher never imports a database driver; this is its only door to the data.
type DataTool interface {
	// Schema returns what the agent in agentDir says about the database in dataDir.
	Schema(ctx context.Context, agentDir, dataDir string) (SchemaInfo, error)
	// RestoreSnapshot asks the agent in agentDir to replace dataDir with the newest premigration snapshot (into a new
	// directory, swapped in by rename; the old one is kept).
	RestoreSnapshot(ctx context.Context, agentDir, dataDir string) error
}

// ExecDataTool implements DataTool by running "<agentDir>/<AgentName> schema-info|rollback-data -data <dir>".
type ExecDataTool struct {
	AgentName string        // default AgentFileName()
	Timeout   time.Duration // per command, default 5 minutes
	Env       []string      // nil means the launcher's own environment
}

const (
	defaultDataToolTimeout = 5 * time.Minute
	maxToolOutput          = 64 << 10
)

// run executes one subcommand of the agent in agentDir and returns its standard output.
func (t ExecDataTool) run(ctx context.Context, agentDir string, args ...string) ([]byte, error) {
	name := t.AgentName
	if name == "" {
		name = AgentFileName()
	}
	timeout := t.Timeout
	if timeout <= 0 {
		timeout = defaultDataToolTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if abs, err := filepath.Abs(agentDir); err == nil {
		agentDir = abs
	}
	cmd := exec.CommandContext(ctx, filepath.Join(agentDir, name), args...)
	cmd.Env = t.Env
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &limitedWriter{b: &out}, &limitedWriter{b: &errb}
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%s %s: %w: %s", name, args[0], err, bytes.TrimSpace(errb.Bytes()))
	}
	return out.Bytes(), nil
}

// Schema runs "schema-info -json" and decodes its answer.
func (t ExecDataTool) Schema(ctx context.Context, agentDir, dataDir string) (SchemaInfo, error) {
	out, err := t.run(ctx, agentDir, "schema-info", "-data", dataDir, "-json")
	if err != nil {
		return SchemaInfo{}, err
	}
	var info SchemaInfo
	if err := json.Unmarshal(out, &info); err != nil {
		return SchemaInfo{}, fmt.Errorf("schema-info output is not JSON: %w", err)
	}
	return info, nil
}

// RestoreSnapshot runs "rollback-data -data <dir>".
func (t ExecDataTool) RestoreSnapshot(ctx context.Context, agentDir, dataDir string) error {
	_, err := t.run(ctx, agentDir, "rollback-data", "-data", dataDir)
	return err
}

// limitedWriter keeps at most maxToolOutput bytes and discards the rest, so a runaway child cannot fill memory.
type limitedWriter struct{ b *bytes.Buffer }

func (w *limitedWriter) Write(p []byte) (int, error) {
	if room := maxToolOutput - w.b.Len(); room > 0 {
		if len(p) < room {
			room = len(p)
		}
		w.b.Write(p[:room])
	}
	return len(p), nil
}
