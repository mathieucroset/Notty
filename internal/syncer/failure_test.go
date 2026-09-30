package syncer

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/mathieucroset/notty/internal/config"
	"github.com/mathieucroset/notty/internal/gitsync"
	"github.com/mathieucroset/notty/internal/gitsync/gittest"
)

func hourlyFetch(c *config.Config) { c.Sync.FetchIntervalM = 60 }

func TestOfflineBackoff(t *testing.T) {
	env := gittest.New(t)
	h := newHarness(t, env.Laptop, hourlyFetch)
	h.start()
	h.wantState(Synced)

	gittest.Write(t, env.Laptop, "offline.md", "written offline\n")
	env.MakeRemoteUnreachable(t)
	h.s.SyncNow()
	h.s.waitIdle()

	if got := h.s.Status(); got.State != Offline || got.Pending != 1 {
		t.Fatalf("Status() = %+v, want Offline with 1 pending", got)
	}
	fetches := h.repo.fetches.Load()
	for i, d := range []time.Duration{30 * time.Second, time.Minute, 2 * time.Minute, 4 * time.Minute, 5 * time.Minute, 5 * time.Minute} {
		want := []time.Duration{d, time.Hour}
		if got := h.clock.Pending(); !slices.Equal(got, want) {
			t.Fatalf("retry %d: pending timers = %v, want %v", i, got, want)
		}
		h.advance(d - time.Second)
		if h.repo.fetches.Load() != fetches {
			t.Fatalf("retry %d ran before %v elapsed", i, d)
		}
		h.advance(time.Second)
		if got := h.repo.fetches.Load(); got != fetches+1 {
			t.Fatalf("retry %d: fetches = %d, want %d", i, got, fetches+1)
		}
		fetches++
		h.wantState(Offline)
	}

	// A save while offline commits; the pending count grows and the backoff
	// schedule is not disturbed.
	gittest.Write(t, env.Laptop, "second.md", "2\n")
	h.s.NoteChanged("second.md")
	h.advance(5 * time.Second)
	if got := h.s.Status(); got.State != Offline || got.Pending != 2 {
		t.Fatalf("after offline save: Status() = %+v, want Offline with 2 pending", got)
	}
	if got, want := h.clock.Pending(), []time.Duration{5 * time.Minute, time.Hour}; !slices.Equal(got, want) {
		t.Fatalf("pending timers after offline save = %v, want %v", got, want)
	}

	// Back online: the next retry syncs and resets the backoff.
	env.RestoreRemote(t)
	h.advance(5*time.Minute - 5*time.Second)
	h.wantState(Synced)
	if !remoteHas(t, env, "offline.md") || !remoteHas(t, env, "second.md") {
		t.Fatalf("remote misses the offline commits")
	}
	if got, want := h.clock.Pending(), []time.Duration{time.Hour}; !slices.Equal(got, want) {
		t.Fatalf("pending timers after recovery = %v, want %v", got, want)
	}
	env.MakeRemoteUnreachable(t)
	h.s.SyncNow()
	h.s.waitIdle()
	h.wantState(Offline)
	if got, want := h.clock.Pending(), []time.Duration{30 * time.Second, time.Hour}; !slices.Equal(got, want) {
		t.Fatalf("backoff not reset after success: pending = %v, want %v", got, want)
	}
}

// fakeSSHAuthFailure makes ssh remotes fail like a rejected key.
func fakeSSHAuthFailure(t *testing.T, repo *gitsync.Repo) {
	t.Helper()
	script := filepath.Join(t.TempDir(), "ssh")
	body := "#!/bin/sh\necho 'git@example.invalid: Permission denied (publickey).' >&2\nexit 255\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	// git runs GIT_SSH_COMMAND through sh, even on Windows (Git for
	// Windows' sh), where the backslashes of a native path would be read
	// as escapes: pass the path quoted and with forward slashes.
	t.Setenv("GIT_SSH_COMMAND", "'"+filepath.ToSlash(script)+"'")
	gittest.Git(t, repo.Dir, "remote", "set-url", "origin", "ssh://git@example.invalid/notes.git")
}

func TestAuthErrorStopsAutomaticRetries(t *testing.T) {
	env := gittest.New(t)
	fakeSSHAuthFailure(t, env.Laptop)
	h := newHarness(t, env.Laptop)
	h.start()

	st := h.s.Status()
	if st.State != Error || st.Detail != "auth" || !errors.Is(st.Err, gitsync.ErrAuth) {
		t.Fatalf("Status() = %+v, want Error/auth wrapping ErrAuth", st)
	}
	if got, want := h.clock.Pending(), []time.Duration{5 * time.Minute}; !slices.Equal(got, want) {
		t.Fatalf("pending timers = %v, want only the fetch timer (no retry)", got)
	}
	fetches := h.repo.fetches.Load()
	h.advance(15 * time.Minute)
	if got := h.repo.fetches.Load(); got != fetches {
		t.Fatalf("fetch timer retried after an auth error: fetches %d -> %d", fetches, got)
	}
	h.wantState(Error)

	// The next save retries.
	gittest.Write(t, env.Laptop, "a.md", "a\n")
	h.s.NoteChanged("a.md")
	h.advance(5 * time.Second)
	if got := h.repo.fetches.Load(); got != fetches+1 {
		t.Fatalf("save did not retry: fetches = %d, want %d", got, fetches+1)
	}
	// So does a manual sync.
	h.s.SyncNow()
	h.s.waitIdle()
	if got := h.repo.fetches.Load(); got != fetches+2 {
		t.Fatalf("SyncNow did not retry: fetches = %d, want %d", got, fetches+2)
	}
	h.wantState(Error)
}
