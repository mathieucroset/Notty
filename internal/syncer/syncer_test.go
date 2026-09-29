package syncer

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/mathieucroset/notty/internal/config"
	"github.com/mathieucroset/notty/internal/gitsync"
	"github.com/mathieucroset/notty/internal/gitsync/gittest"
)

// remoteSubject returns the subject of the newest commit on the remote's main.
func remoteSubject(t *testing.T, env *gittest.Env) string {
	t.Helper()
	return gittest.Git(t, env.Remote, "log", "-1", "--format=%s", "main")
}

func remoteHas(t *testing.T, env *gittest.Env, path string) bool {
	t.Helper()
	out := gittest.Git(t, env.Remote, "ls-tree", "-r", "--name-only", "main")
	return slices.Contains(strings.Split(out, "\n"), path)
}

// deskPush commits and pushes a change made on the desktop.
func deskPush(t *testing.T, env *gittest.Env, change func(r *gitsync.Repo)) {
	t.Helper()
	change(env.Desktop)
	gittest.CommitAll(t, env.Desktop, "desktop change · desktop")
	gittest.Push(t, env.Desktop)
}

func TestStartWithRemoteRunsFullCycle(t *testing.T) {
	env := gittest.New(t)
	h := newHarness(t, env.Laptop)
	if got := h.s.Status().State; got != Idle {
		t.Fatalf("state before Start = %v, want Idle", got)
	}
	h.start()
	h.wantState(Synced)
	if got := h.repo.fetches.Load(); got != 1 {
		t.Fatalf("fetches = %d, want 1", got)
	}
	want := []State{Committing, Pulling, Synced}
	if got := h.rec.states(); !slices.Equal(got, want) {
		t.Fatalf("states = %v, want %v", got, want)
	}
}

func TestSaveCommitsAfterDelayThenPushes(t *testing.T) {
	env := gittest.New(t)
	h := newHarness(t, env.Laptop)
	h.start()

	gittest.Write(t, env.Laptop, "Standup notes.md", "# Standup\n")
	h.s.NoteChanged("Standup notes.md")
	h.advance(4 * time.Second)
	if remoteHas(t, env, "Standup notes.md") || h.repo.pushes.Load() != 0 {
		t.Fatalf("pushed before the commit delay elapsed")
	}
	// Another save restarts the timer.
	h.s.NoteChanged("Standup notes.md")
	h.advance(4 * time.Second)
	if h.repo.pushes.Load() != 0 {
		t.Fatalf("commit timer was not restarted by the second save")
	}
	h.advance(1 * time.Second)

	if !remoteHas(t, env, "Standup notes.md") {
		t.Fatalf("remote does not have the note after the commit delay")
	}
	if got, want := remoteSubject(t, env), "Create Standup notes.md · laptop"; got != want {
		t.Fatalf("remote subject = %q, want %q", got, want)
	}
	h.wantState(Synced)
	if got := h.s.Status().Pending; got != 0 {
		t.Fatalf("Pending = %d, want 0", got)
	}
}

func TestCommitDelayFromConfig(t *testing.T) {
	env := gittest.New(t)
	h := newHarness(t, env.Laptop, func(c *config.Config) { c.Sync.CommitDelayS = 2 })
	h.start()
	gittest.Write(t, env.Laptop, "a.md", "a\n")
	h.s.NoteChanged("a.md")
	h.advance(2 * time.Second)
	if !remoteHas(t, env, "a.md") {
		t.Fatalf("commit_delay_s = 2 not honored")
	}
}

