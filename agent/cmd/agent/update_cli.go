package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ZaidQamhieh/rfe/agent/internal/hostupdate"
	"github.com/ZaidQamhieh/rfe/agent/internal/store"
	"github.com/ZaidQamhieh/rfe/agent/internal/upgrade"
)

// updateUsage is the usage line of "rfe update".
const updateUsage = "update check|stage|apply [-root <dir>] [-url <manifest url>] [-installed <version>] [-force] [-db-size <bytes>]"

// liveDataMarkers are files only a live agent data dir holds; an update root that contains one is refused.
var liveDataMarkers = []string{store.DBFile, "agent-cert.pem", "agent-key.pem"}

// cmdUpdate implements "rfe update check" and "rfe update stage". Both only look at the release; stage also
// downloads, verifies and unpacks it under -root. Neither switches versions, runs the new agent or touches a data dir.
func cmdUpdate(args []string) error {
	if len(args) > 0 && args[0] == "apply" {
		return cmdUpdateApply(args[1:])
	}
	if len(args) == 0 || (args[0] != "check" && args[0] != "stage") {
		return fmt.Errorf("missing or unknown action (usage: rfe %s)", updateUsage)
	}
	action := args[0]
	fs := newAdminFlags("update "+action, updateUsage)
	root := fs.String("root", "", "launcher install root to stage into (required for stage); never a data dir")
	url := fs.String("url", "", "https URL of the signed release manifest (its signature is at <url>.sig)")
	installed := fs.String("installed", version, "installed version to compare against")
	force := fs.Bool("force", false, "allow a release equal to the installed version (never a lower one)")
	data := fs.String("data", "", "agent data dir whose snapshot the stage trial migrates (default: the resolved data dir)")
	dbSize := fs.Int64("db-size", 0, "agent database size in bytes, for the free-space check (2x bundle + 3x this)")
	if _, err := fs.parse(args[1:], 0, 0); err != nil {
		return err
	}
	cfg := hostupdate.Config{
		ManifestURL:   *url,
		Installed:     *installed,
		Force:         *force,
		DatabaseBytes: *dbSize,
		Logf:          func(format string, a ...any) { fmt.Printf(format+"\n", a...) },
	}
	if action == "stage" {
		// The staged bundle migrates a snapshot copy of the live data before it is accepted (never the live database).
		cfg.Trial = upgrade.TrialFunc(updateDataDir(*data), "", 0, cfg.Logf)
	}
	if *root != "" || action == "stage" {
		abs, err := guardUpdateRoot(*root)
		if err != nil {
			return err
		}
		cfg.Root = abs
	}
	ctx := context.Background()
	if action == "check" {
		plan, err := hostupdate.Check(ctx, cfg)
		if err != nil {
			return err
		}
		fmt.Printf("release %s (released %s) is available for %s/%s: %d bytes\n",
			plan.Release.Version, plan.Release.Released.Format("2006-01-02"), plan.Bundle.GOOS, plan.Bundle.GOARCH, plan.Bundle.Size)
		return nil
	}
	res, err := hostupdate.Stage(ctx, cfg)
	if err != nil {
		var te *hostupdate.Error
		if errors.As(err, &te) {
			return fmt.Errorf("update stage failed (%s): %w", te.Op, te.Err)
		}
		return err
	}
	if res.AlreadyStaged {
		fmt.Printf("version %s was already staged in %s\n", res.Version, res.Dir)
	} else {
		fmt.Printf("version %s staged in %s (not switched: the launcher decides when to run it)\n", res.Version, res.Dir)
	}
	return nil
}

// guardUpdateRoot returns root as an absolute path, refusing an empty one, the default data dir (or anything
// inside it) and any folder that holds agent data files, so staging can never write into a live data dir.
func guardUpdateRoot(root string) (string, error) {
	if strings.TrimSpace(root) == "" {
		return "", fmt.Errorf("-root is required (usage: rfe %s)", updateUsage)
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	if data, err := filepath.Abs(defaultDataDir()); err == nil {
		if rel, err := filepath.Rel(data, abs); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return "", fmt.Errorf("-root %s is the agent data dir or inside it; refusing to stage there", abs)
		}
	}
	for _, name := range liveDataMarkers {
		if _, err := os.Lstat(filepath.Join(abs, name)); err == nil {
			return "", fmt.Errorf("-root %s holds %s, so it looks like a live data dir; refusing to stage there", abs, name)
		}
	}
	return abs, nil
}
