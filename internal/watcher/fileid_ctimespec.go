//go:build darwin || freebsd || netbsd

package watcher

import (
	"os"
	"syscall"
)

const haveFileID = true

// fileIDOf returns the inode and status-change time of info.
func fileIDOf(info os.FileInfo) fileID {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fileID{}
	}
	return fileID{ino: uint64(st.Ino), ctime: st.Ctimespec.Nano()}
}
