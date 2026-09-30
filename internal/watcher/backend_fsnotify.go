//go:build !windows

package watcher

import (
	"fmt"

	"github.com/fsnotify/fsnotify"
)

// fsnotifyBackend watches each directory with its own fsnotify watch.
type fsnotifyBackend struct{ *fsnotify.Watcher }

func newBackend(string) (backend, error) {
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("watcher: create fsnotify watcher: %w", err)
	}
	return fsnotifyBackend{fw}, nil
}

func (b fsnotifyBackend) events() <-chan fsnotify.Event { return b.Events }
func (b fsnotifyBackend) errors() <-chan error          { return b.Errors }
