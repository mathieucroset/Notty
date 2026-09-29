package syncer

import (
	"context"
	"errors"
	"maps"
	"slices"
	"testing"
	"time"

	"github.com/mathieucroset/notty/internal/gitsync"
	"github.com/mathieucroset/notty/internal/gitsync/gittest"
)

// shareBase commits base on the laptop, pushes it and syncs the desktop.
func shareBase(t *testing.T, env *gittest.Env, base func(r *gitsync.Repo)) {
	t.Helper()
	base(env.Laptop)
	gittest.CommitAll(t, env.Laptop, "base · laptop")
	gittest.Push(t, env.Laptop)
	gittest.Sync(t, env.Desktop)
}

// reopen opens a fresh Repo on r's directory, as a restarted app would.
func reopen(r *gitsync.Repo) *gitsync.Repo {
	fresh := gitsync.Open(r.Dir)
	fresh.Host = r.Host
	return fresh
}

func head(t *testing.T, r *gitsync.Repo) string {
	t.Helper()
	return gittest.Git(t, r.Dir, "rev-parse", "HEAD")
}

// conflictHarness leaves the laptop syncer in Conflict on note.md, with a
// clean desktop file merged alongside.
func conflictHarness(t *testing.T) (*gittest.Env, *harness) {
	t.Helper()
	env := gittest.New(t)
	shareBase(t, env, func(r *gitsync.Repo) { gittest.Write(t, r, "note.md", "line\n") })
	h := newHarness(t, env.Laptop)
	h.start()
	h.wantState(Synced)
	deskPush(t, env, func(r *gitsync.Repo) {
		gittest.Write(t, r, "note.md", "desktop\n")
		gittest.Write(t, r, "clean.md", "clean from desktop\n")
	})
	gittest.Write(t, env.Laptop, "note.md", "laptop\n")
	h.rec.reset()
	h.host.reset()
	h.s.NoteChanged("note.md")
	h.advance(5 * time.Second)
	return env, h
}

func TestTextConflictEntersConflict(t *testing.T) {
	env, h := conflictHarness(t)

	st := h.s.Status()
	if st.State != Conflict || st.Conflicts != 1 {
		t.Fatalf("Status() = %+v, want Conflict with 1 conflict", st)
	}
	if !env.Laptop.MergeInProgress() {
		t.Fatalf("merge not in progress")
	}
	if len(h.host.unlocks) != 1 || !maps.Equal(h.host.unlocks[0], map[string]bool{"note.md": true}) {
		t.Fatalf("UnlockMutations got %v, want [{note.md:true}]", h.host.unlocks)
	}
	updates := h.rec.all()
	last := updates[len(updates)-1]
	if last.Status.State != Conflict || !slices.Equal(last.Conflicted, []string{"note.md"}) {
		t.Fatalf("last update = %+v, want Conflict with Conflicted [note.md]", last)
	}
	if got, want := h.rec.reindexed(), []string{"clean.md", "note.md"}; !slices.Equal(got, want) {
		t.Fatalf("Reindex = %v, want %v", got, want)
	}
	if got := h.clock.Pending(); len(got) != 0 {
		t.Fatalf("timers still pending in Conflict: %v", got)
	}
	if h.repo.pushes.Load() != 0 {
		t.Fatalf("pushed while conflicted")
	}

	// Saves and "Sync now" neither commit nor stage anything.
	addAlls := h.repo.addAlls.Load()
	gittest.Write(t, env.Laptop, "other.md", "edited during the conflict\n")
	h.s.NoteChanged("other.md")
	h.s.SyncNow()
	h.advance(10 * time.Minute)
	if got := h.repo.addAlls.Load(); got != addAlls {
		t.Fatalf("AddAll called %d times while conflicted", got-addAlls)
	}
	if got := h.clock.Pending(); len(got) != 0 {
		t.Fatalf("NoteChanged armed a timer in Conflict: %v", got)
	}
	h.wantState(Conflict)
}

// failPathConflicts makes auto-resolution fail.
type failPathConflicts struct{ *spyRepo }

func (r *failPathConflicts) PathConflicts() ([]gitsync.PathConflict, []gitsync.Conflict, error) {
	return nil, nil, errors.New("boom")
}

// failConflictedFiles fails the first n ConflictedFiles calls (n < 0: all).
type failConflictedFiles struct {
	*spyRepo
	n int
}

func (r *failConflictedFiles) ConflictedFiles() ([]gitsync.Conflict, error) {
	if r.n != 0 {
		r.n--
		return nil, errors.New("ls-files failed")
	}
	return r.spyRepo.ConflictedFiles()
}

