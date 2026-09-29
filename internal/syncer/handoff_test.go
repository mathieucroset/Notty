package syncer

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/mathieucroset/notty/internal/config"
	"github.com/mathieucroset/notty/internal/gitsync"
	"github.com/mathieucroset/notty/internal/gitsync/gittest"
)

func TestExternalEditingDefersCycles(t *testing.T) {
	env := gittest.New(t)
	h := newHarness(t, env.Laptop)
	h.start()
	fetches := h.repo.fetches.Load()

	h.host.editing.Store(true)
	gittest.Write(t, env.Laptop, "ext.md", "edited in $EDITOR\n")
	h.s.NoteChanged("ext.md")
	h.advance(5 * time.Second) // commit timer
	h.s.SyncNow()              // palette
	h.s.waitIdle()
	h.advance(5 * time.Minute) // fetch timer
	if got := h.repo.fetches.Load(); got != fetches {
		t.Fatalf("fetched %d times while $EDITOR was open", got-fetches)
	}
	if h.host.count("Commit") != 0 || remoteHas(t, env, "ext.md") {
		t.Fatalf("committed while $EDITOR was open")
	}
	// The fetch timer keeps ticking (and deferring) while the editor is open.
	if got, want := h.clock.Pending(), []time.Duration{5 * time.Minute}; !slices.Equal(got, want) {
		t.Fatalf("pending timers = %v, want the fetch timer", got)
	}

	h.host.editing.Store(false)
	h.s.ExternalEditDone()
	h.s.waitIdle()
	if !remoteHas(t, env, "ext.md") {
		t.Fatalf("deferred cycle did not run after the editor exited")
	}
	h.wantState(Synced)
	if got := h.repo.fetches.Load(); got != fetches+1 {
		t.Fatalf("fetches = %d, want exactly one deferred cycle", got-fetches)
	}

	// Nothing deferred: ExternalEditDone does nothing.
	h.s.ExternalEditDone()
	h.s.waitIdle()
	if got := h.repo.fetches.Load(); got != fetches+1 {
		t.Fatalf("ExternalEditDone ran a cycle with nothing deferred")
	}
}

// editOnFetch opens $EDITOR during the fetch, i.e. between the unlocked
// fetch and the locked merge section.
type editOnFetch struct {
	*spyRepo
	h *fakeHost
}

func (r *editOnFetch) Fetch(ctx context.Context) error {
	err := r.spyRepo.Fetch(ctx)
	r.h.editing.Store(true)
	return err
}

func TestEditorOpenedDuringFetchDefersMerge(t *testing.T) {
	env := gittest.New(t)
	shareBase(t, env, func(r *gitsync.Repo) { gittest.Write(t, r, "note.md", "line\n") })
	h := newHarness(t, env.Laptop)
	h.start()
	deskPush(t, env, func(r *gitsync.Repo) { gittest.Write(t, r, "note.md", "desktop\n") })
	h.s.repo = &editOnFetch{spyRepo: h.repo, h: h.host}
	h.host.reset()
	h.rec.reset()
	h.s.SyncNow()
	h.s.waitIdle()

	if got := h.repo.merges.Load(); got != 0 {
		t.Fatalf("merged %d times while $EDITOR was open", got)
	}
	if got := gittest.Read(t, env.Laptop, "note.md"); got != "line\n" {
		t.Fatalf("note.md = %q, changed under the open editor", got)
	}
	if h.repo.pushes.Load() != 0 || slices.Contains(h.rec.states(), Synced) {
		t.Fatalf("pushed or reported Synced after deferring: states %v", h.rec.states())
	}
	want := []string{"Flush", "Fetch", "Lock", "Pause", "Resume", "Unlock"}
	if got := h.host.calls(); !slices.Equal(got, want) {
		t.Fatalf("calls = %v, want %v", got, want)
	}
	if h.host.lockDepth != 0 {
		t.Fatalf("mutations left locked")
	}

	h.s.repo = h.repo
	h.host.editing.Store(false)
	h.s.ExternalEditDone()
	h.s.waitIdle()
	h.wantState(Synced)
	if got := gittest.Read(t, env.Laptop, "note.md"); got != "desktop\n" {
		t.Fatalf("deferred cycle did not merge: note.md = %q", got)
	}
}

func TestStartWhileExternalEditingDefers(t *testing.T) {
	env := gittest.New(t)
	h := newHarness(t, env.Laptop)
	h.host.editing.Store(true)
	h.start()
	if h.repo.fetches.Load() != 0 || h.host.count("Flush") != 0 {
		t.Fatalf("startup cycle ran while $EDITOR was open")
	}
	h.host.editing.Store(false)
	h.s.ExternalEditDone()
	h.s.waitIdle()
	h.wantState(Synced)
	if h.repo.fetches.Load() != 1 {
		t.Fatalf("deferred startup cycle did not run")
	}
}

