//go:build !linux && !windows && !darwin

package server

import (
	"context"
	"runtime"
)

func listHostApps() (string, []appRecord, error) {
	return runtime.GOOS, nil, errAppUnsupported
}

func launchHostApp(context.Context, appRecord) error {
	return errAppUnsupported
}
