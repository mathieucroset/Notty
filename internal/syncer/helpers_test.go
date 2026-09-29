package syncer

import (
	"context"
	"slices"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mathieucroset/notty/internal/config"
	"github.com/mathieucroset/notty/internal/gitsync"
)

// fakeClock is a manual clock. Advance fires due timers in time order and
// lets the syncer settle (finish every queued job) after each one, so timers
// armed by those jobs are scheduled from a deterministic "now".
type fakeClock struct {
	mu     sync.Mutex
	now    time.Time
	seq    int
	timers []*fakeTimer
	settle func()
}

type fakeTimer struct {
	c       *fakeClock
	at      time.Time
	d       time.Duration
	seq     int
	f       func()
	stopped bool
	fired   bool
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) AfterFunc(d time.Duration, f func()) Timer {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seq++
	t := &fakeTimer{c: c, at: c.now.Add(d), d: d, seq: c.seq, f: f}
	c.timers = append(c.timers, t)
	return t
}

func (t *fakeTimer) Stop() bool {
	t.c.mu.Lock()
	defer t.c.mu.Unlock()
	if t.stopped || t.fired {
		return false
	}
	t.stopped = true
	return true
}

// Advance moves time forward by d, firing every timer that falls due.
func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	target := c.now.Add(d)
	for {
		var next *fakeTimer
		for _, t := range c.timers {
			if t.stopped || t.fired || t.at.After(target) {
				continue
			}
			if next == nil || t.at.Before(next.at) || (t.at.Equal(next.at) && t.seq < next.seq) {
				next = t
			}
		}
		if next == nil {
			break
		}
		next.fired = true
		if next.at.After(c.now) {
			c.now = next.at
		}
		settle := c.settle
		c.mu.Unlock()
		next.f()
		if settle != nil {
			settle()
		}
		c.mu.Lock()
	}
	c.now = target
	c.mu.Unlock()
}

// Pending returns the delays (as scheduled) of the timers that have not
// fired or been stopped, sorted.
func (c *fakeClock) Pending() []time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	var ds []time.Duration
	for _, t := range c.timers {
		if !t.stopped && !t.fired {
			ds = append(ds, t.d)
		}
	}
	slices.Sort(ds)
	return ds
}

// fakeHost records every call in order.
type fakeHost struct {
	mu        sync.Mutex
	log       []string
	unlocks   []map[string]bool
	editing   atomic.Bool
	flushErr  error
	onFlush   func(n int) // called with the 1-based flush count
	flushes   int
	lockDepth int
}

func (h *fakeHost) record(s string) {
	h.mu.Lock()
	h.log = append(h.log, s)
	h.mu.Unlock()
}

func (h *fakeHost) Flush() error {
	h.mu.Lock()
	h.flushes++
	n, f, err := h.flushes, h.onFlush, h.flushErr
	h.log = append(h.log, "Flush")
	h.mu.Unlock()
	if f != nil {
		f(n)
	}
	return err
}

func (h *fakeHost) LockMutations() {
	h.mu.Lock()
	h.lockDepth++
	h.log = append(h.log, "Lock")
	h.mu.Unlock()
}

func (h *fakeHost) UnlockMutations(conflicted map[string]bool) {
	h.mu.Lock()
	h.lockDepth--
	h.log = append(h.log, "Unlock")
	h.unlocks = append(h.unlocks, conflicted)
	h.mu.Unlock()
}

func (h *fakeHost) PauseWatcher()         { h.record("Pause") }
func (h *fakeHost) ResumeWatcher()        { h.record("Resume") }
func (h *fakeHost) ExternalEditing() bool { return h.editing.Load() }

func (h *fakeHost) calls() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.log)
}

func (h *fakeHost) count(name string) int {
	n := 0
	for _, c := range h.calls() {
		if c == name {
			n++
		}
	}
	return n
}

func (h *fakeHost) reset() {
	h.mu.Lock()
	h.log = nil
	h.unlocks = nil
	h.mu.Unlock()
}

