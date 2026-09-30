//go:build windows

package watcher

import (
	"errors"
	"testing"
	"time"

	"github.com/fsnotify/fsnotify"
	"golang.org/x/sys/windows"
)

func TestCancelledReadRestartsWatch(t *testing.T) {
	root := t.TempDir()
	w := newTestWatcher(t, root)
	b := w.fsw.(*rdcwBackend)

	// Cancel the root's read the way the exit of the thread that issued it
	// would.
	b.mu.Lock()
	for dw := range b.running {
		if dw.root {
			if err := windows.CancelIoEx(dw.h, nil); err != nil {
				b.mu.Unlock()
				t.Fatalf("CancelIoEx: %v", err)
			}
		}
	}
	b.mu.Unlock()

	select {
	case err := <-w.Errors():
		if !errors.Is(err, fsnotify.ErrEventOverflow) || errors.Is(err, ErrWatchStopped) {
			t.Fatalf("error = %v, want lost events after a restart", err)
		}
	case <-time.After(waitTimeout):
		t.Fatal("no error reported for the cancelled watch")
	}

	writeFile(t, root, "after.md", "x")
	collectUntil(t, w, "after.md")
}

func TestCloseCancelsReadsWhenStopEventFails(t *testing.T) {
	prev := setEvent
	setEvent = func(windows.Handle) error { return windows.ERROR_INVALID_HANDLE }
	t.Cleanup(func() { setEvent = prev })

	w, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	closed := make(chan error, 1)
	go func() { closed <- w.Close() }()
	select {
	case err := <-closed:
		if !errors.Is(err, windows.ERROR_INVALID_HANDLE) {
			t.Fatalf("Close = %v, want the SetEvent error", err)
		}
	case <-time.After(waitTimeout):
		t.Fatal("Close did not return")
	}
	if _, ok := <-w.Events(); ok {
		t.Fatal("Events not closed")
	}
}
