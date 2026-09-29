// Package syncer is Notty's auto-sync state machine (spec §7). It commits
// saved notes after a short delay, fetches and merges the remote, pushes,
// and hands conflicts to the UI. Every git operation runs on one worker
// goroutine, in order.
package syncer

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/mathieucroset/notty/internal/config"
	"github.com/mathieucroset/notty/internal/gitsync"
)

// Host is implemented by the UI (and by a fake in tests).
type Host interface {
	// Flush saves the open buffer to disk. The UI gives up after a timeout
	// and returns an error; the syncer then commits what is on disk.
	Flush() error
	// LockMutations locks every action that changes files for the locked
	// merge section (spec §7); UnlockMutations releases them, with the set
	// of paths left conflicted by the merge.
	LockMutations()
	UnlockMutations(conflicted map[string]bool)
	// PauseWatcher and ResumeWatcher bracket the merge, which rewrites the
	// working tree.
	PauseWatcher()
	ResumeWatcher()
	// ExternalEditing reports whether $EDITOR is open; cycles are deferred
	// until ExternalEditDone.
	ExternalEditing() bool
}

// repoAPI is the subset of *gitsync.Repo the syncer uses; tests wrap the real
// repository to observe calls and inject races.
type repoAPI interface {
	HasRemote() bool
	HasUpstream() bool
	CurrentBranch() (string, error)
	MergeInProgress() bool
	AddAll() error
	Commit(msg string) (bool, error)
	Status() ([]gitsync.StatusEntry, error)
	Fetch(ctx context.Context) error
	Push(ctx context.Context, setUpstream bool) error
	AheadBehind() (ahead, behind int, err error)
	Merge(ref string, allowUnrelated bool) error
	MergeBase(a, b string) (string, error)
	DiffNameStatus(from, to string) ([]gitsync.Change, error)
	ConflictedFiles() ([]gitsync.Conflict, error)
	CommitMerge(msg string) error
	PathConflicts() ([]gitsync.PathConflict, []gitsync.Conflict, error)
	ShowBlob(id string) ([]byte, error)
	Add(paths ...string) error
	Remove(paths ...string) error
	trashRepo
}

const (
	defaultCommitDelay   = 5 * time.Second
	defaultFetchInterval = 5 * time.Minute
)

// backoff is the offline retry schedule (spec §7); the last step repeats.
var backoff = []time.Duration{30 * time.Second, time.Minute, 2 * time.Minute, 4 * time.Minute, 5 * time.Minute}

// job is one unit of work for the worker. Only quit jobs run after Quit.
type job struct {
	fn   func()
	quit bool
}

// timerSlot holds one named timer. gen invalidates callbacks of timers that
// were stopped or replaced but fired anyway.
type timerSlot struct {
	t   Timer
	gen int
}

// Syncer runs the auto-sync state machine. Create it with New and call Start.
type Syncer struct {
	repo     repoAPI
	dir      string
	hostname string
	host     Host
	clock    Clock

	enabled       bool
	commitDelay   time.Duration
	fetchInterval time.Duration

	updates chan Update
	onEmit  func(Update) // test hook, set before Start

	startOnce sync.Once
	netCtx    context.Context // background fetches and pushes; Quit cancels it
	netCancel context.CancelFunc

	// Job queue, consumed by one worker goroutine.
	qmu     sync.Mutex
	qcond   *sync.Cond
	queue   []job
	running bool
	stopped bool

	// Outbox of updates, delivered in order by a pump goroutine so the worker
	// never blocks on the UI.
	omu    sync.Mutex
	outbox []Update
	osig   chan struct{}

	mu           sync.Mutex
	status       Status
	localOnly    bool
	quitting     bool
	cyclePending bool
	authBlocked  bool // no automatic retries until a save or a manual sync
	backoffStep  int
	commitTimer  timerSlot
	fetchTimer   timerSlot
	retryTimer   timerSlot
}

