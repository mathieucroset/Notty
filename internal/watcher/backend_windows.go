//go:build windows

package watcher

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/fsnotify/fsnotify"
	"golang.org/x/sys/windows"
)

// rdcwBufSize is the size of each ReadDirectoryChangesW buffer. 64 KiB is
// also the most a network share accepts.
const rdcwBufSize = 64 * 1024

// rdcwBackend watches the whole vault with one recursive
// ReadDirectoryChangesW watch on the root.
//
// fsnotify watches every directory with its own open handle, and Windows
// refuses to rename a directory while a handle is open anywhere below it:
// with one watch per folder, no folder holding a subfolder could be
// renamed or moved (by Notty or by Explorer) while Notty ran. A single
// handle on the root leaves every folder inside free.
//
// A directory's own watch never reports that directory being removed or
// renamed, so the root's parent is also watched (for directory names only)
// to learn that the vault root went away.
type rdcwBackend struct {
	root string
	evC  chan fsnotify.Event
	errC chan error
	done chan struct{}  // closed by Close
	stop windows.Handle // manual-reset event, set by Close
	wg   sync.WaitGroup // the running dirWatch goroutines

	mu      sync.Mutex
	started bool
	closed  bool
}

// dirWatch is one pending ReadDirectoryChangesW call. It lives on the heap,
// at a fixed address, while the kernel writes into buf and ov.
type dirWatch struct {
	buf  [rdcwBufSize]byte // first, so it is DWORD-aligned
	ov   windows.Overlapped
	h    windows.Handle // the directory
	ev   windows.Handle // signaled when the read completes
	dir  string
	root bool // the vault root (recursive), else its parent
}

func newBackend(root string) (backend, error) {
	stop, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		return nil, fmt.Errorf("watcher: create stop event: %w", os.NewSyscallError("CreateEvent", err))
	}
	return &rdcwBackend{
		root: root,
		evC:  make(chan fsnotify.Event, 64),
		errC: make(chan error),
		done: make(chan struct{}),
		stop: stop,
	}, nil
}

func (b *rdcwBackend) events() <-chan fsnotify.Event { return b.evC }
func (b *rdcwBackend) errors() <-chan error          { return b.errC }

// Add starts the recursive watch of the root on its first call. That watch
// covers every directory below, so later calls do nothing.
func (b *rdcwBackend) Add(string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return fmt.Errorf("watcher: watch %s: %w", b.root, fsnotify.ErrClosed)
	}
	if b.started {
		return nil
	}
	w, err := b.open(b.root, true)
	if err != nil {
		return err
	}
	b.started = true
	b.wg.Add(1)
	go b.watch(w)
	if parent := filepath.Dir(b.root); parent != b.root {
		// Best effort: without it, only a deleted root is noticed.
		if pw, err := b.open(parent, false); err == nil {
			b.wg.Add(1)
			go b.watch(pw)
		}
	}
	return nil
}

// Remove does nothing: the recursive watch follows the tree by itself.
func (b *rdcwBackend) Remove(string) error { return nil }

// Close stops the watches, waits for them to end, and closes the channels.
func (b *rdcwBackend) Close() error {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return nil
	}
	b.closed = true
	b.mu.Unlock()
	close(b.done)
	if err := windows.SetEvent(b.stop); err != nil {
		return fmt.Errorf("watcher: close: %w", os.NewSyscallError("SetEvent", err))
	}
	b.wg.Wait()
	_ = windows.CloseHandle(b.stop)
	close(b.evC)
	close(b.errC)
	return nil
}

// open opens dir and issues its first read, from which on the kernel
// records dir's changes.
func (b *rdcwBackend) open(dir string, root bool) (*dirWatch, error) {
	p, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return nil, fmt.Errorf("watcher: watch %s: %w", dir, err)
	}
	h, err := windows.CreateFile(p, windows.FILE_LIST_DIRECTORY,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OVERLAPPED, 0)
	if err != nil {
		return nil, fmt.Errorf("watcher: watch %w", &os.PathError{Op: "open", Path: dir, Err: err})
	}
	ev, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		_ = windows.CloseHandle(h)
		return nil, fmt.Errorf("watcher: watch %s: %w", dir, os.NewSyscallError("CreateEvent", err))
	}
	w := &dirWatch{h: h, ev: ev, dir: dir, root: root}
	if err := w.read(); err != nil {
		w.close()
		return nil, fmt.Errorf("watcher: watch %s: %w", dir, err)
	}
	return w, nil
}

// read issues an asynchronous ReadDirectoryChangesW call; w.ev is
// signaled when it completes.
func (w *dirWatch) read() error {
	mask := uint32(windows.FILE_NOTIFY_CHANGE_DIR_NAME)
	if w.root {
		mask |= windows.FILE_NOTIFY_CHANGE_FILE_NAME | windows.FILE_NOTIFY_CHANGE_LAST_WRITE
	}
	w.ov = windows.Overlapped{HEvent: w.ev}
	if err := windows.ResetEvent(w.ev); err != nil {
		return os.NewSyscallError("ResetEvent", err)
	}
	if err := windows.ReadDirectoryChanges(w.h, &w.buf[0], rdcwBufSize, w.root, mask, nil, &w.ov, 0); err != nil {
		return os.NewSyscallError("ReadDirectoryChanges", err)
	}
	return nil
}

