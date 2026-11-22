package backup

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// trialDataSubdir is the folder inside the scratch directory that a trial restore unpacks into.
const trialDataSubdir = "data"

// TrialRestore proves an archive can actually be restored and used: it unpacks archive into a scratch
// data directory (never any live one), then calls check with that directory, for example a selfcheck that
// migrates the database and opens it. The check is injected so this package needs nothing from the
// commands that use it. With tmpDir empty a fresh temporary directory is used and removed before returning;
// with tmpDir set the data is restored to tmpDir/data and left there for the caller to inspect and remove.
// The manifest is returned whenever the restore itself succeeded, even if check then failed.
func TrialRestore(archive, tmpDir string, check func(dataDir string) error) (*Manifest, error) {
	if check == nil {
		return nil, errors.New("trial restore needs a check to run")
	}
	root := tmpDir
	if root == "" {
		made, err := os.MkdirTemp("", "rfe-trial-*")
		if err != nil {
			return nil, err
		}
		defer os.RemoveAll(made)
		root = made
	}
	target := filepath.Join(root, trialDataSubdir)
	m, err := Restore(archive, target)
	if err != nil {
		return nil, fmt.Errorf("trial restore: %w", err)
	}
	if err := check(target); err != nil {
		return m, fmt.Errorf("trial restore of %s: check failed: %w", archive, err)
	}
	return m, nil
}