// New returns a syncer for repo. clock is RealClock() in production.
func New(repo *gitsync.Repo, cfg config.Config, host Host, clock Clock) *Syncer {
	s := &Syncer{
		repo:          repo,
		dir:           repo.Dir,
		hostname:      repo.Host,
		host:          host,
		clock:         clock,
		enabled:       cfg.Sync.Enabled,
		commitDelay:   time.Duration(cfg.Sync.CommitDelayS) * time.Second,
		fetchInterval: time.Duration(cfg.Sync.FetchIntervalM) * time.Minute,
		updates:       make(chan Update),
		osig:          make(chan struct{}, 1),
		status:        Status{State: Idle},
	}
	if s.commitDelay <= 0 {
		s.commitDelay = defaultCommitDelay
	}
	if s.fetchInterval <= 0 {
		s.fetchInterval = defaultFetchInterval
	}
	s.qcond = sync.NewCond(&s.qmu)
	return s
}

// Updates delivers every state change and merge result, in order.
func (s *Syncer) Updates() <-chan Update { return s.updates }

// Status returns the current status.
func (s *Syncer) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}

// Start runs the startup logic (spec §7 triggers table): Conflict if a merge
// is in progress, LocalOnly without a remote or with sync disabled, else a
// full cycle. The worker stops when ctx is done.
func (s *Syncer) Start(ctx context.Context) {
	s.ensureWorker(ctx)
	s.enqueue(job{fn: s.startLogic})
}

// NoteChanged (re)starts the commit timer after a save or a watcher event.
// It is ignored in Conflict. It also re-enables automatic retries after an
// authentication failure.
func (s *Syncer) NoteChanged(rel string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.quitting || s.status.State == Conflict {
		return
	}
	s.authBlocked = false
	s.arm(&s.commitTimer, s.commitDelay, s.requestCycle)
}

// SyncNow runs a full cycle immediately. In Conflict it does nothing: the UI
// opens the resolver instead.
func (s *Syncer) SyncNow() {
	s.mu.Lock()
	if s.quitting || s.status.State == Conflict {
		s.mu.Unlock()
		return
	}
	s.authBlocked = false
	s.disarm(&s.commitTimer)
	s.mu.Unlock()
	s.requestCycle()
}

// ConflictResolved is called by the resolver after it committed the merge.
// The syncer leaves Conflict, runs a full cycle (which commits edits made
// during the conflict and pushes the merge) and resumes its timers. If a
// merge is still in progress with conflicts left, it stays in Conflict.
func (s *Syncer) ConflictResolved() {
	s.enqueue(job{fn: func() {
		if s.repo.MergeInProgress() {
			if !s.concludeMerge() {
				return
			}
		} else {
			s.emitTrashWarnings()
		}
		s.mu.Lock()
		s.status = Status{State: Idle}
		s.mu.Unlock()
		s.armFetch()
		s.cycle()
	}})
}

// Quit flushes the buffer (unless skipFlush), commits and, with a remote,
// pushes within ctx's deadline (spec §7, amendment A7). It never blocks past
// ctx and returns the push error, if any; the caller quits regardless.
func (s *Syncer) Quit(ctx context.Context, skipFlush bool) error {
	s.ensureWorker(context.Background())
	s.mu.Lock()
	s.quitting = true
	s.disarm(&s.commitTimer)
	s.disarm(&s.fetchTimer)
	s.disarm(&s.retryTimer)
	s.mu.Unlock()
	// Abort a background fetch or push so the quit job runs sooner.
	s.netCancel()

	done := make(chan error, 1)
	s.enqueue(job{quit: true, fn: func() { done <- s.quitJob(ctx, skipFlush) }})
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return fmt.Errorf("syncer: quit: %w", ctx.Err())
	}
}

