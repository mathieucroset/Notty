//go:build windows

package watcher

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"golang.org/x/sys/windows"
)

// rdcwBufSize is the size of each ReadDirectoryChangesW buffer. 64 KiB is
// also the most a network share accepts.
const rdcwBufSize = 64 * 1024

// setEvent signals an event object; tests make it fail.
var setEvent = windows.SetEvent

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
//
// Each watch runs on its own goroutine, locked to its OS thread: Windows
// cancels an overlapped read when the thread that issued it exits.
type rdcwBackend struct {
	root string
	evC  chan fsnotify.Event
	errC chan error
	done chan struct{}  // closed by Close
	stop windows.Handle // manual-reset event, set by Close
	wg   sync.WaitGroup // the running watch goroutines

	mu      sync.Mutex
	started bool               // the root's watch is running
	parent  bool               // the parent's watch is running
	closed  bool               // Close was called
	running map[*dirWatch]bool // the open watches, for Close to cancel
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
		root:    root,
		evC:     make(chan fsnotify.Event, 64),
		errC:    make(chan error),
		done:    make(chan struct{}),
		stop:    stop,
		running: map[*dirWatch]bool{},
	}, nil
}

func (b *rdcwBackend) events() <-chan fsnotify.Event { return b.evC }
func (b *rdcwBackend) errors() <-chan error          { return b.errC }

// Add starts the recursive watch of the root, unless it is running. That
// watch covers every directory and file below, so Add ignores its
// argument. It returns once the watch is set up: from then on, the kernel
// records the root's changes. A root watch that stopped (reported with
// ErrWatchStopped) is started again by the next Add.
func (b *rdcwBackend) Add(string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return fsnotify.ErrClosed
	}
	if b.started {
		return nil
	}
	if err := b.start(b.root, true); err != nil {
		return err
	}
	b.started = true
	if parent := filepath.Dir(b.root); parent != b.root && !b.parent {
		// Best effort: without it, only a deleted root is noticed.
		if b.start(parent, false) == nil {
			b.parent = true
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
	var err error
	if serr := setEvent(b.stop); serr != nil {
		err = fmt.Errorf("watcher: close: %w", os.NewSyscallError("SetEvent", serr))
		// The watches never see the stop event: cancel their reads until
		// they have all noticed done.
		b.cancelUntilDone()
	}
	b.wg.Wait()
	_ = windows.CloseHandle(b.stop)
	close(b.evC)
	close(b.errC)
	return err
}

// cancelUntilDone cancels the pending reads of the running watches, again
// and again (a watch may issue a new read before it sees done), until every
// watch goroutine has ended.
func (b *rdcwBackend) cancelUntilDone() {
	ended := make(chan struct{})
	go func() {
		b.wg.Wait()
		close(ended)
	}()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		b.mu.Lock()
		for w := range b.running {
			_ = windows.CancelIoEx(w.h, nil)
		}
		b.mu.Unlock()
		select {
		case <-ended:
			return
		case <-tick.C:
		}
	}
}

// start runs a watch of dir on a new goroutine and returns once its first
// read is pending, or with the error that prevented it. b.mu is held.
func (b *rdcwBackend) start(dir string, root bool) error {
	ready := make(chan error, 1)
	b.wg.Add(1)
	go b.run(dir, root, ready)
	return <-ready
}

// run is a watch's goroutine: it opens dir, issues the first read (sending
// the outcome on ready), and delivers dir's changes until Close, until the
// directory is gone, or until the watch fails.
func (b *rdcwBackend) run(dir string, root bool, ready chan<- error) {
	defer b.wg.Done()
	// Every read of this watch is issued from this goroutine, and the
	// thread that issued a read must outlive it.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	w, err := b.open(dir, root)
	ready <- err
	if err != nil {
		return
	}
	err = b.watch(w)
	b.mu.Lock()
	delete(b.running, w)
	if root {
		b.started = false // the next Add starts it again
	} else {
		b.parent = false
	}
	b.mu.Unlock()
	w.close()
	if err != nil {
		b.fail(w, err)
	}
}