func newLocalRepo(t *testing.T) *gitsync.Repo {
	t.Helper()
	gittest.Isolate(t)
	repo, err := gitsync.Init(filepath.Join(t.TempDir(), "vault"), "main")
	if err != nil {
		t.Fatal(err)
	}
	repo.Host = "laptop"
	gittest.SetUser(t, repo, "Laptop User", "laptop@example.com")
	return repo
}

func TestRunSetupAddsRemote(t *testing.T) {
	repo := newLocalRepo(t)
	gittest.Write(t, repo, "local.md", "written before sync was set up\n")
	gittest.CommitAll(t, repo, "Create local.md · laptop")
	remote := gittest.NewEmptyRemote(t)

	h := newHarness(t, repo)
	h.start()
	h.wantState(LocalOnly)

	errc := h.s.RunSetup(func(r *gitsync.Repo) error { return r.RemoteAdd(remote) })
	if err := <-errc; err != nil {
		t.Fatalf("RunSetup: %v", err)
	}
	h.s.waitIdle()

	h.wantState(Synced)
	if got := gittest.Git(t, remote, "log", "-1", "--format=%s", "main"); got != "Create local.md · laptop" {
		t.Fatalf("remote subject = %q", got)
	}
	if !repo.HasUpstream() {
		t.Fatalf("upstream not set by the first push")
	}
	if got, want := h.clock.Pending(), []time.Duration{5 * time.Minute}; !slices.Equal(got, want) {
		t.Fatalf("pending timers = %v, want the fetch timer", got)
	}
}

func TestRunSetupReturnsJobError(t *testing.T) {
	repo := newLocalRepo(t)
	h := newHarness(t, repo)
	h.start()
	boom := errors.New("boom")
	ran := false
	errc := h.s.RunSetup(func(r *gitsync.Repo) error {
		ran = r.Dir == repo.Dir
		return boom
	})
	if err := <-errc; !errors.Is(err, boom) {
		t.Fatalf("RunSetup error = %v, want boom", err)
	}
	h.s.waitIdle()
	if !ran {
		t.Fatalf("job did not receive the syncer's repository")
	}
	h.wantState(LocalOnly)
}

// waitStopped blocks until the worker has exited.
func waitStopped(s *Syncer) {
	s.qmu.Lock()
	for !s.stopped {
		s.qcond.Wait()
	}
	s.qmu.Unlock()
}

func TestRunSetupAfterQuitAnswers(t *testing.T) {
	env := gittest.New(t)
	h := newHarness(t, env.Laptop)
	h.start()

	// Queued before Quit, skipped because of it: still answered.
	release, started := make(chan struct{}), make(chan struct{})
	h.s.enqueue(job{fn: func() { close(started); <-release }})
	<-started
	early := h.s.RunSetup(func(*gitsync.Repo) error { return nil })
	quitDone := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		quitDone <- h.s.Quit(ctx, true)
	}()
	for !h.s.isQuitting() {
		time.Sleep(time.Millisecond)
	}
	close(release)
	if err := <-quitDone; err != nil {
		t.Fatalf("Quit: %v", err)
	}
	for name, errc := range map[string]<-chan error{
		"queued before Quit": early,
		"after Quit":         h.s.RunSetup(func(*gitsync.Repo) error { return nil }),
	} {
		select {
		case err := <-errc:
			if !errors.Is(err, errStopped) {
				t.Fatalf("RunSetup %s = %v, want errStopped", name, err)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("RunSetup %s never answered", name)
		}
	}
}

func TestNoGoroutinesLeftAfterQuitWithoutStart(t *testing.T) {
	env := gittest.New(t)
	s := New(env.Laptop, config.Default(), &fakeHost{}, newFakeClock())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.Quit(ctx, true); err != nil {
		t.Fatalf("Quit: %v", err)
	}
	waitStopped(s) // hangs (test timeout) if the worker leaked
}

func TestStartLinksContextAfterEarlyRunSetup(t *testing.T) {
	repo := newLocalRepo(t)
	s := New(repo, config.Default(), &fakeHost{}, newFakeClock())
	if err := <-s.RunSetup(func(*gitsync.Repo) error { return nil }); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.Start(ctx)
	s.waitIdle()
	cancel()
	waitStopped(s) // hangs (test timeout) if Start did not link its context
}

func TestRunSetupSerializedWithCycles(t *testing.T) {
	env := gittest.New(t)
	h := newHarness(t, env.Laptop)
	h.start()
	// Occupy the worker: the setup job must wait for it.
	release, started := make(chan struct{}), make(chan struct{})
	h.s.enqueue(job{fn: func() { close(started); <-release }})
	<-started
	ran := make(chan struct{})
	errc := h.s.RunSetup(func(*gitsync.Repo) error { close(ran); return nil })
	select {
	case <-ran:
		t.Fatalf("setup job ran concurrently with another job")
	default:
	}
	close(release)
	if err := <-errc; err != nil {
		t.Fatal(err)
	}
	h.s.waitIdle()
	h.wantState(Synced)
}
