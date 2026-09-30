//go:build !(linux || openbsd || dragonfly || solaris || darwin || freebsd || netbsd)

package watcher

import "os"

// haveFileID is false where no inode/ctime is available (for example on
// Windows): self-writes are then identified by mtime and size only.
const haveFileID = false

// fileID is empty where the platform has no inode and status-change time.
type fileID struct{}

func fileIDOf(os.FileInfo) fileID { return fileID{} }
