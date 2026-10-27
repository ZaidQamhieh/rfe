package server

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/ZaidQamhieh/rfe/agent/internal/sidecar"
)

// SidecarStatsFile is where the running agent publishes sidecar counters for `rfe status`, which is a
// separate process and cannot ask the daemon over the authenticated API.
const SidecarStatsFile = "sidecar-stats.json"

const sidecarStatsEvery = 30 * time.Second

// PublishSidecarStats writes the sidecar counters to dataDir every 30 s while there is something to report
// (the file's age tells `status` whether the daemon is alive), until ctx ends.
func PublishSidecarStats(ctx context.Context, dataDir string) {
	write := func() {
		r := sidecar.Snapshot()
		if len(r.Sidecars) == 0 && len(r.Events) == 0 {
			return
		}
		b, err := json.Marshal(r)
		if err != nil {
			return
		}
		tmp := filepath.Join(dataDir, SidecarStatsFile+".tmp")
		if err := os.WriteFile(tmp, b, 0o600); err == nil {
			if err := os.Rename(tmp, filepath.Join(dataDir, SidecarStatsFile)); err != nil {
				log.Printf("sidecar stats: %v", err)
			}
		}
	}
	t := time.NewTicker(sidecarStatsEvery)
	defer t.Stop()
	// The first write lands before the sidecars have finished starting; a second one shortly after keeps `status`
	// from showing a healthy agent as "down" for half a minute.
	early := time.After(3 * time.Second)
	write()
	for {
		select {
		case <-ctx.Done():
			write()
			return
		case <-early:
			write()
		case <-t.C:
			write()
		}
	}
}

// ReadSidecarStats returns the counters the daemon last published, and how old they are.
func ReadSidecarStats(dataDir string) (sidecar.Report, time.Duration, error) {
	var r sidecar.Report
	b, err := os.ReadFile(filepath.Join(dataDir, SidecarStatsFile))
	if err != nil {
		return r, 0, err
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return r, 0, err
	}
	return r, time.Since(r.At), nil
}