// open opens dir and issues its first read, from which on the kernel
// records dir's changes. b.mu is held (by start's caller).
func (b *rdcwBackend) open(dir string, root bool) (*dirWatch, error) {
	p, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: dir, Err: err}
	}
	h, err := windows.CreateFile(p, windows.FILE_LIST_DIRECTORY,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OVERLAPPED, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: dir, Err: err}
	}
	ev, err := windows.CreateEvent(nil, 1, 0, nil)
	if err != nil {
		_ = windows.CloseHandle(h)
		return nil, fmt.Errorf("%s: %w", dir, os.NewSyscallError("CreateEvent", err))
	}
	w := &dirWatch{h: h, ev: ev, dir: dir, root: root}
	if err := w.read(); err != nil {
		w.close()
		return nil, fmt.Errorf("%s: %w", dir, err)
	}
	b.running[w] = true
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

// closing reports whether Close was called.
func (b *rdcwBackend) closing() bool {
	select {
	case <-b.done:
		return true
	default:
		return false
	}
}

// watch delivers w's changes until Close or until the root is gone, and
// then returns nil; otherwise it returns the error that stopped the watch.
// w's first read has been issued, and no read is pending when it returns.
func (b *rdcwBackend) watch(w *dirWatch) error {
	for {
		fired, err := windows.WaitForMultipleObjects([]windows.Handle{w.ev, b.stop}, false, windows.INFINITE)
		if err != nil || fired != windows.WAIT_OBJECT_0 {
			// Closing (or the wait failed).
			w.cancelRead()
			if err != nil && !b.closing() {
				return os.NewSyscallError("WaitForMultipleObjects", err)
			}
			return nil
		}
		var n uint32
		switch err := windows.GetOverlappedResult(w.h, &w.ov, &n, false); {
		case err == nil:
		case errors.Is(err, windows.ERROR_NOTIFY_ENUM_DIR):
			n = 0 // more changes than the buffer holds
		case errors.Is(err, windows.ERROR_OPERATION_ABORTED):
			// Close cancels reads only when it cannot signal the stop
			// event; any other cancellation stops the watch.
			if b.closing() {
				return nil
			}
			return fmt.Errorf("read cancelled: %w", os.NewSyscallError("ReadDirectoryChanges", err))
		default:
			return os.NewSyscallError("ReadDirectoryChanges", err)
		}
		// Copy the records and issue the next read at once, so the kernel
		// has a buffer again while they are delivered.
		var batch []byte
		if n > 0 {
			batch = append([]byte(nil), w.buf[:n]...)
		}
		rerr := w.read()
		ok := true
		switch {
		case n > 0:
			ok = b.deliver(w, batch)
		case w.root:
			ok = !b.rootGone() && b.sendError(fsnotify.ErrEventOverflow)
		}
		if !ok {
			// Closed, or the root is gone.
			if rerr == nil {
				w.cancelRead()
			}
			return nil
		}
		if rerr != nil {
			return rerr
		}
	}
}

// cancelRead cancels w's pending read and waits until the kernel no longer
// uses w's buffer.
func (w *dirWatch) cancelRead() {
	_ = windows.CancelIoEx(w.h, &w.ov)
	var n uint32
	_ = windows.GetOverlappedResult(w.h, &w.ov, &n, true)
}

// deliver sends the events in buf, a list of FILE_NOTIFY_INFORMATION
// records. Changes inside .git directories are dropped here, before they
// cost anything more. The parent's watch only reports the root's own
// removal or renaming. It reports false once the backend is closed.
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
		rel := windows.UTF16ToString(name)
		full := filepath.Join(w.dir, rel)
		op := opOf(action)
		switch {
		case op == 0:
		case w.root:
			if inGitDir(rel) {
				break
			}
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

// inGitDir reports whether rel, a path relative to the root with "\"
// separators, is a .git directory or lies inside one. The watcher ignores
// those paths too (see ignored).
func inGitDir(rel string) bool {
	for part := range strings.SplitSeq(rel, `\`) {
		if strings.EqualFold(part, ".git") {
			return true
		}
	}
	return false
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
// watch is best effort and fails silently. Any other root failure is
// reported as ErrWatchStopped, on which the watcher restarts the watch.
func (b *rdcwBackend) fail(w *dirWatch, err error) {
	if !w.root || b.rootGone() {
		return
	}
	b.sendError(fmt.Errorf("%w: %s: %w", ErrWatchStopped, w.dir, err))
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
