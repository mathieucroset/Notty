//go:build unix && !darwin

package watcher

import "golang.org/x/sys/unix"

// openFlags are the flags fsnotify's kqueue backend opens watched paths with
// on the BSDs (elsewhere they only serve the tests).
const openFlags = unix.O_NONBLOCK | unix.O_RDONLY | unix.O_CLOEXEC