func (s *Syncer) quitJob(ctx context.Context, skipFlush bool) error {
	if ctx.Err() != nil {
		return fmt.Errorf("syncer: quit: %w", ctx.Err())
	}
	if !skipFlush {
		_ = s.host.Flush()
	}
	// In Conflict the merge stays in progress for the next start: no commit,
	// no push (spec §7).
	if s.Status().State == Conflict || s.repo.MergeInProgress() {
		return nil
	}
	if err := s.commitAll(); err != nil {
		return err
	}
	s.mu.Lock()
	localOnly := s.localOnly
	s.mu.Unlock()
	if localOnly || !s.repo.HasRemote() {
		return nil
	}
	if !s.needsPush() {
		return nil
	}
	s.setState(Pushing)
	if err := s.repo.Push(ctx, !s.repo.HasUpstream()); err != nil {
		return fmt.Errorf("syncer: quit: %w", err)
	}
	s.setStatus(Status{State: Synced})
	return nil
}

// ensureWorker starts the worker and the update pump once.
func (s *Syncer) ensureWorker(ctx context.Context) {
	s.startOnce.Do(func() {
		s.netCtx, s.netCancel = context.WithCancel(ctx)
		go s.work(ctx)
		go s.pump(ctx)
		go func() {
			<-ctx.Done()
			s.qmu.Lock()
			s.qcond.Broadcast()
			s.qmu.Unlock()
		}()
	})
}

func (s *Syncer) enqueue(j job) {
	s.qmu.Lock()
	s.queue = append(s.queue, j)
	s.qcond.Broadcast()
	s.qmu.Unlock()
}

func (s *Syncer) work(ctx context.Context) {
	for {
		s.qmu.Lock()
		for len(s.queue) == 0 && ctx.Err() == nil {
			s.qcond.Wait()
		}
		if ctx.Err() != nil {
			s.stopped = true
			s.queue = nil
			s.qcond.Broadcast()
			s.qmu.Unlock()
			return
		}
		j := s.queue[0]
		s.queue = s.queue[1:]
		s.running = true
		s.qmu.Unlock()

		if j.quit || !s.isQuitting() {
			j.fn()
		}

		s.qmu.Lock()
		s.running = false
		s.qcond.Broadcast()
		s.qmu.Unlock()
	}
}

// waitIdle blocks until the queue is empty and no job runs (tests only).
func (s *Syncer) waitIdle() {
	s.qmu.Lock()
	for (len(s.queue) > 0 || s.running) && !s.stopped {
		s.qcond.Wait()
	}
	s.qmu.Unlock()
}

func (s *Syncer) pump(ctx context.Context) {
	for {
		s.omu.Lock()
		if len(s.outbox) == 0 {
			s.omu.Unlock()
			select {
			case <-s.osig:
				continue
			case <-ctx.Done():
				return
			}
		}
		u := s.outbox[0]
		s.outbox = s.outbox[1:]
		s.omu.Unlock()
		select {
		case s.updates <- u:
		case <-ctx.Done():
			return
		}
	}
}

// emit queues u for the UI. It is only called from the worker.
func (s *Syncer) emit(u Update) {
	if s.onEmit != nil {
		s.onEmit(u)
	}
	s.omu.Lock()
	s.outbox = append(s.outbox, u)
	s.omu.Unlock()
	select {
	case s.osig <- struct{}{}:
	default:
	}
}

// setStatus records st and emits it if it differs from the current status.
func (s *Syncer) setStatus(st Status) {
	s.mu.Lock()
	changed := !sameStatus(s.status, st)
	s.status = st
	s.mu.Unlock()
	if changed {
		s.emit(Update{Status: st})
	}
}

func (s *Syncer) setState(state State) { s.setStatus(Status{State: state}) }

func sameStatus(a, b Status) bool {
	if a.State != b.State || a.Pending != b.Pending || a.Conflicts != b.Conflicts || a.Detail != b.Detail {
		return false
	}
	if (a.Err == nil) != (b.Err == nil) {
		return false
	}
	return a.Err == nil || a.Err.Error() == b.Err.Error()
}

func (s *Syncer) isQuitting() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.quitting
}

// arm (re)starts a timer slot; fire runs on the clock's goroutine. Must be
// called with s.mu held.
func (s *Syncer) arm(slot *timerSlot, d time.Duration, fire func()) {
	if slot.t != nil {
		slot.t.Stop()
	}
	slot.gen++
	gen := slot.gen
	slot.t = s.clock.AfterFunc(d, func() {
		s.mu.Lock()
		if slot.gen != gen || s.quitting {
			s.mu.Unlock()
			return
		}
		slot.t = nil
		s.mu.Unlock()
		fire()
	})
}

