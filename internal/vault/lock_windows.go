//go:build windows

package vault

import (
	"errors"

	"golang.org/x/sys/windows"
)

// stillActive is the exit code GetExitCodeProcess reports for a running
// process (STILL_ACTIVE).
const stillActive = 259

// processAlive reports whether a process with this PID is running. Access
// denied means it exists but belongs to someone else; an invalid parameter
// means there is no such process.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	switch {
	case errors.Is(err, windows.ERROR_ACCESS_DENIED):
		return true
	case errors.Is(err, windows.ERROR_INVALID_PARAMETER):
		return false
	case err != nil:
		return true // unknown failure: never take over a lock we cannot judge
	}
	defer windows.CloseHandle(h)
	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err != nil {
		return true
	}
	return code == stillActive
}
