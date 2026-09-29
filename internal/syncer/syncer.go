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
}

const (
	defaultCommitDelay   = 5 * time.Second
	defaultFetchInterval = 5 * time.Minute
)

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
	commitTimer  timerSlot
	fetchTimer   timerSlot
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
func (s *Syncer) NoteChanged(rel string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.quitting {
		return
	}
	s.arm(&s.commitTimer, s.commitDelay, s.requestCycle)
}

// SyncNow runs a full cycle immediately.
func (s *Syncer) SyncNow() {
	s.mu.Lock()
	if s.quitting {
		s.mu.Unlock()
		return
	}
	s.disarm(&s.commitTimer)
	s.mu.Unlock()
	s.requestCycle()
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

// cycle is the full cycle of spec §7.
func (s *Syncer) cycle() {
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
	defer s.armFetch()
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
		if err := s.mergeSection(); err != nil {
			s.fail(err)
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

func (s *Syncer) headExists() bool {
	_, err := s.repo.MergeBase("HEAD", "HEAD")
	return err == nil
}

// mergeSection is the locked merge section of spec §7.
func (s *Syncer) mergeSection() (err error) {
	s.setState(Merging)
	branch, err := s.repo.CurrentBranch()
	if err != nil {
		return fmt.Errorf("syncer: merge: %w", err)
	}
	ref := "origin/" + branch

	conflicted := map[string]bool{}
	s.host.LockMutations()
	s.host.PauseWatcher()
	defer func() {
		s.host.ResumeWatcher()
		s.host.UnlockMutations(conflicted)
	}()

	_ = s.host.Flush()
	if err := s.commitAll(); err != nil {
		return err
	}
	err = s.repo.Merge(ref, false)
	if errors.Is(err, gitsync.ErrLocalChanges) {
		// Another program changed a file in the same instant: commit and
		// retry once.
		if cerr := s.commitAll(); cerr != nil {
			return cerr
		}
		err = s.repo.Merge(ref, false)
	}
	if err != nil {
		return fmt.Errorf("syncer: merge: %w", err)
	}
	reindex, reload := s.reindex()
	s.emit(Update{Status: s.Status(), Reindex: reindex, Reload: reload})
	return nil
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

// fail enters Error with err.
func (s *Syncer) fail(err error) {
	s.setStatus(Status{State: Error, Err: err})
}
