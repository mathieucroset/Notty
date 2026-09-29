// Package watcher notices files changed outside the app (edits made in
// $EDITOR, files changed by hand or by git) so the UI can re-index and reload.
//
// It wraps fsnotify with a recursive directory watch, the vault ignore rules,
// suppression of the app's own saves, a pause switch for working-tree-changing
// git operations, and a 100ms debounce that batches bursts into one Event.
package watcher

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

const (
	// debounce is the quiet window: an Event is emitted once no change has
	// been seen for this long.
	debounce = 100 * time.Millisecond
	// maxDelay caps how long a continuous stream of changes can hold back an
	// Event, so a busy file cannot starve the consumer.
	maxDelay = time.Second
	// errBuffer is the capacity of the Errors channel; further errors are
	// dropped while it is full.
	errBuffer = 16
)

// ErrRootGone is delivered on Errors when the vault root itself is deleted or
// renamed. The watcher reports nothing further; the caller should close it.
var ErrRootGone = errors.New("watcher: vault root was removed or renamed")

// Event is one debounced batch of changed paths. Paths are vault-relative,
// use "/" separators, and are deduplicated and sorted. A path may name a
// file or a directory, and may no longer exist (deletions and renames are
// reported under the old name); the consumer checks existence.
type Event struct{ Paths []string }

// fileStamp identifies the content of a file the app wrote itself.
type fileStamp struct {
	mtime time.Time
	size  int64
}

// Watcher reports changes under a vault root. Create it with New and release
// it with Close.
type Watcher struct {
	root   string
	fsw    *fsnotify.Watcher
	events chan Event
	errors chan error
	done   chan struct{}
	wg     sync.WaitGroup
	once   sync.Once

	mu         sync.Mutex
	paused     bool
	selfWrites map[string]fileStamp // rel -> stamp recorded by NoteSelfWrite

	// Owned by the run goroutine.
	dirs           map[string]bool // watched directories, vault-relative ("." = root)
	pending        map[string]bool // changed paths not yet debounced
	ready          map[string]bool // debounced paths awaiting delivery
	urgent         []error         // errors delivered even when the Errors buffer is full
	rootGone       bool            // ErrRootGone already queued
	overflowQueued bool            // an overflow error is queued in urgent
}

// New starts watching root and every directory below it, except ignored ones.
// Only a root that cannot be read or watched is an error; subdirectories that
// cannot be (for example because of permissions) are skipped and reported on
// Errors.
func New(root string) (*Watcher, error) {
	w, err := newWatcher(root)
	if err != nil {
		return nil, err
	}
	w.start()
	return w, nil
}

// newWatcher sets up the watches without starting the event loop.
func newWatcher(root string) (*Watcher, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("watcher: resolve root: %w", err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("watcher: stat root: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("watcher: root %q is not a directory", abs)
	}
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("watcher: create fsnotify watcher: %w", err)
	}
	w := &Watcher{
		root:       abs,
		fsw:        fsw,
		events:     make(chan Event),
		errors:     make(chan error, errBuffer),
		done:       make(chan struct{}),
		selfWrites: map[string]fileStamp{},
		dirs:       map[string]bool{},
		pending:    map[string]bool{},
		ready:      map[string]bool{},
	}
	errs, err := w.addTree(".", nil)
	if err != nil {
		_ = fsw.Close()
		return nil, err
	}
	for _, e := range errs {
		w.sendError(e) // buffered: delivered once the caller reads Errors
	}
	return w, nil
}

func (w *Watcher) start() {
	w.wg.Add(1)
	go w.run()
}

// Events delivers debounced batches of changes. It is closed by Close.
func (w *Watcher) Events() <-chan Event { return w.events }

// Errors delivers non-fatal watcher errors: directories that cannot be read or
// watched, and event queue overflows (after which a full re-index is
// advisable). It is closed by Close.
func (w *Watcher) Errors() <-chan error { return w.errors }

// NoteSelfWrite records that the app has just written rel (call it right after
// the save, including after an atomic tmp-file-then-rename save). Subsequent
// changes to rel are ignored for as long as the file still has the size and
// modification time it has now; the first change that alters them, or deletes
// the file, is reported again.
func (w *Watcher) NoteSelfWrite(rel string) {
	rel = cleanRel(rel)
	info, err := os.Stat(w.abs(rel))
	w.mu.Lock()
	defer w.mu.Unlock()
	if err != nil {
		delete(w.selfWrites, rel)
		return
	}
	w.selfWrites[rel] = fileStamp{mtime: info.ModTime(), size: info.Size()}
}

