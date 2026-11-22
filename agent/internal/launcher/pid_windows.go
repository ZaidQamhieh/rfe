//go:build windows

package launcher

import "golang.org/x/sys/windows"

// stillActive is the exit code GetExitCodeProcess reports for a process that has not ended.
const stillActive = 259

// pidAlive reports whether a process with this pid is running. A process object outlives its process while any
// handle to it is open (antivirus, another tool), so opening it is not enough: the exit code must say it still runs.
func pidAlive(pid int) bool {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		// Access denied means the process exists but belongs to someone else; anything else means it is gone.
		return err == windows.ERROR_ACCESS_DENIED
	}
	defer windows.CloseHandle(h)
	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err != nil {
		return true
	}
	return code == stillActive
}
