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
	// been seen for this long, and a path is only ever reported once it has
	// itself been quiet this long (so the app's NoteSelfWrite, called right
	// after a save, always lands before the save's event is judged).
	debounce = 100 * time.Millisecond
	// maxDelay caps how long a continuous stream of changes can hold back an
	// Event: after it, the paths already quiet for debounce are delivered and
	// the still-changing ones wait for their own quiet window.
	maxDelay = time.Second
	// barrierRel is the sentinel file Pause and Resume create to learn when
	// the event loop has caught up with the kernel's event queue. Its name
	// ends in .notty-tmp, so every other consumer ignores it.
	barrierRel = ".notty/.watch-barrier.notty-tmp"
	// barrierTimeout bounds how long Pause and Resume wait for the barrier.
	barrierTimeout = 200 * time.Millisecond
	// nottyDir is the app's metadata directory. Its own entry is never
	// reported (Pause may create it); the files inside it are.
	nottyDir = ".notty"
	// selfWriteTTL is how long a NoteSelfWrite record stays valid.
	selfWriteTTL = 5 * time.Second
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

// fileID is a file's inode and status-change time, where the platform
// provides them (see fileIDOf); the zero value otherwise.
type fileID struct {
	ino   uint64
	ctime int64 // nanoseconds
}

// fileStamp identifies the content of a file the app wrote itself.
type fileStamp struct {
	mtime    time.Time
	size     int64
	id       fileID
	recorded time.Time // when NoteSelfWrite ran
}

func stampOf(info os.FileInfo, now time.Time) fileStamp {
	return fileStamp{mtime: info.ModTime(), size: info.Size(), id: fileIDOf(info), recorded: now}
}

// matches reports whether info still describes the file as it was stamped.
func (s fileStamp) matches(info os.FileInfo) bool {
	return info.ModTime().Equal(s.mtime) && info.Size() == s.size && fileIDOf(info) == s.id
}

// Watcher reports changes under a vault root. Create it with New and release
// it with Close.
type Watcher struct {
	root   string
	fsw    *fsnotify.Watcher
	events chan Event
	errors chan error
	done   chan struct{}
	ctl    chan chan struct{} // barrier requests to the run goroutine
	wg     sync.WaitGroup
	once   sync.Once

	pauseMu sync.Mutex // serializes Pause and Resume

	mu         sync.Mutex
	pauses     int                  // Pause nesting depth; paused while > 0
	selfWrites map[string]fileStamp // rel -> stamp recorded by NoteSelfWrite

	// Owned by the run goroutine.
	dirs           map[string]bool      // watched directories, vault-relative ("." = root)
	pending        map[string]time.Time // changed paths not yet debounced -> last change
	ready          map[string]bool      // debounced paths awaiting delivery
	urgent         []error              // errors delivered even when the Errors buffer is full
	rootGone       bool                 // ErrRootGone already queued
	overflowQueued bool                 // an overflow error is queued in urgent
	barriers       []chan struct{}      // barrier requests awaiting the sentinel's event
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
		ctl:        make(chan chan struct{}),
		selfWrites: map[string]fileStamp{},
		dirs:       map[string]bool{},
		pending:    map[string]time.Time{},
		ready:      map[string]bool{},
	}
	_ = os.Remove(w.abs(barrierRel)) // stale sentinel from a crash
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
// the save, including after an atomic tmp-file-then-rename save). For the next
// 5 seconds, changes to rel are ignored as long as the file still has the
// size, modification time, inode and status-change time it has now (inode and
// ctime where the platform has them). The first change that alters them, or
// deletes the file, is reported and ends the suppression.
func (w *Watcher) NoteSelfWrite(rel string) {
	rel = cleanRel(rel)
	info, err := os.Stat(w.abs(rel))
	now := time.Now()
	w.mu.Lock()
	defer w.mu.Unlock()
	for k, st := range w.selfWrites {
		if now.Sub(st.recorded) > selfWriteTTL {
			delete(w.selfWrites, k)
		}
	}
	if err != nil {
		delete(w.selfWrites, rel)
		return
	}
	w.selfWrites[rel] = stampOf(info, now)
}

// Pause drops every change made from now until the matching Resume. Use it
// only around git operations that change the working tree; the caller
// re-indexes the files those operations changed. Pauses nest: the watcher is
// paused until every Pause has been matched by a Resume.
//
// Before pausing, the outermost Pause waits (up to 200ms) until the event loop
// has handled every change made before the call, by writing a sentinel file
// under .notty/ (creating that directory if needed) and waiting for its event.
// Changes made before Pause are therefore still delivered. New directories
// keep being watched while paused.
func (w *Watcher) Pause() {
	w.pauseMu.Lock()
	defer w.pauseMu.Unlock()
	if w.pauseDepth() == 0 {
		w.barrier()
	}
	w.mu.Lock()
	w.pauses++
	w.mu.Unlock()
}

// Resume ends one Pause; an unmatched Resume does nothing. The outermost
// Resume first waits, like Pause, until every change made before the call has
// been handled (and dropped), so writes made during the pause are not reported
// after it.
func (w *Watcher) Resume() {
	w.pauseMu.Lock()
	defer w.pauseMu.Unlock()
	switch w.pauseDepth() {
	case 0:
		return
	case 1:
		w.barrier()
	}
	w.mu.Lock()
	w.pauses--
	w.mu.Unlock()
}

