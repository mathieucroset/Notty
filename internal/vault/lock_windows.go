//go:build windows

package vault

import "os"

// processAlive reports whether a process with this PID exists. On Windows
// os.FindProcess opens the process and fails if there is none.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	_ = p.Release()
	return true
}
