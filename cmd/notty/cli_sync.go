package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/mathieucroset/notty/internal/config"
	"github.com/mathieucroset/notty/internal/gitsync"
	"github.com/mathieucroset/notty/internal/syncer"
	"github.com/mathieucroset/notty/internal/vault"
)

// Exit codes of notty sync (plan Task 38).
const (
	exitSynced   = 0
	exitError    = 1
	exitConflict = 2
)

// syncTimeout bounds the whole headless cycle; quitTimeout bounds the final
// commit (and push, if anything is left) once the cycle has ended.
var (
	syncTimeout = 2 * time.Minute
	quitTimeout = 10 * time.Second
)

const conflictMessage = "⚠ merge conflict — open notty to resolve"

// headlessHost is the syncer Host of notty sync: there is no open buffer,
// editor, or watcher, so every hook is a no-op.
type headlessHost struct{}

var _ syncer.Host = headlessHost{}

func (headlessHost) Flush() error                    { return nil }
func (headlessHost) LockMutations()                  {}
func (headlessHost) UnlockMutations(map[string]bool) {}
func (headlessHost) PauseWatcher()                   {}
func (headlessHost) ResumeWatcher()                  {}
func (headlessHost) ExternalEditing() bool           { return false }

// runSync is `notty sync` (spec §11): one headless full cycle under the
// vault lock. It exits 0 when synced (or committed locally without a
// remote), 1 on error, and 2 on conflict.
func runSync(args []string, vaultFlag string, e env) int {
	flags, vaultPath := subcommandFlags("sync", vaultFlag, e)
	pos, err := parseInterspersed(flags, args)
	if err != nil {
		return flagExit(err)
	}
	if len(pos) > 0 {
		_, _ = fmt.Fprintf(e.stderr, "notty sync: unexpected argument %q\n%s", pos[0], usage)
		return 2
	}
	fail := func(format string, a ...any) int {
		_, _ = fmt.Fprintf(e.stderr, "notty: "+format+"\n", a...)
		return exitError
	}

	if _, err := e.lookPath("git"); err != nil {
		return fail("git is not installed; notty sync needs it")
	}
	root, err := vaultRoot(*vaultPath, e)
	if err != nil {
		return fail("%v", err)
	}
	if !isDir(root) {
		return fail("no vault at %s: %v", root, errNeedsSetup)
	}
	repo := gitsync.Open(root)
	if !repo.IsRepo() {
		return fail("%s is not a git repository: run notty to set up sync", root)
	}
	cfg, err := config.Load(e.configPath, root)
	if err != nil {
		return fail("%v", err)
	}

	// Spec §9: the same lock as the TUI, for the whole run, without waiting.
	lock, err := vault.AcquireLock(root, 0)
	if err != nil {
		var held vault.ErrLocked
		if errors.As(err, &held) {
			who := "vault is open in Notty"
			if held.Pid > 0 {
				who = fmt.Sprintf("vault is open in Notty (pid %d)", held.Pid)
			}
			return fail("%s — it syncs automatically", who)
		}
		return fail("%v", err)
	}
	defer func() { _ = lock.Release() }()

	// Spec §7: while in Conflict, notty sync exits nonzero without committing.
	if repo.MergeInProgress() {
		_, _ = fmt.Fprintln(e.stderr, conflictMessage)
		return exitConflict
	}
	// The lock file must never be committed.
	if _, err := vault.EnsureGitignore(root); err != nil {
		return fail("%v", err)
	}
	return syncOnce(repo, cfg, e.stdout, e.stderr)
}

// syncOnce runs the syncer's startup logic (a full cycle, or LocalOnly
// without a remote or with sync disabled), waits for its outcome, then
// quits it and reports the outcome.
func syncOnce(repo *gitsync.Repo, cfg config.Config, stdout, stderr io.Writer) int {
	ctx, cancel := context.WithTimeout(context.Background(), syncTimeout)
	defer cancel()
	s := syncer.New(repo, cfg, headlessHost{}, syncer.RealClock())
	s.Start(ctx)
	st, ok := awaitOutcome(ctx, s, stderr)

	// quit finishes the syncer: Quit commits anything left (the only commit
	// in LocalOnly) and pushes if needed; in Conflict it commits nothing.
	quit := func() error {
		qctx, qcancel := context.WithTimeout(context.Background(), quitTimeout)
		defer qcancel()
		return s.Quit(qctx, true)
	}
	// stop finishes the syncer without another commit or push attempt.
	stop := func() {
		qctx, qcancel := context.WithCancel(context.Background())
		qcancel()
		_ = s.Quit(qctx, true)
	}

	if !ok {
		stop()
		_, _ = fmt.Fprintf(stderr, "notty: sync timed out after %v\n", syncTimeout)
		return exitError
	}
	switch st.State {
	case syncer.Synced, syncer.LocalOnly:
		if err := quit(); err != nil {
			_, _ = fmt.Fprintf(stderr, "notty: sync failed: %v\n", err)
			return exitError
		}
		if st.State == syncer.Synced {
			_, _ = fmt.Fprintln(stdout, "✓ synced")
		} else if !cfg.Sync.Enabled {
			_, _ = fmt.Fprintln(stdout, "✓ committed locally (sync is disabled)")
		} else {
			_, _ = fmt.Fprintln(stdout, "✓ committed locally (no remote)")
		}
		return exitSynced
	case syncer.Conflict:
		_ = quit()
		_, _ = fmt.Fprintln(stderr, conflictMessage)
		return exitConflict
	case syncer.Offline:
		stop()
		_, _ = fmt.Fprintf(stderr, "notty: offline: the remote is unreachable (%d local commit(s) not pushed)\n", st.Pending)
		return exitError
	default: // Error
		stop()
		_, _ = fmt.Fprintf(stderr, "notty: sync failed: %v\n", st.Err)
		if st.Detail == "auth" {
			_, _ = fmt.Fprintln(stderr, "notty: check your credentials with `ssh -T git@github.com` or `gh auth status`")
		}
		return exitError
	}
}

// awaitOutcome reads the syncer's updates until it reaches a state that
// ends a headless run, printing trash warnings on the way. It returns false
// if ctx ends first.
func awaitOutcome(ctx context.Context, s *syncer.Syncer, stderr io.Writer) (syncer.Status, bool) {
	for {
		select {
		case u := <-s.Updates():
			for _, w := range u.TrashWarnings {
				_, _ = fmt.Fprintf(stderr, "⚠ %s was deleted on %s, but you edited it here: restore it from the trash in notty\n", w.Path, w.Host)
			}
			switch u.Status.State {
			case syncer.Synced, syncer.LocalOnly, syncer.Conflict, syncer.Offline, syncer.Error:
				return u.Status, true
			}
		case <-ctx.Done():
			return syncer.Status{}, false
		}
	}
}