// barrier returns once the run goroutine has handled every kernel event queued
// before the call, or after barrierTimeout, or when the watcher closes.
func (w *Watcher) barrier() {
	req := make(chan struct{})
	select {
	case w.ctl <- req:
	case <-w.done:
		return
	}
	t := time.NewTimer(barrierTimeout)
	defer t.Stop()
	select {
	case <-req:
	case <-t.C:
	case <-w.done:
	}
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
	defer w.releaseBarriers()

	timer := time.NewTimer(debounce)
	timer.Stop()
	defer timer.Stop()
	var (
		timerC       <-chan time.Time // nil while nothing is pending
		firstPending time.Time        // first change of the current batch
		lastChange   time.Time        // latest change of any path
		out          chan Event       // nil while nothing is ready
		ev           Event            // the batch offered on out
	)
	// arm schedules the next flush: at the end of the vault-wide quiet window,
	// or at the maxDelay cap if that comes first.
	arm := func(now time.Time) {
		if len(w.pending) == 0 {
			timer.Stop()
			timerC = nil
			return
		}
		deadline := lastChange.Add(debounce)
		if limit := firstPending.Add(maxDelay); limit.Before(deadline) {
			deadline = limit
		}
		timer.Reset(max(deadline.Sub(now), 0))
		timerC = timer.C
	}
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
			now := time.Now()
			if !w.handle(fe, now) {
				continue
			}
			// Every recorded change restarts the quiet window.
			if wasIdle {
				firstPending = now
			}
			lastChange = now
			arm(now)

		case err, ok := <-w.fsw.Errors:
			if !ok {
				return
			}
			w.handleFSError(err)

		case req := <-w.ctl:
			w.startBarrier(req)

		case <-timerC:
			timerC = nil
			now := time.Now()
			w.flush(now)
			firstPending = now // any path still pending starts a new batch
			arm(now)
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

// startBarrier creates the sentinel file. req is closed when the sentinel's
// creation event comes back through the loop, which proves every kernel event
// queued before it has been handled. If the sentinel cannot be created, req is
// closed at once.
func (w *Watcher) startBarrier(req chan struct{}) {
	// Mkdir, not MkdirAll: never recreate a vault root that has been removed.
	if err := os.Mkdir(w.abs(nottyDir), 0o755); w.rootGone || (err != nil && !errors.Is(err, fs.ErrExist)) {
		close(req)
		return
	}
	if !w.dirs[nottyDir] {
		errs, _ := w.addTree(nottyDir, nil)
		for _, e := range errs {
			w.sendError(e)
		}
		if !w.dirs[nottyDir] {
			close(req)
			return
		}
	}
	path := w.abs(barrierRel)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if errors.Is(err, fs.ErrExist) {
		// Left over from a barrier that timed out: recreate it so that a
		// creation event is guaranteed.
		_ = os.Remove(path)
		f, err = os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	}
	if err != nil {
		close(req)
		return
	}
	_ = f.Close()
	w.barriers = append(w.barriers, req)
}

// releaseBarriers completes every waiting barrier and removes the sentinel.
func (w *Watcher) releaseBarriers() {
	if len(w.barriers) == 0 {
		return
	}
	_ = os.Remove(w.abs(barrierRel))
	for _, req := range w.barriers {
		close(req)
	}
	w.barriers = nil
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
// changed path, at time now, unless it is ignored or the watcher is paused. It
// reports whether any change was recorded.
func (w *Watcher) handle(fe fsnotify.Event, now time.Time) bool {
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
	if rel == barrierRel {
		if fe.Has(fsnotify.Create) {
			w.releaseBarriers()
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
	recorded := false

	if fe.Has(fsnotify.Remove) || fe.Has(fsnotify.Rename) {
		w.unwatchTree(rel)
	}
	if fe.Has(fsnotify.Create) {
		if info, err := os.Lstat(fe.Name); err == nil && info.IsDir() {
			// Files may have been created inside before the watch existed,
			// so report everything found while adding the watches.
			var found func(string)
			if !paused {
				found = func(sub string) {
					w.pending[sub] = now
					recorded = true
				}
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
	if paused || rel == nottyDir {
		return recorded
	}
	w.pending[rel] = now
	return true
}

// flush moves the pending paths that have been quiet for debounce to the
// ready set, dropping the app's own writes.
func (w *Watcher) flush(now time.Time) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for rel, last := range w.pending {
		if now.Sub(last) < debounce {
			continue
		}
		delete(w.pending, rel)
		if !w.isSelfWrite(rel, now) {
			w.ready[rel] = true
		}
	}
}

// isSelfWrite reports whether rel is still exactly as the app wrote it, per a
// NoteSelfWrite record younger than selfWriteTTL. A stale or mismatched record
// is dropped. w.mu must be held.
func (w *Watcher) isSelfWrite(rel string, now time.Time) bool {
	stamp, ok := w.selfWrites[rel]
	if !ok {
		return false
	}
	if now.Sub(stamp.recorded) <= selfWriteTTL {
		if info, err := os.Stat(w.abs(rel)); err == nil && stamp.matches(info) {
			return true
		}
	}
	delete(w.selfWrites, rel)
	return false
}

// addTree watches rel and every non-ignored directory below it. When found is
// non-nil, it is called with every path discovered below rel. A directory that
// cannot be read or watched (permissions, inotify watch limit) is skipped and
// its error collected in errs; fatal is non-nil only when the vault root itself
// cannot be walked or watched.
func (w *Watcher) addTree(rel string, found func(string)) (errs []error, fatal error) {
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
			found(sub)
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

func (w *Watcher) isPaused() bool { return w.pauseDepth() > 0 }

func (w *Watcher) pauseDepth() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.pauses
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
