//go:build linux || openbsd || dragonfly || solaris

package watcher

import (
	"os"
	"syscall"
)

const haveFileID = true

// fileID is a file's inode and status-change time.
type fileID struct {
	ino   uint64
	ctime int64 // nanoseconds
}

// fileIDOf returns the inode and status-change time of info.
func fileIDOf(info os.FileInfo) fileID {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fileID{}
	}
	return fileID{ino: st.Ino, ctime: st.Ctim.Nano()}
}
