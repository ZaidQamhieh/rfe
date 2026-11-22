package apps

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
)

// Limits of the Windows AppsFolder inventory (the old agent's).
const (
	maxWindowsApps         = 5000
	maxWindowsOutputBytes  = 4 << 20
	maxWindowsNameChars    = 256
	maxWindowsAUMIDChars   = 257
	appsFolderLaunchPrefix = `shell:AppsFolder\`
)

// safeAUMIDPattern accepts an application user model id: a package family and
// an optional "!app" part, plain characters only, so it cannot carry a switch,
// a path or a command when appended to the shell:AppsFolder moniker.
var safeAUMIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}(![A-Za-z0-9][A-Za-z0-9._-]{0,127})?$`)

// appsFolderInventoryScript reads the current user's AppsFolder and prints
// {"apps":[{"name":...,"aumid":...}]}. The caps are built from the Go constants
// so the PowerShell and Go limits cannot drift apart.
var appsFolderInventoryScript = `$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = [System.Text.UTF8Encoding]::new($false)
$folder = (New-Object -ComObject Shell.Application).NameSpace('shell:::{4234d49b-0245-4df3-b780-3893943456e1}')
if ($null -eq $folder) { throw 'AppsFolder unavailable' }
$rows = New-Object System.Collections.Generic.List[object]
foreach ($item in $folder.Items()) {
    $name = [string]$item.Name
    $aumid = [string]$item.Path
    if ($name.Length -gt ` + strconv.Itoa(maxWindowsNameChars) + `) { $name = $name.Substring(0, ` + strconv.Itoa(maxWindowsNameChars) + `) }
    if ($name.Length -gt 0 -and $aumid.Length -gt 0 -and $aumid.Length -le ` + strconv.Itoa(maxWindowsAUMIDChars) + `) {
        $rows.Add([pscustomobject]@{ name = $name; aumid = $aumid })
    }
    if ($rows.Count -ge ` + strconv.Itoa(maxWindowsApps) + `) { break }
}
$payload = [pscustomobject]@{ apps = $rows.ToArray() }
ConvertTo-Json -InputObject $payload -Compress -Depth 3
`

// windowsInventory is the JSON the script prints.
type windowsInventory struct {
	Apps []struct {
		Name  string `json:"name"`
		AUMID string `json:"aumid"`
	} `json:"apps"`
}

// parseWindowsInventory turns the script's output into sorted listings. Output
// over maxWindowsOutputBytes or malformed JSON is an error; empty output is an
// empty list. Rows with an unsafe or repeated AUMID or an empty name are
// dropped, and at most maxWindowsApps rows are read.
func parseWindowsInventory(output []byte) ([]Listing, error) {
	if len(output) > maxWindowsOutputBytes {
		return nil, ErrProviderMissing
	}
	var inv windowsInventory
	if len(strings.TrimSpace(string(output))) > 0 {
		if err := json.Unmarshal(output, &inv); err != nil {
			return nil, ErrProviderMissing
		}
	}
	rows := inv.Apps
	if len(rows) > maxWindowsApps {
		rows = rows[:maxWindowsApps]
	}
	apps := make([]Listing, 0, len(rows))
	seen := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		if strings.TrimSpace(row.AUMID) != row.AUMID || !safeAUMIDPattern.MatchString(row.AUMID) {
			continue
		}
		if _, dup := seen[row.AUMID]; dup {
			continue
		}
		seen[row.AUMID] = struct{}{}
		name := displayName(row.Name)
		if name == "" {
			continue
		}
		apps = append(apps, Listing{
			App: App{ID: makeID(windowsIDDomain, row.AUMID), Name: name, Launchable: true},
			Ref: row.AUMID,
		})
	}
	sortApps(apps)
	return apps, nil
}
