package watcher

import "github.com/fsnotify/fsnotify"

// backend is the operating system's change notification source. Add takes
// the absolute path of a directory, or of a file (gaps.go watches the files
// fsnotify's kqueue backend could not list); Remove takes the absolute path
// of a directory; events name absolute paths.
//
// On Windows it is one recursive watch of the vault root (see
// backend_windows.go); elsewhere it is fsnotify, with one watch per
// directory.
type backend interface {
	Add(dir string) error
	Remove(dir string) error
	Close() error
	events() <-chan fsnotify.Event
	errors() <-chan error
}
