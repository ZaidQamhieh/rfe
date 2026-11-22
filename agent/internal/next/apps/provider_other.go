//go:build !linux && !windows && !darwin

package apps

import (
	"context"
	"runtime"
)

// unsupported is the provider of a platform with no safe app catalog.
type unsupported struct{}

// List reports that the platform has no catalog.
func (unsupported) List(context.Context) (string, []Listing, error) {
	return runtime.GOOS, nil, ErrProviderUnsupported
}

// Launch reports that the platform cannot launch apps.
func (unsupported) Launch(context.Context, Listing) error { return ErrProviderUnsupported }

// platformProviders returns the placeholder provider for this platform.
func platformProviders() (Lister, Launcher) { return unsupported{}, unsupported{} }
