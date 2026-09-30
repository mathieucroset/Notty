//go:build unix

package watcher

import "golang.org/x/sys/unix"

// openable opens p the way fsnotify's kqueue backend does to watch it, and
// closes it again.
func openable(p string) error {
	fd, err := unix.Open(p, openFlags, 0)
	if err != nil {
		return err
	}
	return unix.Close(fd)
}
