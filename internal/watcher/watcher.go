// Package watcher notices files changed outside the app (edits made in
// $EDITOR, files changed by hand or by git) so the UI can re-index and reload.
//
// It wraps fsnotify (on Windows, one recursive ReadDirectoryChangesW watch,
// see backend_windows.go) with a recursive directory watch, the vault ignore rules,
// suppression of the app's own saves, a pause switch for working-tree-changing
// git operations, and a 100ms debounce that batches bursts into one Event.
package watcher

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
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
	// barrierPrefix and barrierSuffix frame the names of the sentinel files
	// Pause and Resume create to learn when the event loop has caught up
	// with the kernel's event queue (see barrierRel). The names end in
	// .notty-tmp, so every other consumer ignores them.
	barrierPrefix = ".notty/.watch-barrier-"
	barrierSuffix = ".notty-tmp"
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
	// restartWindow is how long after restarting a stopped watch another
	// stop is taken as final.
	restartWindow = time.Minute
)

// openBackend creates the change notification source; tests wrap it.
var openBackend = newBackend

// barrierRel is the vault-relative name of barrier number seq's sentinel.
// Every barrier has its own name: fsnotify's kqueue backend reports a path
// as created only once while it still tracks it, so a sentinel created
// under the name of one just removed might never be reported.
func barrierRel(seq uint64) string {
	return barrierPrefix + strconv.FormatUint(seq, 10) + barrierSuffix
}

// barrierSeqOf reports the barrier number of a sentinel's vault-relative
// path, and whether rel is a sentinel at all.
func barrierSeqOf(rel string) (uint64, bool) {
	num, ok := strings.CutPrefix(rel, barrierPrefix)
	if !ok {
		return 0, false
	}
	if num, ok = strings.CutSuffix(num, barrierSuffix); !ok {
		return 0, false
	}
	seq, err := strconv.ParseUint(num, 10, 64)
	return seq, err == nil
}

// barrier is a request waiting for its sentinel's creation event.
type barrier struct {
	seq uint64
	req chan struct{}
}

// ErrRootGone is delivered on Errors when the vault root itself is deleted or
// renamed. The watcher reports nothing further; the caller should close it.
var ErrRootGone = errors.New("watcher: vault root was removed or renamed")

// ErrEventsLost is matched (errors.Is) by the error delivered on Errors when
// changes were missed: after an event queue overflow, or once a stopped
// watch was restarted. The caller should re-index the whole vault. It is
// fsnotify.ErrEventOverflow.
var ErrEventsLost = fsnotify.ErrEventOverflow

// ErrWatchStopped is delivered on Errors when the operating system stopped
// reporting changes (on Windows, the vault's watch failed) and restarting
// the watch did not work: changes made outside the app are no longer
// noticed. A watch that restarts is reported as lost events instead (see
// Errors).
var ErrWatchStopped = errors.New("watcher: stopped watching the vault")

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
	root string
	// rootInfo identifies the vault root directory as found by New: an
	// event reporting the root removed or renamed while the root path
	// still names that same directory (a rename only by case) is not a
	// loss of the root.
	rootInfo os.FileInfo
	fsw      backend
	log      *slog.Logger
	events   chan Event
	errors   chan error
	done     chan struct{}
	ctl      chan chan struct{} // barrier requests to the run goroutine
	wg       sync.WaitGroup
	once     sync.Once

	pauseMu sync.Mutex // serializes Pause and Resume

	mu         sync.Mutex
	pauses     int                  // Pause nesting depth; paused while > 0
	selfWrites map[string]fileStamp // rel -> stamp recorded by NoteSelfWrite

	// Owned by the run goroutine.
	dirs           map[string]bool      // watched directories, vault-relative ("." = root)
	skipped        map[string]bool      // directories not watched for lack of permission, already logged
	pending        map[string]time.Time // changed paths not yet debounced -> last change
	ready          map[string]bool      // debounced paths awaiting delivery
	urgent         []error              // errors delivered even when the Errors buffer is full
	rootGone       bool                 // ErrRootGone already queued
	overflowQueued bool                 // an overflow error is queued in urgent
	stoppedQueued  bool                 // ErrWatchStopped already queued
	lastRestart    time.Time            // when a stopped watch was last restarted
	barriers       []barrier            // barrier requests awaiting their sentinel's event
	barrierSeq     uint64               // number of the latest barrier
	trackGaps      bool                 // list the directories with a gap (kqueue), see gaps.go
	gaps           map[string]gap       // directories with a gap -> their listing past the blocker
}

