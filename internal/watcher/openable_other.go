//go:build !unix

package watcher

// openable is only needed with fsnotify's kqueue backend, on unix systems.
func openable(string) error { return nil }
