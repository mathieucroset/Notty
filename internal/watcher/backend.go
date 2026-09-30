package watcher

import "github.com/fsnotify/fsnotify"

// backend is the operating system's change notification source. Add and
// Remove take absolute directory paths; events name absolute paths.
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
