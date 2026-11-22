//go:build windows

package apps

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// windowsInventoryTimeout bounds the PowerShell inventory run.
const windowsInventoryTimeout = 10 * time.Second

// windowsProvider lists the current user's AppsFolder through PowerShell and
// launches an entry through explorer.exe.
type windowsProvider struct{}

// platformProviders returns the Windows catalog and launcher.
func platformProviders() (Lister, Launcher) { return windowsProvider{}, windowsProvider{} }

// List runs the fixed inventory script (no client text in it) and parses the
// JSON it prints. A missing PowerShell, a failed run or bad output is
// ErrProviderMissing.
func (windowsProvider) List(ctx context.Context) (string, []Listing, error) {
	powershell, err := systemFile("WindowsPowerShell", "v1.0", "powershell.exe")
	if err != nil {
		return PlatformWindows, nil, ErrProviderMissing
	}
	runCtx, cancel := context.WithTimeout(ctx, windowsInventoryTimeout)
	defer cancel()
	cmd := exec.CommandContext(runCtx, powershell,
		"-NoLogo", "-NoProfile", "-NonInteractive", "-Command", appsFolderInventoryScript)
	cmd.Stderr = io.Discard
	output, err := cmd.Output()
	if err != nil {
		return PlatformWindows, nil, ErrProviderMissing
	}
	apps, err := parseWindowsInventory(output)
	if err != nil {
		return PlatformWindows, nil, err
	}
	return PlatformWindows, apps, nil
}

// Launch starts "explorer.exe shell:AppsFolder\<AUMID>" for an AUMID that
// passed the strict pattern, without a shell and without elevation. The handle
// is released at once: Explorer may keep running.
func (windowsProvider) Launch(_ context.Context, app Listing) error {
	if !app.Launchable {
		return ErrNotLaunchable
	}
	if !windowsHasActiveSession() {
		return ErrNoDesktopSession
	}
	if !safeAUMIDPattern.MatchString(app.Ref) {
		return ErrGone
	}
	windowsDir, err := windows.GetWindowsDirectory()
	if err != nil {
		return ErrProviderMissing
	}
	explorer := filepath.Join(windowsDir, "explorer.exe")
	if info, err := os.Stat(explorer); err != nil || !info.Mode().IsRegular() {
		return ErrProviderMissing
	}
	cmd := exec.Command(explorer, appsFolderLaunchPrefix+app.Ref)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return ErrStartFailed
	}
	_ = cmd.Process.Release()
	return nil
}

// systemFile returns the path of a regular file under the Windows system
// directory.
func systemFile(parts ...string) (string, error) {
	dir, err := windows.GetSystemDirectory()
	if err != nil {
		return "", err
	}
	path := filepath.Join(append([]string{dir}, parts...)...)
	if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
		return "", os.ErrNotExist
	}
	return path, nil
}

// windowsHasActiveSession reports whether this process runs in an active,
// non-service (session 0) interactive session.
func windowsHasActiveSession() bool {
	var current uint32
	if windows.ProcessIdToSessionId(windows.GetCurrentProcessId(), &current) != nil || current == 0 {
		return false
	}
	var sessions *windows.WTS_SESSION_INFO
	var count uint32
	if windows.WTSEnumerateSessions(0, 0, 1, &sessions, &count) != nil || sessions == nil {
		return false
	}
	defer windows.WTSFreeMemory(uintptr(unsafe.Pointer(sessions)))
	for _, session := range unsafe.Slice(sessions, count) {
		if session.SessionID == current && session.State == windows.WTSActive {
			return true
		}
	}
	return false
}