// disarm stops a timer slot. Must be called with s.mu held.
func (s *Syncer) disarm(slot *timerSlot) {
	if slot.t != nil {
		slot.t.Stop()
		slot.t = nil
	}
	slot.gen++
}

// requestCycle queues a full cycle unless one is already queued.
func (s *Syncer) requestCycle() {
	s.mu.Lock()
	if s.cyclePending {
		s.mu.Unlock()
		return
	}
	s.cyclePending = true
	s.mu.Unlock()
	s.enqueue(job{fn: func() {
		s.mu.Lock()
		s.cyclePending = false
		s.mu.Unlock()
		s.cycle()
	}})
}

func (s *Syncer) startLogic() {
	localOnly := !s.enabled || !s.repo.HasRemote()
	s.mu.Lock()
	s.localOnly = localOnly
	if localOnly {
		s.disarm(&s.fetchTimer)
	}
	s.mu.Unlock()
	if s.repo.MergeInProgress() && !s.concludeMerge() {
		return
	}
	if localOnly {
		s.setState(LocalOnly)
		return
	}
	s.armFetch()
	s.cycle()
}

// armFetch schedules the next fetch tick unless sync is local-only.
func (s *Syncer) armFetch() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.quitting || s.localOnly {
		return
	}
	s.arm(&s.fetchTimer, s.fetchInterval, func() { s.enqueue(job{fn: s.fetchTick}) })
}

// concludeMerge handles a merge found in progress: with conflicts left it
// enters Conflict and returns false; with every file resolved (the app
// stopped before committing the merge) it commits the merge and returns true.
func (s *Syncer) concludeMerge() bool {
	paths, err := s.conflictedPaths()
	if err != nil {
		s.fail(err)
		return false
	}
	if len(paths) > 0 {
		s.enterConflict(Update{}, paths)
		return false
	}
	if err := s.repo.CommitMerge("Merge · " + s.hostname); err != nil {
		s.fail(fmt.Errorf("syncer: %w", err))
		return false
	}
	s.emitTrashWarnings()
	return true
}

// emitTrashWarnings checks the merge commit at HEAD (made on top of
// ORIG_HEAD after a conflict) for notes trashed remotely but edited here.
func (s *Syncer) emitTrashWarnings() {
	if revID(s.repo, "HEAD^2") == "" || revID(s.repo, "HEAD^1") != revID(s.repo, "ORIG_HEAD") {
		return
	}
	ws, err := detectTrashWarnings(s.repo, s.dir, "ORIG_HEAD", s.hostname)
	if err != nil || len(ws) == 0 {
		return
	}
	s.emit(Update{Status: s.Status(), TrashWarnings: ws})
}

// conflictedPaths lists the unmerged paths, sorted.
func (s *Syncer) conflictedPaths() ([]string, error) {
	conflicts, err := s.repo.ConflictedFiles()
	if err != nil {
		return nil, fmt.Errorf("syncer: %w", err)
	}
	paths := make([]string, 0, len(conflicts))
	for _, c := range conflicts {
		paths = append(paths, c.Path)
	}
	slices.Sort(paths)
	return paths, nil
}

// enterConflict pauses every timer and emits u with the Conflict status and
// the conflicted paths.
func (s *Syncer) enterConflict(u Update, paths []string) {
	st := Status{State: Conflict, Conflicts: len(paths)}
	s.mu.Lock()
	s.disarm(&s.commitTimer)
	s.disarm(&s.fetchTimer)
	s.disarm(&s.retryTimer)
	s.status = st
	s.mu.Unlock()
	u.Status = st
	u.Conflicted = paths
	s.emit(u)
}

