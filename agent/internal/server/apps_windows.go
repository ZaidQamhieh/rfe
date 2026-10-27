//go:build windows

package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const maxWindowsApps = 5000

var safeAUMIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}(![A-Za-z0-9][A-Za-z0-9._-]{0,127})?$`)

const appsFolderInventoryScript = `$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = [System.Text.UTF8Encoding]::new($false)
$folder = (New-Object -ComObject Shell.Application).NameSpace('shell:::{4234d49b-0245-4df3-b780-3893943456e1}')
if ($null -eq $folder) { throw 'AppsFolder unavailable' }
$rows = New-Object System.Collections.Generic.List[object]
foreach ($item in $folder.Items()) {
    $name = [string]$item.Name
    $aumid = [string]$item.Path
    if ($name.Length -gt 256) { $name = $name.Substring(0, 256) }
    if ($name.Length -gt 0 -and $aumid.Length -gt 0 -and $aumid.Length -le 257) {
        $rows.Add([pscustomobject]@{ name = $name; aumid = $aumid })
    }
    if ($rows.Count -ge 5000) { break }
}
$payload = [pscustomobject]@{ apps = $rows.ToArray() }
ConvertTo-Json -InputObject $payload -Compress -Depth 3
`

type windowsAppInventoryRow struct {
	Name  string `json:"name"`
	AUMID string `json:"aumid"`
}

type windowsAppInventory struct {
	Apps []windowsAppInventoryRow `json:"apps"`
}

func listHostApps() (string, []appRecord, error) {
	powershell, err := windowsPowerShellPath()
	if err != nil {
		return "windows", nil, errLauncherMissing
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, powershell,
		"-NoLogo", "-NoProfile", "-NonInteractive", "-Command", appsFolderInventoryScript)
	cmd.Stderr = io.Discard
	output, err := cmd.Output()
	if err != nil || len(output) > 4<<20 {
		return "windows", nil, errLauncherMissing
	}

	var inventory windowsAppInventory
	if len(strings.TrimSpace(string(output))) > 0 {
		if err := json.Unmarshal(output, &inventory); err != nil {
			return "windows", nil, errLauncherMissing
		}
	}
	rows := inventory.Apps
	if len(rows) > maxWindowsApps {
		rows = rows[:maxWindowsApps]
	}

	apps := make([]appRecord, 0, len(rows))
	seenAUMIDs := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		aumid := row.AUMID
		if strings.TrimSpace(aumid) != aumid {
			continue
		}
		if !safeAUMIDPattern.MatchString(aumid) {
			continue
		}
		if _, exists := seenAUMIDs[aumid]; exists {
			continue
		}
		seenAUMIDs[aumid] = struct{}{}
		name := safeAppDisplay(row.Name)
		if name == "" {
			continue
		}
		apps = append(apps, appRecord{
			ID:         windowsAppID(aumid),
			Name:       name,
			Launchable: true,
			launchRef:  aumid,
		})
	}
	sort.Slice(apps, func(i, j int) bool {
		left, right := strings.ToLower(apps[i].Name), strings.ToLower(apps[j].Name)
		if left == right {
			return apps[i].ID < apps[j].ID
		}
		return left < right
	})
	return "windows", apps, nil
}

func launchHostApp(_ context.Context, requested appRecord) error {
	if !requested.Launchable {
		return errAppNotLaunchable
	}
	if !windowsHasActiveInteractiveSession() {
		return errNoDesktopSession
	}

	// The ID from the HTTP path is resolved against a fresh current-user
	// AppsFolder snapshot before the AUMID is used. The request never supplies
	// an AUMID, filesystem path, command, or arguments.
	_, currentApps, err := listHostApps()
	if err != nil {
		return err
	}
	var current *appRecord
	for i := range currentApps {
		if currentApps[i].ID == requested.ID {
			current = &currentApps[i]
			break
		}
	}
	if current == nil {
		return errAppNotFound
	}
	if !current.Launchable {
		return errAppNotLaunchable
	}
	if !safeAUMIDPattern.MatchString(current.launchRef) {
		return errAppNotFound
	}

	windowsDir, err := windows.GetWindowsDirectory()
	if err != nil {
		return errLauncherMissing
	}
	explorer := filepath.Join(windowsDir, "explorer.exe")
	info, err := os.Stat(explorer)
	if err != nil || !info.Mode().IsRegular() {
		return errLauncherMissing
	}

	cmd := exec.Command(explorer, `shell:AppsFolder\`+current.launchRef)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return errAppStartFailed
	}
	// Explorer accepts the activation request and may remain running. Release
	// the process handle without waiting on the interactive shell lifetime.
	_ = cmd.Process.Release()
	return nil
}

func windowsAppID(aumid string) string {
	sum := sha256.Sum256([]byte("rfe-host-app-windows-v1\x00" + aumid))
	return "app_" + hex.EncodeToString(sum[:])
}

func windowsPowerShellPath() (string, error) {
	systemDir, err := windows.GetSystemDirectory()
	if err != nil {
		return "", err
	}
	path := filepath.Join(systemDir, "WindowsPowerShell", "v1.0", "powershell.exe")
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return "", os.ErrNotExist
	}
	return path, nil
}

func windowsHasActiveInteractiveSession() bool {
	var currentSession uint32
	if windows.ProcessIdToSessionId(windows.GetCurrentProcessId(), &currentSession) != nil || currentSession == 0 {
		return false
	}

	var sessions *windows.WTS_SESSION_INFO
	var count uint32
	if windows.WTSEnumerateSessions(0, 0, 1, &sessions, &count) != nil || sessions == nil {
		return false
	}
	defer windows.WTSFreeMemory(uintptr(unsafe.Pointer(sessions)))
	for _, session := range unsafe.Slice(sessions, count) {
		if session.SessionID == currentSession && session.State == windows.WTSActive {
			return true
		}
	}
	return false
}