// Pause drops every change seen until Resume. Use it only around git
// operations that change the working tree; the caller re-indexes the files
// those operations changed. Changes seen before Pause are still delivered.
// New directories are still watched while paused.
func (w *Watcher) Pause() {
	w.mu.Lock()
	w.paused = true
	w.mu.Unlock()
}

// Resume ends a Pause. Kernel events queued by writes made just before Resume
// may still be delivered; consumers must tolerate redundant paths.
func (w *Watcher) Resume() {
	w.mu.Lock()
	w.paused = false
	w.mu.Unlock()
}

// Close stops watching, waits for the internal goroutine to exit, and closes
// the Events and Errors channels. Undelivered changes are discarded. It is
// safe to call more than once.
func (w *Watcher) Close() error {
	var err error
	w.once.Do(func() {
		close(w.done)
		if cerr := w.fsw.Close(); cerr != nil {
			err = fmt.Errorf("watcher: close: %w", cerr)
		}
		w.wg.Wait()
	})
	return err
}

func (w *Watcher) run() {
	defer w.wg.Done()
	defer close(w.errors)
	defer close(w.events)

	timer := time.NewTimer(debounce)
	timer.Stop()
	defer timer.Stop()
	var (
		timerC       <-chan time.Time // nil while nothing is pending
		firstPending time.Time
		out          chan Event // nil while nothing is ready
		ev           Event      // the batch offered on out
	)
	for {
		var (
			errOut    chan error // nil while no urgent error is queued
			urgentErr error
		)
		if len(w.urgent) > 0 {
			errOut, urgentErr = w.errors, w.urgent[0]
		}

		select {
		case <-w.done:
			return

		case fe, ok := <-w.fsw.Events:
			if !ok {
				return
			}
			wasIdle := len(w.pending) == 0
			if !w.handle(fe) {
				continue
			}
			// Every recorded change restarts the quiet window, capped at
			// maxDelay after the first change of the batch.
			now := time.Now()
			if wasIdle {
				firstPending = now
			}
			wait := min(debounce, max(firstPending.Add(maxDelay).Sub(now), 0))
			timer.Reset(wait)
			timerC = timer.C

		case err, ok := <-w.fsw.Errors:
			if !ok {
				return
			}
			w.handleFSError(err)

		case <-timerC:
			timerC = nil
			w.flush()
			if len(w.ready) > 0 {
				out = w.events
				ev = Event{Paths: sortedKeys(w.ready)}
			}

		case out <- ev:
			clear(w.ready)
			out, ev = nil, Event{}

		case errOut <- urgentErr:
			w.urgent = w.urgent[1:]
			if errors.Is(urgentErr, fsnotify.ErrEventOverflow) {
				w.overflowQueued = false
			}
		}
	}
}

// handleFSError forwards an fsnotify error. After a queue overflow, events
// (including directory creations and removals) were lost: the overflow is
// queued for guaranteed delivery, once until delivered, and the directory
// watches are rebuilt.
func (w *Watcher) handleFSError(err error) {
	if !errors.Is(err, fsnotify.ErrEventOverflow) {
		w.sendError(fmt.Errorf("watcher: %w", err))
		return
	}
	if !w.overflowQueued {
		w.overflowQueued = true
		w.urgent = append(w.urgent, fmt.Errorf("watcher: events lost, re-index the vault: %w", err))
	}
	w.resync()
}

// resync rebuilds the directory watches from disk: it drops watches on
// directories that no longer exist (or were renamed away) and watches every
// directory currently present.
func (w *Watcher) resync() {
	for d := range w.dirs {
		if d == "." {
			continue
		}
		if info, err := os.Lstat(w.abs(d)); err != nil || !info.IsDir() {
			w.unwatchTree(d)
		}
	}
	clear(w.dirs) // re-adding an existing watch is harmless
	errs, err := w.addTree(".", nil)
	for _, e := range errs {
		w.sendError(e)
	}
	if err != nil {
		w.sendError(err)
	}
}