func TestFetchTimerBringsDesktopChange(t *testing.T) {
	env := gittest.New(t)
	h := newHarness(t, env.Laptop)
	h.start()
	h.rec.reset()

	deskPush(t, env, func(r *gitsync.Repo) { gittest.Write(t, r, "Desk/idea.md", "# Idea\n") })
	h.advance(4 * time.Minute)
	if _, err := os.Stat(filepath.Join(env.Laptop.Dir, "Desk", "idea.md")); err == nil {
		t.Fatalf("desktop change arrived before the fetch interval")
	}
	h.advance(1 * time.Minute)

	if got := gittest.Read(t, env.Laptop, "Desk/idea.md"); got != "# Idea\n" {
		t.Fatalf("laptop Desk/idea.md = %q", got)
	}
	if got := h.rec.reindexed(); !slices.Equal(got, []string{"Desk/idea.md"}) {
		t.Fatalf("Reindex = %v, want [Desk/idea.md]", got)
	}
	if got := h.rec.reloaded(); !slices.Equal(got, []string{"Desk/idea.md"}) {
		t.Fatalf("Reload = %v, want [Desk/idea.md]", got)
	}
	h.wantState(Synced)

	// The fetch timer keeps running.
	deskPush(t, env, func(r *gitsync.Repo) { gittest.Write(t, r, "Desk/second.md", "2\n") })
	h.advance(5 * time.Minute)
	if got := gittest.Read(t, env.Laptop, "Desk/second.md"); got != "2\n" {
		t.Fatalf("second fetch tick did not merge: %q", got)
	}
}

func TestFetchTimerPushesWhenOnlyAhead(t *testing.T) {
	env := gittest.New(t)
	h := newHarness(t, env.Laptop)
	h.start()
	// A commit made outside the syncer (e.g. by `notty sync` crash leftovers).
	gittest.Write(t, env.Laptop, "local.md", "x\n")
	gittest.CommitAll(t, env.Laptop, "Create local.md · laptop")
	h.advance(5 * time.Minute)
	if !remoteHas(t, env, "local.md") {
		t.Fatalf("fetch tick did not push the local commit")
	}
	h.wantState(Synced)
}

func TestCleanMergeReindexIncludesRenames(t *testing.T) {
	env := gittest.New(t)
	h := newHarness(t, env.Laptop)
	h.start()
	h.rec.reset()

	deskPush(t, env, func(r *gitsync.Repo) {
		gittest.Write(t, r, "desk.md", "from desktop\n")
		gittest.Move(t, r, "README.md", "docs/README.md")
	})
	gittest.Write(t, env.Laptop, "lap.md", "from laptop\n")
	h.s.SyncNow()
	h.s.waitIdle()

	h.wantState(Synced)
	if got, want := h.rec.reindexed(), []string{"README.md", "desk.md", "docs/README.md"}; !slices.Equal(got, want) {
		t.Fatalf("Reindex = %v, want %v", got, want)
	}
	if got, want := h.rec.reloaded(), []string{"desk.md", "docs/README.md"}; !slices.Equal(got, want) {
		t.Fatalf("Reload = %v, want %v", got, want)
	}
	for _, p := range []string{"lap.md", "desk.md", "docs/README.md"} {
		if !remoteHas(t, env, p) {
			t.Errorf("remote misses %s after merge and push", p)
		}
	}
	if env.Laptop.MergeInProgress() {
		t.Fatalf("merge still in progress after a clean merge")
	}
}

func TestLockedMergeSectionOrder(t *testing.T) {
	env := gittest.New(t)
	h := newHarness(t, env.Laptop)
	h.start()
	deskPush(t, env, func(r *gitsync.Repo) { gittest.Write(t, r, "desk.md", "d\n") })
	h.host.reset()
	// The second flush (inside the locked section) saves a buffer that was
	// edited during the fetch.
	h.host.onFlush = func(n int) {
		if n == 2 {
			gittest.Write(t, env.Laptop, "late.md", "saved during fetch\n")
		}
	}
	h.host.mu.Lock()
	h.host.flushes = 0
	h.host.mu.Unlock()

	h.s.SyncNow()
	h.s.waitIdle()

	want := []string{"Flush", "Fetch", "Lock", "Pause", "Flush", "Commit", "Merge", "Resume", "Unlock", "Push"}
	if got := h.host.calls(); !slices.Equal(got, want) {
		t.Fatalf("calls = %v\nwant    %v", got, want)
	}
	if !remoteHas(t, env, "late.md") || !remoteHas(t, env, "desk.md") {
		t.Fatalf("remote misses late.md or desk.md")
	}
	if h.host.lockDepth != 0 {
		t.Fatalf("lock depth = %d after the cycle", h.host.lockDepth)
	}
	if len(h.host.unlocks) != 1 || len(h.host.unlocks[0]) != 0 {
		t.Fatalf("UnlockMutations got %v, want one empty set", h.host.unlocks)
	}
}

