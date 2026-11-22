//go:build linux

package apps

// platformProviders returns the Linux catalog (XDG desktop entries) and the gio
// launcher.
func platformProviders() (Lister, Launcher) {
	return newXDGLister(), newGioLauncher()
}
