package gitsync_test

import (
	"context"
	"errors"
	"runtime"
	"testing"
	"time"

	"github.com/mathieucroset/notty/internal/gitsync"
	"github.com/mathieucroset/notty/internal/gitsync/gittest"
)

// A hanging ssh (simulated with GIT_SSH_COMMAND="sleep 60; true") must be cut
// off by the context and classified as a network error. On Unix the whole
// process group is killed, so the call returns well before exec's WaitDelay
// (2s) would force the pipes closed.
func TestNetworkTimeoutKillsHangingSSH(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("needs a POSIX shell and sleep")
	}
	env := gittest.New(t)
	t.Setenv("GIT_SSH_COMMAND", "sleep 60; true")
	const url = "ssh://git@example.invalid/notes.git"

	check := func(t *testing.T, op func(ctx context.Context) error) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
		defer cancel()
		start := time.Now()
		err := op(ctx)
		elapsed := time.Since(start)
		if !errors.Is(err, gitsync.ErrNetwork) || !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("err = %v, want ErrNetwork wrapping DeadlineExceeded", err)
		}
		if elapsed > 1500*time.Millisecond {
			t.Fatalf("took %v; the ssh child was not killed with git", elapsed)
		}
	}
	t.Run("ls-remote", func(t *testing.T) {
		check(t, func(ctx context.Context) error {
			_, _, err := gitsync.LsRemote(ctx, url)
			return err
		})
	})
	t.Run("fetch", func(t *testing.T) {
		gittest.Git(t, env.Laptop.Dir, "remote", "set-url", "origin", url)
		check(t, env.Laptop.Fetch)
	})
}