func (w *dirWatch) close() {
	_ = windows.CloseHandle(w.ev)
	_ = windows.CloseHandle(w.h)
}

// watch delivers w's changes until Close, or until its directory is gone.
// w's first read has been issued.
func (b *rdcwBackend) watch(w *dirWatch) {
	defer b.wg.Done()
	defer w.close()
	for first := true; ; first = false {
		if !first {
			if err := w.read(); err != nil {
				b.fail(w, err)
				return
			}
		}
		fired, err := windows.WaitForMultipleObjects([]windows.Handle{w.ev, b.stop}, false, windows.INFINITE)
		if err != nil || fired != windows.WAIT_OBJECT_0 {
			// Closing (or the wait failed): cancel the read and wait until
			// the kernel no longer uses w's buffer.
			_ = windows.CancelIoEx(w.h, &w.ov)
			var n uint32
			_ = windows.GetOverlappedResult(w.h, &w.ov, &n, true)
			if err != nil {
				b.fail(w, os.NewSyscallError("WaitForMultipleObjects", err))
			}
			return
		}
		var n uint32
		switch err := windows.GetOverlappedResult(w.h, &w.ov, &n, false); {
		case err == nil:
		case errors.Is(err, windows.ERROR_NOTIFY_ENUM_DIR):
			n = 0 // more changes than the buffer holds
		case errors.Is(err, windows.ERROR_OPERATION_ABORTED):
			return
		default:
			b.fail(w, os.NewSyscallError("ReadDirectoryChanges", err))
			return
		}
		if n == 0 {
			if !w.root {
				continue
			}
			if b.rootGone() {
				return
			}
			if !b.sendError(fsnotify.ErrEventOverflow) {
				return
			}
			continue
		}
		if !b.deliver(w, w.buf[:n]) {
			return
		}
	}
}

// deliver sends the events in buf, a list of FILE_NOTIFY_INFORMATION
// records. The parent's watch only reports the root's own removal or
// renaming. It reports false once the backend is closed.
func (b *rdcwBackend) deliver(w *dirWatch, buf []byte) bool {
	for off := 0; off+12 <= len(buf); {
		next := int(binary.LittleEndian.Uint32(buf[off:]))
		action := binary.LittleEndian.Uint32(buf[off+4:])
		size := int(binary.LittleEndian.Uint32(buf[off+8:]))
		start := off + 12
		if start+size > len(buf) {
			break
		}
		name := make([]uint16, size/2)
		for i := range name {
			name[i] = binary.LittleEndian.Uint16(buf[start+2*i:])
		}
		full := filepath.Join(w.dir, windows.UTF16ToString(name))
		op := opOf(action)
		switch {
		case op == 0:
		case w.root:
			if !b.sendEvent(fsnotify.Event{Name: full, Op: op}) {
				return false
			}
		case op&(fsnotify.Remove|fsnotify.Rename) != 0 && strings.EqualFold(full, b.root):
			if !b.sendEvent(fsnotify.Event{Name: b.root, Op: op}) {
				return false
			}
		}
		if next == 0 {
			break
		}
		off += next
	}
	return true
}

// opOf maps a FILE_ACTION_* code to fsnotify's operations, as fsnotify
// does: the new name of a renamed entry is a Create.
func opOf(action uint32) fsnotify.Op {
	switch action {
	case windows.FILE_ACTION_ADDED, windows.FILE_ACTION_RENAMED_NEW_NAME:
		return fsnotify.Create
	case windows.FILE_ACTION_REMOVED:
		return fsnotify.Remove
	case windows.FILE_ACTION_MODIFIED:
		return fsnotify.Write
	case windows.FILE_ACTION_RENAMED_OLD_NAME:
		return fsnotify.Rename
	}
	return 0
}

// fail reports a watch that stopped on err. A root watch that failed
// because the root is gone reports the root removed instead; the parent's
// watch is best effort and fails silently.
func (b *rdcwBackend) fail(w *dirWatch, err error) {
	if !w.root || b.rootGone() {
		return
	}
	b.sendError(fmt.Errorf("watcher: watch %s: %w", w.dir, err))
}

// rootGone reports whether the root no longer exists, sending its removal
// if so.
func (b *rdcwBackend) rootGone() bool {
	if _, err := os.Stat(b.root); err == nil {
		return false
	}
	b.sendEvent(fsnotify.Event{Name: b.root, Op: fsnotify.Remove})
	return true
}

func (b *rdcwBackend) sendEvent(e fsnotify.Event) bool {
	select {
	case b.evC <- e:
		return true
	case <-b.done:
		return false
	}
}

func (b *rdcwBackend) sendError(err error) bool {
	select {
	case b.errC <- err:
		return true
	case <-b.done:
		return false
	}
}
