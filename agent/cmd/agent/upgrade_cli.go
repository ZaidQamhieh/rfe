package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/ZaidQamhieh/rfe/agent/internal/launcher"
	"github.com/ZaidQamhieh/rfe/agent/internal/store"
	"github.com/ZaidQamhieh/rfe/agent/internal/upgrade"
)

// thisBuildSchemaInfo describes the database in dir from this build's point of view, reading it read-only.
func thisBuildSchemaInfo(dir string) (launcher.SchemaInfo, error) {
	uv, minCompat, err := store.ReadSchemaState(dir)
	if err != nil {
		return launcher.SchemaInfo{}, err
	}
	return launcher.SchemaInfo{UserVersion: uv, MinCompatible: minCompat, SupportedLevel: store.SchemaVersion()}, nil
}

// cmdSchemaInfo implements "rfe schema-info [-data <dir>] [-json]": it prints the database's user_version and
// min_compatible and the highest min_compatible this build can open. It never migrates or writes the database. The
// launcher and "rfe downgrade" run it from the bundle of the version they are considering.
func cmdSchemaInfo(args []string) error {
	fs := newAdminFlags("schema-info", "schema-info [-data <dir>] [-json]")
	data := fs.String("data", "", "agent data dir")
	asJSON := fs.Bool("json", false, "print JSON")
	if _, err := fs.parse(args, 0, 0); err != nil {
		return err
	}
	info, err := thisBuildSchemaInfo(adminDataDir(*data))
	if err != nil {
		return err
	}
	if *asJSON {
		out, err := json.Marshal(info)
		if err != nil {
			return err
		}
		fmt.Println(string(out))
		return nil
	}
	fmt.Printf("user_version %d, min_compatible %d, this build opens up to %d (can open: %v)\n", info.UserVersion, info.MinCompatible, info.SupportedLevel, info.CanOpen())
	return nil
}

// restoreOptionsFor builds the options for restoring the newest premigration snapshot over dataDir, validated by
// this build's own schema check.
func restoreOptionsFor(dataDir, backups string) upgrade.RestoreOptions {
	return upgrade.RestoreOptions{DataDir: dataDir, BackupDir: backups, Validate: func(restored string) error {
		info, err := thisBuildSchemaInfo(restored)
		if err != nil {
			return err
		}
		if !info.CanOpen() {
			return fmt.Errorf("restored database needs level %d but this build supports %d", info.MinCompatible, info.SupportedLevel)
		}
		return nil
	}}
}

// cmdRollbackData implements "rfe rollback-data [-data <dir>] [-backups <dir>]", which the launcher runs from the
// previous version's bundle when a breaking migration has to be undone. The agent must not be running.
func cmdRollbackData(args []string) error {
	fs := newAdminFlags("rollback-data", "rollback-data [-data <dir>] [-backups <dir>]")
	data := fs.String("data", "", "agent data dir to replace")
	backups := fs.String("backups", "", "folder with the premigration snapshots (default <data dir>/backups)")
	if _, err := fs.parse(args, 0, 0); err != nil {
		return err
	}
	res, err := upgrade.RestoreSnapshot(restoreOptionsFor(adminDataDir(*data), *backups))
	if err != nil {
		return err
	}
	fmt.Printf("restored %s; the previous data dir was kept as %s (%d revocations re-applied)\n", res.Archive, res.OldDir, res.Revocations)
	return nil
}

// cmdDowngrade implements "rfe downgrade -root <dir> -to <version> [-data <dir>] [-backups <dir>]".
func cmdDowngrade(args []string) error {
	fs := newAdminFlags("downgrade", "downgrade -root <dir> -to <version> [-data <dir>] [-backups <dir>]")
	root := fs.String("root", "", "launcher install root (required)")
	to := fs.String("to", "", "installed version to go back to (required)")
	data := fs.String("data", "", "agent data dir")
	backups := fs.String("backups", "", "folder with the premigration snapshots (default <data dir>/backups)")
	if _, err := fs.parse(args, 0, 0); err != nil {
		return err
	}
	if *root == "" || *to == "" {
		return errors.New("downgrade needs -root and -to (usage: rfe downgrade -root <dir> -to <version>)")
	}
	absRoot, err := guardUpdateRoot(*root)
	if err != nil {
		return err
	}
	res, err := upgrade.Downgrade(upgrade.DowngradeOptions{Root: absRoot, DataDir: adminDataDir(*data), BackupDir: *backups, Target: *to,
		Logf: func(f string, a ...any) { fmt.Printf(f+"\n", a...) }})
	if err != nil {
		return err
	}
	if res.Restored {
		fmt.Printf("downgraded to %s with data restored from %s; the previous data dir was kept as %s\n", *to, res.Archive, res.OldDir)
	} else {
		fmt.Printf("downgraded to %s (version pointer only)\n", *to)
	}
	return nil
}

// cmdUpdateApply implements "rfe update apply -root <dir> -version <v> [-data <dir>] [-force]". It takes the
// premigration snapshot and records the staged version as pending; it cannot stop a running daemon, so the owner
// then restarts the agent and the launcher starts the pending version. Without -force it refuses while transfers run.
func cmdUpdateApply(args []string) error {
	fs := newAdminFlags("update apply", "update apply -root <dir> -version <v> [-data <dir>] [-backups <dir>] [-force]")
	root := fs.String("root", "", "launcher install root (required)")
	ver := fs.String("version", "", "staged version to switch to (required)")
	data := fs.String("data", "", "agent data dir")
	backups := fs.String("backups", "", "folder for the premigration snapshot (default <data dir>/backups)")
	force := fs.Bool("force", false, "switch even while transfers are running")
	if _, err := fs.parse(args, 0, 0); err != nil {
		return err
	}
	if *ver == "" {
		return fmt.Errorf("-version is required (usage: rfe %s)", updateUsage)
	}
	absRoot, err := guardUpdateRoot(*root)
	if err != nil {
		return err
	}
	dir := adminDataDir(*data)
	opt := upgrade.ApplyOptions{Root: absRoot, DataDir: dir, BackupDir: *backups, Version: *ver, AgentVersion: version,
		Logf: func(f string, a ...any) { fmt.Printf(f+"\n", a...) }}
	if !*force {
		db, err := openAdminStore(dir)
		if err != nil {
			return err
		}
		opt.Idle = upgrade.StoreIdle(db)
		busy := !opt.Idle(context.Background())
		if err := db.Close(); err != nil {
			return err
		}
		if busy {
			return fmt.Errorf("%w: transfers are in progress (use -force to switch anyway)", upgrade.ErrBusy)
		}
	}
	if err := upgrade.Apply(context.Background(), opt); err != nil {
		return err
	}
	fmt.Printf("version %s is pending; restart the agent (for example: rfe stop) and the launcher starts it\n", *ver)
	return nil
}

// updateDataDir is where the stage trial reads the live data from: -data, else the resolved default.
func updateDataDir(flagVal string) string {
	if abs, err := filepath.Abs(adminDataDir(flagVal)); err == nil {
		return abs
	}
	return adminDataDir(flagVal)
}