// cycle is the full cycle of spec §7.
func (s *Syncer) cycle() {
	if s.Status().State == Conflict {
		return
	}
	if s.repo.MergeInProgress() && !s.concludeMerge() {
		return
	}
	s.setState(Committing)
	_ = s.host.Flush()
	if err := s.commitAll(); err != nil {
		s.fail(err)
		return
	}
	s.mu.Lock()
	localOnly := s.localOnly
	s.mu.Unlock()
	if localOnly {
		s.setState(LocalOnly)
		return
	}
	s.setState(Pulling)
	if err := s.repo.Fetch(s.netCtx); err != nil {
		s.fail(err)
		return
	}
	s.afterFetch()
}

// fetchTick is the fetch timer (spec §7): fetch; if the remote is ahead run
// the rest of a full cycle, if only local is ahead push.
func (s *Syncer) fetchTick() {
	s.mu.Lock()
	conflict := s.status.State == Conflict
	skip := s.authBlocked
	s.mu.Unlock()
	if conflict {
		return // the timer resumes when the conflict is resolved
	}
	defer s.armFetch()
	if skip {
		return
	}
	s.setState(Pulling)
	if err := s.repo.Fetch(s.netCtx); err != nil {
		s.fail(err)
		return
	}
	_, behind, err := s.repo.AheadBehind()
	if err != nil {
		s.fail(err)
		return
	}
	if behind > 0 {
		_ = s.host.Flush()
		if err := s.commitAll(); err != nil {
			s.fail(err)
			return
		}
	}
	s.afterFetch()
}

// afterFetch runs steps 3 and 4 of the full cycle: merge when behind, push
// when ahead.
func (s *Syncer) afterFetch() {
	_, behind, err := s.repo.AheadBehind()
	if err != nil {
		s.fail(err)
		return
	}
	if behind > 0 {
		conflicted, err := s.mergeSection()
		if err != nil {
			s.fail(err)
			return
		}
		if conflicted {
			return
		}
	}
	if s.needsPush() {
		s.setState(Pushing)
		if err := s.repo.Push(s.netCtx, !s.repo.HasUpstream()); err != nil {
			s.fail(err)
			return
		}
	}
	s.mu.Lock()
	s.backoffStep = 0
	s.authBlocked = false
	s.disarm(&s.retryTimer)
	s.mu.Unlock()
	s.setStatus(Status{State: Synced})
}

// needsPush reports whether local commits are not on the remote yet, or the
// branch has commits but no upstream.
func (s *Syncer) needsPush() bool {
	ahead, _, err := s.repo.AheadBehind()
	if err != nil {
		return false
	}
	if ahead > 0 {
		return true
	}
	return !s.repo.HasUpstream() && s.headExists()
}

func (s *Syncer) headExists() bool { return revID(s.repo, "HEAD") != "" }

// mergeSection is the locked merge section of spec §7. It reports whether
// the merge stopped with conflicts (the syncer is then in Conflict).
func (s *Syncer) mergeSection() (conflicted bool, err error) {
	s.setState(Merging)
	branch, err := s.repo.CurrentBranch()
	if err != nil {
		return false, fmt.Errorf("syncer: merge: %w", err)
	}
	ref := "origin/" + branch

	conflictSet := map[string]bool{}
	s.host.LockMutations()
	s.host.PauseWatcher()
	defer func() {
		s.host.ResumeWatcher()
		s.host.UnlockMutations(conflictSet)
	}()

	_ = s.host.Flush()
	if err := s.commitAll(); err != nil {
		return false, err
	}
	err = s.repo.Merge(ref, false)
	if errors.Is(err, gitsync.ErrLocalChanges) {
		// Another program changed a file in the same instant: commit and
		// retry once.
		if cerr := s.commitAll(); cerr != nil {
			return false, cerr
		}
		err = s.repo.Merge(ref, false)
	}
	stopped := errors.Is(err, gitsync.ErrConflict) && s.repo.MergeInProgress()
	if err != nil && !stopped {
		return false, fmt.Errorf("syncer: merge: %w", err)
	}
	var paths []string
	if stopped {
		if err := s.autoResolve(); err != nil {
			return false, err
		}
		if paths, err = s.conflictedPaths(); err != nil {
			return false, err
		}
	}
	reindex, reload := s.reindex()
	u := Update{Reindex: reindex, Reload: reload}
	if len(paths) > 0 {
		for _, p := range paths {
			conflictSet[p] = true
		}
		s.enterConflict(u, paths)
		return true, nil
	}
	if stopped {
		// Nothing is left unmerged: conclude the merge.
		if err := s.repo.CommitMerge("Merge · " + s.hostname); err != nil {
			return false, fmt.Errorf("syncer: merge: %w", err)
		}
	}
	// Warnings are best effort: a failed check must not fail the sync.
	u.TrashWarnings, _ = detectTrashWarnings(s.repo, s.dir, "ORIG_HEAD", s.hostname)
	u.Status = s.Status()
	s.emit(u)
	return false, nil
}