// handle updates the directory watches for one fsnotify event and records the
// changed path unless it is ignored or the watcher is paused. It reports
// whether a change was recorded.
func (w *Watcher) handle(fe fsnotify.Event) bool {
	rel, ok := w.rel(fe.Name)
	if !ok {
		return false
	}
	if rel == "." {
		if (fe.Has(fsnotify.Remove) || fe.Has(fsnotify.Rename)) && !w.rootGone {
			w.rootGone = true
			w.urgent = append(w.urgent, ErrRootGone)
		}
		return false
	}
	if ignored(rel) {
		return false
	}
	// Pure attribute changes (chmod, touch -a) do not change content.
	if fe.Op == fsnotify.Chmod {
		return false
	}
	paused := w.isPaused()

	if fe.Has(fsnotify.Remove) || fe.Has(fsnotify.Rename) {
		w.unwatchTree(rel)
	}
	if fe.Has(fsnotify.Create) {
		if info, err := os.Lstat(fe.Name); err == nil && info.IsDir() {
			// Files may have been created inside before the watch existed,
			// so report everything found while adding the watches.
			var found map[string]bool
			if !paused {
				found = w.pending
			}
			errs, err := w.addTree(rel, found)
			for _, e := range errs {
				w.sendError(e)
			}
			if err != nil {
				w.sendError(err)
			}
		}
	}
	if paused {
		return false
	}
	w.pending[rel] = true
	return true
}

// flush moves pending paths to the ready set, dropping the app's own writes.
func (w *Watcher) flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	for rel := range w.pending {
		if stamp, ok := w.selfWrites[rel]; ok {
			info, err := os.Stat(w.abs(rel))
			if err == nil && info.ModTime().Equal(stamp.mtime) && info.Size() == stamp.size {
				continue
			}
			delete(w.selfWrites, rel)
		}
		w.ready[rel] = true
	}
	clear(w.pending)
}

// addTree watches rel and every non-ignored directory below it. When found is
// non-nil, every path discovered below rel is added to it. A directory that
// cannot be read or watched (permissions, inotify watch limit) is skipped and
// its error collected in errs; fatal is non-nil only when the vault root itself
// cannot be walked or watched.
func (w *Watcher) addTree(rel string, found map[string]bool) (errs []error, fatal error) {
	fatal = filepath.WalkDir(w.abs(rel), func(p string, d fs.DirEntry, err error) error {
		sub, ok := w.rel(p)
		if !ok {
			return nil
		}
		if err != nil {
			switch {
			case sub == ".":
				return fmt.Errorf("watcher: walk vault root: %w", err)
			case errors.Is(err, fs.ErrNotExist):
				// Vanished while walking.
			default:
				errs = append(errs, fmt.Errorf("watcher: walk %s: %w", sub, err))
			}
			if d != nil && d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if sub != "." && ignored(sub) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if found != nil && sub != rel {
			found[sub] = true
		}
		if !d.IsDir() || w.dirs[sub] {
			return nil
		}
		if err := w.fsw.Add(p); err != nil {
			switch {
			case sub == ".":
				return fmt.Errorf("watcher: watch vault root: %w", err)
			case errors.Is(err, fs.ErrNotExist):
				// Vanished while walking.
			default:
				errs = append(errs, fmt.Errorf("watcher: watch %s: %w", sub, err))
			}
			return filepath.SkipDir
		}
		w.dirs[sub] = true
		return nil
	})
	return errs, fatal
}

// unwatchTree drops the watches on rel and every directory below it. It must
// run before a directory renamed within the vault is watched again under its
// new name, because the kernel keeps the old watch on the moved directory.
func (w *Watcher) unwatchTree(rel string) {
	prefix := rel + "/"
	for d := range w.dirs {
		if d == rel || strings.HasPrefix(d, prefix) {
			delete(w.dirs, d)
			_ = w.fsw.Remove(w.abs(d)) // already gone if the directory was deleted
		}
	}
}

func (w *Watcher) sendError(err error) {
	select {
	case w.errors <- err:
	default:
	}
}

func (w *Watcher) isPaused() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.paused
}

func (w *Watcher) abs(rel string) string {
	return filepath.Join(w.root, filepath.FromSlash(rel))
}

// rel converts an absolute path below root to a vault-relative "/" path.
func (w *Watcher) rel(p string) (string, bool) {
	r, err := filepath.Rel(w.root, p)
	if err != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
		return "", false
	}
	return filepath.ToSlash(r), true
}

func cleanRel(rel string) string {
	return filepath.ToSlash(filepath.Clean(filepath.FromSlash(rel)))
}

// ignored reports whether a vault-relative path is excluded from watching:
// anything inside a .git directory, temporary save files, crash-recovery
// buffers, and the running-instance lock.
func ignored(rel string) bool {
	switch {
	case strings.HasSuffix(rel, ".notty-tmp"):
		return true
	case rel == ".notty/lock":
		return true
	case rel == ".notty/recovery" || strings.HasPrefix(rel, ".notty/recovery/"):
		return true
	}
	return slices.Contains(strings.Split(rel, "/"), ".git")
}

func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