func TestConflictedSetSurvivesFailures(t *testing.T) {
	tests := []struct {
		name       string
		wrap       func(*spyRepo) repoAPI
		wantUnlock map[string]bool
	}{
		{"auto-resolve fails", func(r *spyRepo) repoAPI { return &failPathConflicts{r} }, map[string]bool{"note.md": true}},
		{"listing fails once", func(r *spyRepo) repoAPI { return &failConflictedFiles{r, 1} }, map[string]bool{"note.md": true}},
		{"listing always fails", func(r *spyRepo) repoAPI { return &failConflictedFiles{r, -1} }, map[string]bool{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := gittest.New(t)
			shareBase(t, env, func(r *gitsync.Repo) { gittest.Write(t, r, "note.md", "line\n") })
			h := newHarness(t, env.Laptop)
			h.start()
			h.s.repo = tt.wrap(h.repo)
			deskPush(t, env, func(r *gitsync.Repo) { gittest.Write(t, r, "note.md", "desktop\n") })
			gittest.Write(t, env.Laptop, "note.md", "laptop\n")
			h.host.reset()
			h.s.NoteChanged("note.md")
			h.advance(5 * time.Second)

			h.wantState(Conflict)
			if !env.Laptop.MergeInProgress() {
				t.Fatalf("merge not in progress")
			}
			if len(h.host.unlocks) != 1 || !maps.Equal(h.host.unlocks[0], tt.wantUnlock) {
				t.Fatalf("UnlockMutations got %v, want [%v]", h.host.unlocks, tt.wantUnlock)
			}
			if got := h.clock.Pending(); len(got) != 0 {
				t.Fatalf("timers pending in Conflict: %v", got)
			}
		})
	}
}

func TestStartConflictWhenListingFails(t *testing.T) {
	env := gittest.New(t)
	shareBase(t, env, func(r *gitsync.Repo) { gittest.Write(t, r, "note.md", "line\n") })
	deskPush(t, env, func(r *gitsync.Repo) { gittest.Write(t, r, "note.md", "desktop\n") })
	gittest.Write(t, env.Laptop, "note.md", "laptop\n")
	gittest.CommitAll(t, env.Laptop, "Update note.md · laptop")
	if err := env.Laptop.Fetch(context.Background()); err != nil {
		t.Fatal(err)
	}
	_ = env.Laptop.Merge("origin/main", false)
	h := newHarness(t, reopen(env.Laptop))
	h.s.repo = &failConflictedFiles{h.repo, -1}
	h.start()
	h.wantState(Conflict)
	if h.repo.addAlls.Load() != 0 {
		t.Fatalf("AddAll called during a merge")
	}
}

func TestQuitDuringConflictDoesNotCommit(t *testing.T) {
	env, h := conflictHarness(t)
	before := head(t, env.Laptop)
	remoteBefore := gittest.Git(t, env.Remote, "rev-parse", "main")
	addAlls := h.repo.addAlls.Load()
	gittest.Write(t, env.Laptop, "other.md", "x\n")
	h.host.reset()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := h.s.Quit(ctx, false); err != nil {
		t.Fatalf("Quit: %v", err)
	}
	if got := h.host.calls(); !slices.Equal(got, []string{"Flush"}) {
		t.Fatalf("calls during Quit = %v, want only Flush", got)
	}
	if head(t, env.Laptop) != before || h.repo.addAlls.Load() != addAlls {
		t.Fatalf("Quit committed or staged during a conflict")
	}
	if gittest.Git(t, env.Remote, "rev-parse", "main") != remoteBefore {
		t.Fatalf("Quit pushed during a conflict")
	}
	if !env.Laptop.MergeInProgress() {
		t.Fatalf("Quit ended the merge")
	}
}