// spyRepo wraps a real repository, logging calls into the host's log and
// allowing a hook before Merge.
type spyRepo struct {
	*gitsync.Repo
	host      *fakeHost
	beforeMrg func(n int) // called with the 1-based merge count
	merges    atomic.Int32
	fetches   atomic.Int32
	pushes    atomic.Int32
	addAlls   atomic.Int32
}

func (r *spyRepo) AddAll() error {
	r.addAlls.Add(1)
	return r.Repo.AddAll()
}

func (r *spyRepo) Commit(msg string) (bool, error) {
	r.host.record("Commit")
	return r.Repo.Commit(msg)
}

func (r *spyRepo) Merge(ref string, allowUnrelated bool) error {
	n := int(r.merges.Add(1))
	r.host.record("Merge")
	if r.beforeMrg != nil {
		r.beforeMrg(n)
	}
	return r.Repo.Merge(ref, allowUnrelated)
}

func (r *spyRepo) Fetch(ctx context.Context) error {
	r.fetches.Add(1)
	r.host.record("Fetch")
	return r.Repo.Fetch(ctx)
}

func (r *spyRepo) Push(ctx context.Context, setUpstream bool) error {
	r.pushes.Add(1)
	r.host.record("Push")
	return r.Repo.Push(ctx, setUpstream)
}

// recorder collects emitted updates.
type recorder struct {
	mu      sync.Mutex
	updates []Update
}

func (r *recorder) add(u Update) {
	r.mu.Lock()
	r.updates = append(r.updates, u)
	r.mu.Unlock()
}

func (r *recorder) all() []Update {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.updates)
}

func (r *recorder) states() []State {
	var st []State
	for _, u := range r.all() {
		st = append(st, u.Status.State)
	}
	return st
}

// reindexed returns the union of every Reindex list.
func (r *recorder) reindexed() []string {
	return r.union(func(u Update) []string { return u.Reindex })
}

func (r *recorder) reloaded() []string {
	return r.union(func(u Update) []string { return u.Reload })
}

func (r *recorder) union(get func(Update) []string) []string {
	set := map[string]bool{}
	for _, u := range r.all() {
		for _, p := range get(u) {
			set[p] = true
		}
	}
	out := make([]string, 0, len(set))
	for p := range set {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

func (r *recorder) trashWarnings() []TrashWarning {
	var ws []TrashWarning
	for _, u := range r.all() {
		ws = append(ws, u.TrashWarnings...)
	}
	return ws
}

func (r *recorder) reset() {
	r.mu.Lock()
	r.updates = nil
	r.mu.Unlock()
}

type harness struct {
	t     *testing.T
	s     *Syncer
	host  *fakeHost
	clock *fakeClock
	repo  *spyRepo
	rec   *recorder
	ctx   context.Context
}

// newHarness builds a syncer over repo with a fake host and clock. It does
// not start it.
func newHarness(t *testing.T, repo *gitsync.Repo, mutate ...func(*config.Config)) *harness {
	t.Helper()
	cfg := config.Default()
	for _, m := range mutate {
		m(&cfg)
	}
	host := &fakeHost{}
	clock := newFakeClock()
	s := New(repo, cfg, host, clock)
	spy := &spyRepo{Repo: repo, host: host}
	s.repo = spy
	rec := &recorder{}
	s.onEmit = rec.add
	clock.settle = s.waitIdle
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		s.waitIdle()
		cancel()
	})
	return &harness{t: t, s: s, host: host, clock: clock, repo: spy, rec: rec, ctx: ctx}
}

func (h *harness) start() {
	h.t.Helper()
	h.s.Start(h.ctx)
	h.s.waitIdle()
}

func (h *harness) advance(d time.Duration) {
	h.clock.Advance(d)
	h.s.waitIdle()
}

func (h *harness) wantState(want State) {
	h.t.Helper()
	if got := h.s.Status(); got.State != want {
		h.t.Fatalf("Status() = %+v, want state %v (updates: %v)", got, want, h.rec.states())
	}
}