// reindex returns the paths changed by the last merge (spec §7: every file in
// DiffNameStatus(ORIG_HEAD, "")) and those of them that still exist.
func (s *Syncer) reindex() (reindex, reload []string) {
	changes, err := s.repo.DiffNameStatus("ORIG_HEAD", "")
	if err != nil {
		return nil, nil
	}
	seen := map[string]bool{}
	for _, c := range changes {
		for _, p := range []string{c.Path, c.OldPath} {
			if p != "" && !seen[p] {
				seen[p] = true
				reindex = append(reindex, p)
			}
		}
	}
	slices.Sort(reindex)
	for _, p := range reindex {
		if fi, err := os.Stat(filepath.Join(s.dir, filepath.FromSlash(p))); err == nil && fi.Mode().IsRegular() {
			reload = append(reload, p)
		}
	}
	return reindex, reload
}

// commitAll stages everything and commits it with an automatic message.
func (s *Syncer) commitAll() error {
	if err := s.repo.AddAll(); err != nil {
		return fmt.Errorf("syncer: commit: %w", err)
	}
	entries, err := s.repo.Status()
	if err != nil {
		return fmt.Errorf("syncer: commit: %w", err)
	}
	changes := stagedChanges(entries)
	if len(changes) == 0 {
		return nil
	}
	if _, err := s.repo.Commit(CommitMessage(changes, s.hostname)); err != nil {
		return fmt.Errorf("syncer: commit: %w", err)
	}
	return nil
}

// stagedChanges converts the index side of status entries into changes.
func stagedChanges(entries []gitsync.StatusEntry) []gitsync.Change {
	var changes []gitsync.Change
	for _, e := range entries {
		if (e.Kind != '1' && e.Kind != '2') || len(e.XY) != 2 {
			continue
		}
		switch e.XY[0] {
		case 'A', 'C':
			changes = append(changes, gitsync.Change{Status: 'A', Path: e.Path})
		case 'M', 'T':
			changes = append(changes, gitsync.Change{Status: 'M', Path: e.Path})
		case 'D':
			changes = append(changes, gitsync.Change{Status: 'D', Path: e.Path})
		case 'R':
			changes = append(changes, gitsync.Change{Status: 'R', Path: e.Path, OldPath: e.OrigPath})
		}
	}
	return changes
}

// fail records a failed step: Offline with a retry on network errors
// (spec §7 backoff), Error without automatic retries on authentication
// errors, Error otherwise. Failures after Quit (whose network context is
// cancelled) are ignored.
func (s *Syncer) fail(err error) {
	if s.isQuitting() {
		return
	}
	switch {
	case errors.Is(err, gitsync.ErrNetwork):
		ahead, _, _ := s.repo.AheadBehind()
		s.mu.Lock()
		if s.retryTimer.t == nil {
			d := backoff[min(s.backoffStep, len(backoff)-1)]
			s.backoffStep++
			s.arm(&s.retryTimer, d, s.requestCycle)
		}
		s.mu.Unlock()
		s.setStatus(Status{State: Offline, Pending: ahead})
	case errors.Is(err, gitsync.ErrAuth):
		s.mu.Lock()
		s.authBlocked = true
		s.disarm(&s.retryTimer)
		s.mu.Unlock()
		s.setStatus(Status{State: Error, Err: err, Detail: "auth"})
	default:
		s.setStatus(Status{State: Error, Err: err})
	}
}
