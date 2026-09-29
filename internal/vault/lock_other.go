//go:build !unix && !windows

package vault

// processAlive cannot check processes on this platform, so it assumes the
// process is alive: a lock is then never taken over as stale.
func processAlive(pid int) bool { return pid > 0 }
