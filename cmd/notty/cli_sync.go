package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
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
	quiet := flags.Bool("quiet", false, "print nothing; messages go to the log")
	wait := flags.Duration("wait", 0, "how long to wait for a vault lock held by another process")
	pos, err := parseInterspersed(flags, args)
	if err != nil {
		return flagExit(err)
	}
	if len(pos) > 0 {
		_, _ = fmt.Fprintf(e.stderr, "notty sync: unexpected argument %q\n%s", pos[0], usage)
		return 2
	}
	// Quiet (notty -q's background child, amendment B2): nothing on
	// stdout, and every stderr message goes to the log instead.
	stdout, stderr := e.stdout, e.stderr
	if *quiet {
		stdout, stderr = io.Discard, logWriter{}
	}
	fail := func(format string, a ...any) int {
		msg := fmt.Sprintf(format, a...)
		if *quiet {
			slog.Error("sync: " + msg)
		} else {
			_, _ = fmt.Fprintf(stderr, "notty: %s\n", msg)
		}
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

	// Spec §9: the same lock as the TUI, for the whole run, waiting up to
	// --wait for another process to release it.
	lock, err := vault.AcquireLock(root, 0)
	var held vault.ErrLocked
	if errors.As(err, &held) && *wait > 0 {
		if *quiet {
			slog.Info("sync: waiting for vault lock", "wait", *wait)
		} else {
			_, _ = fmt.Fprintln(stderr, "notty: waiting for vault lock…")
		}
		lock, err = vault.AcquireLock(root, *wait)
	}
	if err != nil {
		if errors.As(err, &held) {
			who := "vault is open in Notty"
			if held.Pid > 0 {
				who = fmt.Sprintf("vault is open in Notty (pid %d)", held.Pid)
			}
			if *quiet {
				// The normal outcome while the TUI is open, which syncs
				// the change itself.
				slog.Info("sync: " + who + " — it syncs automatically")
				return exitError
			}
			return fail("%s — it syncs automatically", who)
		}
		return fail("%v", err)
	}
	// If this process dies before the deferred Release (a crash or a kill),
	// the lock is left behind; the next AcquireLock takes it over because
	// its pid is no longer alive (spec §9 stale locks).
	defer func() { _ = lock.Release() }()

	// The lock file must never be committed. A merge in progress is left
	// untouched (.gitignore could be one of its conflicted files).
	if !repo.MergeInProgress() {
		if _, err := vault.EnsureGitignore(root); err != nil {
			return fail("%v", err)
		}
	}
	// A merge found in progress is handled by the syncer's startup (spec
	// §7): with conflicts left it enters Conflict, and notty sync exits 2
	// without committing; with every file resolved it commits the merge
	// and the cycle goes on.
	return syncOnce(repo, cfg, stdout, stderr)
}

// logWriter is notty sync's stderr in quiet mode: it logs each line
// written, without the "notty: " prefix, at Warn for warnings (lines
// starting with "⚠") and at Error otherwise.
type logWriter struct{}

func (logWriter) Write(p []byte) (int, error) {
	for _, line := range strings.Split(string(p), "\n") {
		line = strings.TrimPrefix(strings.TrimSpace(line), "notty: ")
		switch {
		case line == "":
		case strings.HasPrefix(line, "⚠"):
			slog.Warn("sync: " + line)
		default:
			slog.Error("sync: " + line)
		}
	}
	return len(p), nil
}

// vaultRoot loads the local config and returns the absolute vault root:
// vaultFlag if set, else the configured vault.
func vaultRoot(vaultFlag string, e env) (string, error) {
	cfg, err := config.Load(e.configPath, "")
	if err != nil {
		return "", err
	}
	root := cfg.VaultPath()
	if vaultFlag != "" {
		root = config.ExpandHome(vaultFlag)
	}
	if abs, err := filepath.Abs(root); err == nil {
		root = abs
	}
	return root, nil
}

// isDir reports whether p is an existing directory.
func isDir(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
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