func TestMergeRefusedByLocalChangesRetriesOnce(t *testing.T) {
	env := gittest.New(t)
	h := newHarness(t, env.Laptop)
	h.start()
	deskPush(t, env, func(r *gitsync.Repo) { gittest.Write(t, r, "same.md", "identical\n") })
	// Another program creates the same file in the instant between the
	// commit and the merge: git refuses to overwrite the untracked file.
	h.repo.beforeMrg = func(n int) {
		if n == 1 {
			gittest.Write(t, env.Laptop, "same.md", "identical\n")
		}
	}
	h.s.SyncNow()
	h.s.waitIdle()

	if got := h.repo.merges.Load(); got != 2 {
		t.Fatalf("merges = %d, want 2 (refused, then retried)", got)
	}
	h.wantState(Synced)
	if env.Laptop.MergeInProgress() {
		t.Fatalf("merge in progress after retry")
	}
}

func TestMergeRefusedTwiceIsError(t *testing.T) {
	env := gittest.New(t)
	h := newHarness(t, env.Laptop)
	h.start()
	deskPush(t, env, func(r *gitsync.Repo) {
		gittest.Write(t, r, "same.md", "desktop\n")
		gittest.Write(t, r, "other.md", "desktop\n")
	})
	h.repo.beforeMrg = func(n int) {
		// Each time, a fresh untracked file collides with an incoming one.
		switch n {
		case 1:
			gittest.Write(t, env.Laptop, "same.md", "desktop\n")
		case 2:
			gittest.Write(t, env.Laptop, "other.md", "desktop\n")
		}
	}
	h.s.SyncNow()
	h.s.waitIdle()
	h.wantState(Error)
	if err := h.s.Status().Err; err == nil || !strings.Contains(err.Error(), "local changes") {
		t.Fatalf("Err = %v, want a local-changes error", err)
	}
	if h.host.lockDepth != 0 {
		t.Fatalf("mutations left locked after a failed merge")
	}
}

func TestPushRejectedAfterRemoteMovedRetriesOnce(t *testing.T) {
	env := gittest.New(t)
	h := newHarness(t, env.Laptop)
	h.start()
	// The desktop pushes between our fetch and our push.
	h.repo.beforePsh = func(n int) {
		if n == 1 {
			deskPush(t, env, func(r *gitsync.Repo) { gittest.Write(t, r, "race.md", "desktop\n") })
		}
	}
	gittest.Write(t, env.Laptop, "mine.md", "laptop\n")
	h.rec.reset()
	h.s.SyncNow()
	h.s.waitIdle()

	h.wantState(Synced)
	if got := h.repo.pushes.Load(); got != 2 {
		t.Fatalf("pushes = %d, want 2 (rejected, then retried after merging)", got)
	}
	if !remoteHas(t, env, "mine.md") || !remoteHas(t, env, "race.md") {
		t.Fatalf("remote misses mine.md or race.md")
	}
	if got := h.rec.reindexed(); !slices.Contains(got, "race.md") {
		t.Fatalf("Reindex = %v, want race.md", got)
	}
}

func TestLocalOnlyCommitsWithoutPush(t *testing.T) {
	gittest.Isolate(t)
	repo, err := gitsync.Init(filepath.Join(t.TempDir(), "vault"), "main")
	if err != nil {
		t.Fatal(err)
	}
	repo.Host = "laptop"
	gittest.SetUser(t, repo, "Laptop User", "laptop@example.com")
	h := newHarness(t, repo)
	h.start()
	h.wantState(LocalOnly)

	gittest.Write(t, repo, "first.md", "# First\n")
	h.s.NoteChanged("first.md")
	h.advance(5 * time.Second)

	if got := gittest.Git(t, repo.Dir, "log", "-1", "--format=%s"); got != "Create first.md · laptop" {
		t.Fatalf("last commit = %q", got)
	}
	h.wantState(LocalOnly)
	if h.repo.fetches.Load() != 0 || h.repo.pushes.Load() != 0 {
		t.Fatalf("LocalOnly fetched or pushed")
	}
	if got := h.clock.Pending(); len(got) != 0 {
		t.Fatalf("pending timers in LocalOnly = %v, want none (no fetch timer)", got)
	}
}

