package syncer

import (
	"errors"
	"path/filepath"
	"slices"
	"testing"
	"time"

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