func TestStartWithMergeInProgressEntersConflict(t *testing.T) {
	env := gittest.New(t)
	shareBase(t, env, func(r *gitsync.Repo) { gittest.Write(t, r, "note.md", "line\n") })
	deskPush(t, env, func(r *gitsync.Repo) { gittest.Write(t, r, "note.md", "desktop\n") })
	gittest.Write(t, env.Laptop, "note.md", "laptop\n")
	gittest.CommitAll(t, env.Laptop, "Update note.md · laptop")
	if err := env.Laptop.Fetch(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := env.Laptop.Merge("origin/main", false); !errors.Is(err, gitsync.ErrConflict) {
		t.Fatalf("merge = %v, want conflict", err)
	}
	// The app crashed here; a new syncer starts on the same repo.
	h := newHarness(t, reopen(env.Laptop))
	h.start()

	st := h.s.Status()
	if st.State != Conflict || st.Conflicts != 1 {
		t.Fatalf("Status() = %+v, want Conflict", st)
	}
	updates := h.rec.all()
	if len(updates) != 1 || !slices.Equal(updates[0].Conflicted, []string{"note.md"}) {
		t.Fatalf("updates = %+v, want one with Conflicted [note.md]", updates)
	}
	if h.repo.fetches.Load() != 0 || h.repo.addAlls.Load() != 0 || h.host.count("Flush") != 0 {
		t.Fatalf("Start in Conflict fetched, staged or flushed")
	}
	if got := h.clock.Pending(); len(got) != 0 {
		t.Fatalf("timers pending in Conflict: %v", got)
	}
}

func TestStartWithResolvedMergeCommitsIt(t *testing.T) {
	env := gittest.New(t)
	shareBase(t, env, func(r *gitsync.Repo) { gittest.Write(t, r, "note.md", "line\n") })
	deskPush(t, env, func(r *gitsync.Repo) { gittest.Write(t, r, "note.md", "desktop\n") })
	gittest.Write(t, env.Laptop, "note.md", "laptop\n")
	gittest.CommitAll(t, env.Laptop, "Update note.md · laptop")
	if err := env.Laptop.Fetch(context.Background()); err != nil {
		t.Fatal(err)
	}
	_ = env.Laptop.Merge("origin/main", false)
	// Every file was resolved but the app died before committing the merge.
	gittest.Write(t, env.Laptop, "note.md", "resolved\n")
	if err := env.Laptop.Add("note.md"); err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, reopen(env.Laptop))
	h.start()
	h.wantState(Synced)
	if env.Laptop.MergeInProgress() {
		t.Fatalf("merge still in progress")
	}
	if got := remoteSubject(t, env); got != "Merge · laptop" {
		t.Fatalf("remote subject = %q, want the merge commit", got)
	}
}

func TestConflictResolvedWhileExternalEditing(t *testing.T) {
	env, h := conflictHarness(t)
	gittest.Write(t, env.Laptop, "note.md", "resolved\n")
	if err := env.Laptop.Add("note.md"); err != nil {
		t.Fatal(err)
	}
	if err := env.Laptop.CommitMerge("Merge · laptop"); err != nil {
		t.Fatal(err)
	}
	h.host.editing.Store(true)
	h.s.ConflictResolved()
	h.s.waitIdle()
	// The cycle is deferred, but the UI must not keep showing the conflict.
	h.wantState(Idle)
	h.host.editing.Store(false)
	h.s.ExternalEditDone()
	h.s.waitIdle()
	h.wantState(Synced)
	if got := remoteSubject(t, env); got != "Merge · laptop" {
		t.Fatalf("remote subject = %q", got)
	}
}

func TestQuitAfterWorkerStoppedReturns(t *testing.T) {
	env := gittest.New(t)
	h := newHarness(t, env.Laptop)
	ctx, cancel := context.WithCancel(context.Background())
	h.s.Start(ctx)
	h.s.waitIdle()
	cancel()
	h.s.qmu.Lock()
	for !h.s.stopped {
		h.s.qcond.Wait()
	}
	h.s.qmu.Unlock()
	if err := h.s.Quit(context.Background(), true); err == nil {
		t.Fatalf("Quit on a stopped syncer returned nil")
	}
	if err := <-h.s.RunSetup(func(*gitsync.Repo) error { return nil }); err == nil {
		t.Fatalf("RunSetup on a stopped syncer returned nil")
	}
}

func TestConflictResolvedPushesAndResumes(t *testing.T) {
	env, h := conflictHarness(t)

	// Still conflicted: ConflictResolved keeps the Conflict state.
	h.s.ConflictResolved()
	h.s.waitIdle()
	h.wantState(Conflict)

	// The resolver writes, stages and commits the merge.
	gittest.Write(t, env.Laptop, "note.md", "laptop\ndesktop\n")
	if err := env.Laptop.Add("note.md"); err != nil {
		t.Fatal(err)
	}
	if err := env.Laptop.CommitMerge("Merge · laptop"); err != nil {
		t.Fatal(err)
	}
	h.s.ConflictResolved()
	h.s.waitIdle()

	h.wantState(Synced)
	if got := remoteSubject(t, env); got != "Merge · laptop" {
		t.Fatalf("remote subject = %q, want the merge commit", got)
	}
	if got, want := h.clock.Pending(), []time.Duration{5 * time.Minute}; !slices.Equal(got, want) {
		t.Fatalf("pending timers = %v, want the fetch timer resumed", got)
	}
	gittest.Write(t, env.Laptop, "after.md", "after\n")
	h.s.NoteChanged("after.md")
	h.advance(5 * time.Second)
	if !remoteHas(t, env, "after.md") {
		t.Fatalf("saves are not synced after the conflict was resolved")
	}
}
