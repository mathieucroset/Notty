//go:build windows

package watcher

import (
	"encoding/binary"
	"errors"
	"path/filepath"
	"slices"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/fsnotify/fsnotify"
	"golang.org/x/sys/windows"
)

// notifyRecord is one FILE_NOTIFY_INFORMATION record.
type notifyRecord struct {
	action uint32
	name   string
}

// notifyBuffer lays records out as ReadDirectoryChangesW does.
func notifyBuffer(recs []notifyRecord) []byte {
	var buf []byte
	for i, r := range recs {
		name := utf16.Encode([]rune(r.name))
		size := 12 + 2*len(name)
		size = (size + 3) &^ 3 // DWORD-aligned
		rec := make([]byte, size)
		if i < len(recs)-1 {
			binary.LittleEndian.PutUint32(rec[0:], uint32(size))
		}
		binary.LittleEndian.PutUint32(rec[4:], r.action)
		binary.LittleEndian.PutUint32(rec[8:], uint32(2*len(name)))
		for j, c := range name {
			binary.LittleEndian.PutUint16(rec[12+2*j:], c)
		}
		buf = append(buf, rec...)
	}
	return buf
}

func TestDeliverDropsGitPaths(t *testing.T) {
	tests := []struct {
		name string
		recs []notifyRecord
		want []string // delivered names, relative to the root
	}{
		{"note", []notifyRecord{{windows.FILE_ACTION_MODIFIED, `a.md`}}, []string{`a.md`}},
		{"git directory itself", []notifyRecord{{windows.FILE_ACTION_ADDED, `.git`}}, nil},
		{"inside .git", []notifyRecord{{windows.FILE_ACTION_ADDED, `.git\objects\ab\cdef`}}, nil},
		{"inside a nested .git", []notifyRecord{{windows.FILE_ACTION_MODIFIED, `sub\.GIT\index`}}, nil},
		{"name merely containing .git", []notifyRecord{{windows.FILE_ACTION_ADDED, `x.gitignore`}}, []string{`x.gitignore`}},
		{"mixed batch", []notifyRecord{
			{windows.FILE_ACTION_MODIFIED, `.git\index`},
			{windows.FILE_ACTION_MODIFIED, `Work\b.md`},
			{windows.FILE_ACTION_REMOVED, `.git\refs\heads\main.lock`},
			{windows.FILE_ACTION_ADDED, `c.md`},
		}, []string{`Work\b.md`, `c.md`}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			b := &rdcwBackend{root: root, evC: make(chan fsnotify.Event, 16), done: make(chan struct{})}
			if !b.deliver(&dirWatch{dir: root, root: true}, notifyBuffer(tc.recs)) {
				t.Fatal("deliver reported the backend closed")
			}
			close(b.evC)
			var got []string
			for e := range b.evC {
				rel, err := filepath.Rel(root, e.Name)
				if err != nil {
					t.Fatalf("rel: %v", err)
				}
				got = append(got, rel)
			}
			if !slices.Equal(got, tc.want) {
				t.Fatalf("delivered %v, want %v", got, tc.want)
			}
		})
	}
}

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
