package watcher

import "golang.org/x/sys/unix"

// openFlags are the flags fsnotify's kqueue backend opens watched paths with.
const openFlags = unix.O_EVTONLY | unix.O_CLOEXEC