// New starts watching root and every directory below it, except ignored ones.
// Only a root that cannot be read or watched is an error. Subdirectories the
// user may not read are skipped and logged, once per directory: they were
// locked on purpose, and reporting them would warn about them on every start.
// Subdirectories that cannot be watched for other reasons (the inotify watch
// limit) are skipped and reported on Errors.
func New(root string) (*Watcher, error) {
	w, err := newWatcher(root, nil)
	if err != nil {
		return nil, err
	}
	w.start()
	return w, nil
}

// newWatcher sets up the watches without starting the event loop. A nil log
// means slog's default logger.
func newWatcher(root string, log *slog.Logger) (*Watcher, error) {
	if log == nil {
		log = slog.Default()
	}
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
	// Where the file ID is read lazily (Windows), read it now, while the
	// path still names this directory.
	_ = os.SameFile(info, info)
	fsw, err := openBackend(abs)
	if err != nil {
		return nil, err
	}
	w := &Watcher{
		root:       abs,
		rootInfo:   info,
		fsw:        fsw,
		log:        log,
		events:     make(chan Event),
		errors:     make(chan error, errBuffer),
		done:       make(chan struct{}),
		ctl:        make(chan chan struct{}),
		selfWrites: map[string]fileStamp{},
		dirs:       map[string]bool{},
		skipped:    map[string]bool{},
		trackGaps:  usesKqueue(),
		gaps:       map[string]gap{},
		pending:    map[string]time.Time{},
		ready:      map[string]bool{},
	}
	w.removeStaleBarriers()
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

// Errors delivers watcher errors: directories that cannot be read or watched
// (except for lack of permission, which is only logged); ErrEventsLost, never
// while paused (the caller re-indexes after the git operation anyway);
// ErrWatchStopped; and ErrRootGone. It is closed by Close.
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
	defer w.releaseBarriers(math.MaxUint64)

	timer := time.NewTimer(debounce)
	timer.Stop()
	defer timer.Stop()
	poll := time.NewTicker(gapPoll)
	poll.Stop()
	defer poll.Stop()
	var pollC <-chan time.Time // nil while no directory has a gap
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
	// changed restarts the quiet window after a recorded change.
	changed := func(wasIdle bool, now time.Time) {
		if wasIdle {
			firstPending = now
		}
		lastChange = now
		arm(now)
	}
	for {
		switch {
		case len(w.gaps) > 0 && pollC == nil:
			poll.Reset(gapPoll)
			pollC = poll.C
		case len(w.gaps) == 0 && pollC != nil:
			poll.Stop()
			pollC = nil
		}
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

		case fe, ok := <-w.fsw.events():
			if !ok {
				return
			}
			wasIdle := len(w.pending) == 0
			now := time.Now()
			if w.handle(fe, now) {
				changed(wasIdle, now)
			}

		case <-pollC:
			wasIdle := len(w.pending) == 0
			now := time.Now()
			if w.pollGaps(now) {
				changed(wasIdle, now)
			}

		case err, ok := <-w.fsw.errors():
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

// startBarrier creates a new sentinel file. req is closed when the sentinel's
// creation event (or a later sentinel's) comes back through the loop, which
// proves every kernel event queued before it has been handled. If the
// sentinel cannot be created, req is closed at once.
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
	w.barrierSeq++
	seq := w.barrierSeq
	path := w.abs(barrierRel(seq))
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if errors.Is(err, fs.ErrExist) {
		// Left over from an earlier run: recreate it so that a creation
		// event is guaranteed.
		_ = os.Remove(path)
		f, err = os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	}
	if err != nil {
		close(req)
		return
	}
	_ = f.Close()
	w.barriers = append(w.barriers, barrier{seq: seq, req: req})
}

// releaseBarriers completes the waiting barriers numbered up to seq, whose
// sentinels were created before (or as) the one just seen, and removes
// their sentinels.
func (w *Watcher) releaseBarriers(seq uint64) {
	kept := w.barriers[:0]
	for _, b := range w.barriers {
		if b.seq > seq {
			kept = append(kept, b)
			continue
		}
		_ = os.Remove(w.abs(barrierRel(b.seq)))
		close(b.req)
	}
	clear(w.barriers[len(kept):])
	w.barriers = kept
}

// removeStaleBarriers removes the sentinels left behind by a crash.
func (w *Watcher) removeStaleBarriers() {
	entries, err := os.ReadDir(w.abs(nottyDir))
	if err != nil {
		return
	}
	for _, e := range entries {
		name := e.Name()
		// Also the single ".watch-barrier.notty-tmp" of older versions.
		if strings.HasPrefix(name, ".watch-barrier") && strings.HasSuffix(name, barrierSuffix) {
			_ = os.Remove(w.abs(nottyDir + "/" + name))
		}
	}
}

// handleFSError forwards a backend error.
//
// After a queue overflow, events (including directory creations and
// removals) were lost: the directory watches are rebuilt, and unless paused
// the loss is queued for guaranteed delivery, once until delivered.
//
// A stopped watch (ErrWatchStopped) is restarted by rebuilding the watches,
// and the events missed meanwhile are reported as lost. When the restart
// fails, or the watch stops again within restartWindow of a restart, the
// stop is queued for guaranteed delivery instead.
func (w *Watcher) handleFSError(err error) {
	switch {
	case errors.Is(err, ErrWatchStopped):
		if w.lastRestart.IsZero() || time.Since(w.lastRestart) > restartWindow {
			w.lastRestart = time.Now()
			rerr := w.resync()
			if rerr == nil {
				w.log.Warn("watcher: the vault's watch stopped and was restarted", "err", err)
				w.eventsLost(err)
				return
			}
			w.log.Warn("watcher: could not restart the vault's watch", "err", rerr)
		}
		if !w.stoppedQueued {
			w.stoppedQueued = true
			w.urgent = append(w.urgent, err)
		}
	case errors.Is(err, fsnotify.ErrEventOverflow):
		if rerr := w.resync(); rerr != nil {
			w.sendError(rerr)
		}
		w.eventsLost(err)
	default:
		w.sendError(fmt.Errorf("watcher: %w", err))
	}
}

// eventsLost queues the loss of events for guaranteed delivery, once until
// delivered, unless paused: the caller re-indexes after a pause anyway.
func (w *Watcher) eventsLost(cause error) {
	if w.overflowQueued || w.isPaused() {
		return
	}
	w.overflowQueued = true
	if errors.Is(cause, fsnotify.ErrEventOverflow) {
		w.urgent = append(w.urgent, fmt.Errorf("watcher: events lost: %w", cause))
		return
	}
	// Not wrapped: the watch works again, and the error must not match
	// ErrWatchStopped.
	w.urgent = append(w.urgent, fmt.Errorf("watcher: events lost: %w (%s)", fsnotify.ErrEventOverflow, cause.Error()))
}

// resync rebuilds the directory watches from disk: it drops watches on
// directories that no longer exist (or were renamed away) and watches every
// directory currently present. It returns the error of a vault root that
// cannot be watched.
func (w *Watcher) resync() error {
	for d := range w.dirs {
		if d == "." {
			continue
		}
		if info, err := os.Lstat(w.abs(d)); err != nil || !info.IsDir() {
			w.unwatchTree(d)
		}
	}
	clear(w.dirs) // re-adding an existing watch is harmless
	clear(w.gaps) // listed again while adding the watches
	errs, err := w.addTree(".", nil)
	for _, e := range errs {
		w.sendError(e)
	}
	return err
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
		if (fe.Has(fsnotify.Remove) || fe.Has(fsnotify.Rename)) && !w.rootGone && !w.rootStillThere() {
			w.rootGone = true
			w.urgent = append(w.urgent, ErrRootGone)
		}
		return false
	}
	if seq, ok := barrierSeqOf(rel); ok {
		if fe.Has(fsnotify.Create) {
			w.releaseBarriers(seq)
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
	// Windows reports a Write on a directory whose entries changed; the
	// entries have their own events.
	if fe.Op == fsnotify.Write {
		if info, err := os.Lstat(fe.Name); err == nil && info.IsDir() {
			return false
		}
	}
	paused := w.isPaused()
	recorded := false
	if w.trackGaps {
		repeat, r := w.gapEvent(rel, fe.Has(fsnotify.Create), now, !paused)
		if repeat {
			// kqueue reports a blocker as created on every change to its
			// directory: only the listing was news.
			return r
		}
		recorded = r
	}

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

// rootStillThere reports whether the root path still names the directory
// New found there.
func (w *Watcher) rootStillThere() bool {
	info, err := os.Stat(w.root)
	return err == nil && os.SameFile(info, w.rootInfo)
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
// cannot be read or watched is skipped: for lack of permission it is logged
// (see skip), otherwise (inotify watch limit) its error is collected in errs.
// fatal is non-nil only when the vault root itself cannot be walked or
// watched.
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
			case errors.Is(err, fs.ErrPermission):
				w.skip(sub, err)
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
		if w.trackGaps && sub != "." && !d.IsDir() && blocks(p, d.Type()) {
			w.markGap(sub)
		}
		if !d.IsDir() || w.dirs[sub] {
			return nil
		}
		if err := w.fsw.Add(p); err != nil {
			if w.trackGaps && sub != "." {
				w.markGap(sub)
			}
			switch {
			case sub == ".":
				return fmt.Errorf("watcher: watch vault root: %w", err)
			case errors.Is(err, fs.ErrNotExist):
				// Vanished while walking.
			case errors.Is(err, fs.ErrPermission):
				w.skip(sub, err)
			default:
				errs = append(errs, fmt.Errorf("watcher: watch %s: %w", sub, err))
			}
			return filepath.SkipDir
		}
		w.dirs[sub] = true
		delete(w.skipped, sub)
		return nil
	})
	return errs, fatal
}

// skip logs that the directory rel is not watched because the user may not
// read it, once until it is removed or becomes readable.
func (w *Watcher) skip(rel string, err error) {
	if w.skipped[rel] {
		return
	}
	w.skipped[rel] = true
	w.log.Warn("watcher: directory not readable, changes inside it are not noticed", "dir", rel, "err", err)
}

// unwatchTree drops the watches on rel and every directory below it, and
// forgets the skipped directories and the gaps there. It must run before a
// directory renamed within the vault is watched again under its new name,
// because the kernel keeps the old watch on the moved directory.
func (w *Watcher) unwatchTree(rel string) {
	prefix := rel + "/"
	for d := range w.dirs {
		if d == rel || strings.HasPrefix(d, prefix) {
			delete(w.dirs, d)
			_ = w.fsw.Remove(w.abs(d)) // already gone if the directory was deleted
		}
	}
	for d := range w.skipped {
		if d == rel || strings.HasPrefix(d, prefix) {
			delete(w.skipped, d)
		}
	}
	for d := range w.gaps {
		if d == rel || strings.HasPrefix(d, prefix) {
			delete(w.gaps, d)
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
