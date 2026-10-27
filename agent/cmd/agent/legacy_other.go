//go:build !linux && !windows

package main

// retireLegacyService has nothing to retire here: the macOS launchd label did not change.
func retireLegacyService(daemon bool) (bool, error) {
	return true, nil
}