func TestSyncDisabledBehavesLikeLocalOnly(t *testing.T) {
	env := gittest.New(t)
	h := newHarness(t, env.Laptop, func(c *config.Config) { c.Sync.Enabled = false })
	h.start()
	h.wantState(LocalOnly)
	gittest.Write(t, env.Laptop, "x.md", "x\n")
	h.s.SyncNow()
	h.s.waitIdle()
	if got := gittest.Git(t, env.Laptop.Dir, "log", "-1", "--format=%s"); got != "Create x.md · laptop" {
		t.Fatalf("last commit = %q", got)
	}
	if remoteHas(t, env, "x.md") || h.repo.fetches.Load() != 0 {
		t.Fatalf("sync.enabled = false still fetched or pushed")
	}
	h.wantState(LocalOnly)
}

func TestFirstPushSetsUpstream(t *testing.T) {
	gittest.Isolate(t)
	remote := gittest.NewEmptyRemote(t)
	repo, err := gitsync.Init(filepath.Join(t.TempDir(), "vault"), "main")
	if err != nil {
		t.Fatal(err)
	}
	repo.Host = "laptop"
	gittest.SetUser(t, repo, "Laptop User", "laptop@example.com")
	if err := repo.RemoteAdd(remote); err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, repo)
	h.start() // unborn branch, nothing to commit: nothing to push either
	h.wantState(Synced)

	gittest.Write(t, repo, "a.md", "a\n")
	h.s.SyncNow()
	h.s.waitIdle()
	h.wantState(Synced)
	if !repo.HasUpstream() {
		t.Fatalf("first push did not set the upstream")
	}
	if got := gittest.Git(t, remote, "log", "-1", "--format=%s", "main"); got != "Create a.md · laptop" {
		t.Fatalf("remote subject = %q", got)
	}
}

func TestQuitFlushesCommitsAndPushes(t *testing.T) {
	for _, skip := range []bool{false, true} {
		t.Run(map[bool]string{false: "flush", true: "skipFlush"}[skip], func(t *testing.T) {
			env := gittest.New(t)
			h := newHarness(t, env.Laptop)
			h.start()
			h.host.reset()
			gittest.Write(t, env.Laptop, "q.md", "quit\n")
			h.s.NoteChanged("q.md")

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := h.s.Quit(ctx, skip); err != nil {
				t.Fatalf("Quit: %v", err)
			}
			if got := h.host.count("Flush"); got != map[bool]int{false: 1, true: 0}[skip] {
				t.Fatalf("Flush calls = %d with skipFlush=%v", got, skip)
			}
			if !remoteHas(t, env, "q.md") {
				t.Fatalf("Quit did not push")
			}
			// Timers are stopped and later requests are ignored.
			if got := h.clock.Pending(); len(got) != 0 {
				t.Fatalf("pending timers after Quit = %v", got)
			}
			h.s.SyncNow()
			h.s.waitIdle()
			if got := h.repo.fetches.Load(); got != 1 {
				t.Fatalf("fetches after Quit = %d, want only the startup fetch", got)
			}
		})
	}
}

func TestQuitNeverBlocksPastDeadline(t *testing.T) {
	env := gittest.New(t)
	h := newHarness(t, env.Laptop)
	h.start()
	// Occupy the worker with a job that blocks until released.
	release, started := make(chan struct{}), make(chan struct{})
	h.s.enqueue(job{fn: func() { close(started); <-release }})
	defer close(release)
	<-started

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := h.s.Quit(ctx, true)
	if err == nil {
		t.Fatalf("Quit returned nil while the worker was busy")
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("Quit blocked for %v", d)
	}
}

func TestUpdatesChannelDeliversInOrder(t *testing.T) {
	env := gittest.New(t)
	h := newHarness(t, env.Laptop)
	h.start()
	want := h.rec.states()
	var got []State
	timeout := time.After(5 * time.Second)
	for len(got) < len(want) {
		select {
		case u := <-h.s.Updates():
			got = append(got, u.Status.State)
		case <-timeout:
			t.Fatalf("Updates() delivered %v, want %v", got, want)
		}
	}
	if !slices.Equal(got, want) {
		t.Fatalf("Updates() = %v, want %v", got, want)
	}
}
