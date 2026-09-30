package main

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mathieucroset/notty/internal/gitsync"
	"github.com/mathieucroset/notty/internal/gitsync/gittest"
	"github.com/mathieucroset/notty/internal/vault"
)

// syncFixture is a fixture whose vault is the gittest laptop clone.
func syncFixture(t *testing.T) (*fixture, *gittest.Env) {
	t.Helper()
	g := gittest.New(t)
	f := newFixture(t)
	f.vaultDir = g.Laptop.Dir
	f.writeConfig(t, "vault = \""+filepath.ToSlash(f.vaultDir)+"\"\n")
	return f, g
}

func remoteSubject(t *testing.T, g *gittest.Env) string {
	t.Helper()
	return gittest.Git(t, "", "--git-dir", g.Remote, "log", "-1", "--format=%s", "main")
}

func assertUnlocked(t *testing.T, root string) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(root, ".notty", "lock")); !os.IsNotExist(err) {
		t.Errorf("vault lock not released: %v", err)
	}
}

func TestSyncPushesLocalChange(t *testing.T) {
	f, g := syncFixture(t)
	gittest.Write(t, g.Laptop, "Ideas.md", "# Ideas\n")
	if code := run([]string{"sync"}, f.env()); code != 0 {
		t.Fatalf("exit code %d, stderr %q", code, f.stderr.String())
	}
	if !strings.Contains(f.stdout.String(), "synced") {
		t.Errorf("stdout = %q, want synced", f.stdout.String())
	}
	if got := remoteSubject(t, g); !strings.Contains(got, "Ideas.md") {
		t.Errorf("remote head subject = %q, want the commit of Ideas.md", got)
	}
	// The lock the run held is gitignored, never committed.
	if files := gittest.Git(t, g.Laptop.Dir, "ls-files"); strings.Contains(files, ".notty/lock") {
		t.Errorf("lock file committed: %q", files)
	}
	assertUnlocked(t, f.vaultDir)
}

func TestSyncPullsRemoteChange(t *testing.T) {
	f, g := syncFixture(t)
	gittest.Write(t, g.Desktop, "Plan.md", "# Plan\n")
	gittest.CommitAll(t, g.Desktop, "Create Plan.md · desktop")
	gittest.Push(t, g.Desktop)
	if code := run([]string{"--vault", f.vaultDir, "sync"}, f.env()); code != 0 {
		t.Fatalf("exit code %d, stderr %q", code, f.stderr.String())
	}
	if got := gittest.Read(t, g.Laptop, "Plan.md"); got != "# Plan\n" {
		t.Errorf("Plan.md = %q", got)
	}
}

// conflictingChange pushes a README change from the desktop and makes a
// conflicting, uncommitted one on the laptop.
func conflictingChange(t *testing.T, g *gittest.Env) {
	t.Helper()
	gittest.Write(t, g.Desktop, "README.md", "# Desktop notes\n")
	gittest.CommitAll(t, g.Desktop, "Update README.md · desktop")
	gittest.Push(t, g.Desktop)
	gittest.Write(t, g.Laptop, "README.md", "# Laptop notes\n")
}

func TestSyncConflictExits2(t *testing.T) {
	f, g := syncFixture(t)
	conflictingChange(t, g)
	if code := run([]string{"sync"}, f.env()); code != 2 {
		t.Fatalf("exit code %d, want 2; stderr %q", code, f.stderr.String())
	}
	if !strings.Contains(f.stderr.String(), "merge conflict") {
		t.Errorf("stderr = %q, want a merge conflict message", f.stderr.String())
	}
	if !g.Laptop.MergeInProgress() {
		t.Error("merge not left in progress for the TUI's resolver")
	}
	assertUnlocked(t, f.vaultDir)
}

// startConflictedMerge leaves the laptop in a conflicted merge of README.md.
func startConflictedMerge(t *testing.T, g *gittest.Env) {
	t.Helper()
	conflictingChange(t, g)
	gittest.CommitAll(t, g.Laptop, "Update README.md · laptop")
	if err := g.Laptop.Fetch(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := g.Laptop.Merge("origin/main", false); !errors.Is(err, gitsync.ErrConflict) {
		t.Fatalf("merge: %v, want a conflict", err)
	}
}

// A merge found in progress at start is handled by the syncer (spec §7):
// with conflicts left notty sync exits 2 without committing; with every
// file resolved the merge is committed and the sync goes on.
func TestSyncMergeInProgress(t *testing.T) {
	t.Run("unresolved", func(t *testing.T) {
		f, g := syncFixture(t)
		startConflictedMerge(t, g)
		gittest.Write(t, g.Laptop, "Extra.md", "# Extra\n")
		head := gittest.Git(t, g.Laptop.Dir, "rev-parse", "HEAD")

		if code := run([]string{"sync"}, f.env()); code != 2 {
			t.Fatalf("exit code %d, want 2; stderr %q", code, f.stderr.String())
		}
		if got := gittest.Git(t, g.Laptop.Dir, "rev-parse", "HEAD"); got != head {
			t.Errorf("HEAD moved from %s to %s", head, got)
		}
		if !g.Laptop.MergeInProgress() {
			t.Error("merge no longer in progress")
		}
		if !strings.Contains(f.stderr.String(), "merge conflict") {
			t.Errorf("stderr = %q", f.stderr.String())
		}
		assertUnlocked(t, f.vaultDir)
	})
	t.Run("resolved but uncommitted", func(t *testing.T) {
		f, g := syncFixture(t)
		startConflictedMerge(t, g)
		gittest.Write(t, g.Laptop, "README.md", "# Both\n")
		gittest.Git(t, g.Laptop.Dir, "add", "README.md")

		if code := run([]string{"sync"}, f.env()); code != 0 {
			t.Fatalf("exit code %d, want 0; stderr %q", code, f.stderr.String())
		}
		if g.Laptop.MergeInProgress() {
			t.Error("merge still in progress")
		}
		// The merge commit (two parents) reached the remote.
		parents := gittest.Git(t, "", "--git-dir", g.Remote, "log", "--merges", "-1", "--format=%P", "main")
		if len(strings.Fields(parents)) != 2 {
			t.Errorf("no merge commit on the remote (parents %q)", parents)
		}
		if got := gittest.Git(t, "", "--git-dir", g.Remote, "show", "main:README.md"); got != "# Both" {
			t.Errorf("remote README.md = %q", got)
		}
	})
}

func TestSyncWhileLocked(t *testing.T) {
	f, g := syncFixture(t)
	lock, err := vault.AcquireLock(f.vaultDir, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lock.Release() }()
	gittest.Write(t, g.Laptop, "Ideas.md", "# Ideas\n")
	before := remoteSubject(t, g)

	if code := run([]string{"sync"}, f.env()); code != 1 {
		t.Fatalf("exit code %d, want 1", code)
	}
	want := "vault is open in Notty (pid " + strconv.Itoa(os.Getpid()) + ") — it syncs automatically"
	if !strings.Contains(f.stderr.String(), want) {
		t.Errorf("stderr = %q, want %q", f.stderr.String(), want)
	}
	if got := remoteSubject(t, g); got != before {
		t.Errorf("remote changed to %q while locked", got)
	}
	if _, err := os.Stat(filepath.Join(f.vaultDir, ".notty", "lock")); err != nil {
		t.Errorf("the holder's lock was removed: %v", err)
	}
}

func TestSyncLocalOnlyCommits(t *testing.T) {
	t.Run("no remote", func(t *testing.T) {
		gittest.Isolate(t)
		f := newFixture(t)
		repo, err := gitsync.Init(f.vaultDir, "main")
		if err != nil {
			t.Fatal(err)
		}
		gittest.SetUser(t, repo, "Laptop User", "laptop@example.com")
		gittest.Write(t, repo, "Ideas.md", "# Ideas\n")
		f.writeConfig(t, "")
		if code := run([]string{"--vault", f.vaultDir, "sync"}, f.env()); code != 0 {
			t.Fatalf("exit code %d, stderr %q", code, f.stderr.String())
		}
		if got := gittest.Git(t, f.vaultDir, "log", "-1", "--format=%s"); !strings.Contains(got, "Ideas.md") {
			t.Errorf("head subject = %q, want the commit of Ideas.md", got)
		}
		if !strings.Contains(f.stdout.String(), "committed") {
			t.Errorf("stdout = %q", f.stdout.String())
		}
		assertUnlocked(t, f.vaultDir)
	})
	t.Run("sync disabled", func(t *testing.T) {
		f, g := syncFixture(t)
		f.writeConfig(t, "vault = \""+filepath.ToSlash(f.vaultDir)+"\"\n[sync]\nenabled = false\n")
		before := remoteSubject(t, g)
		gittest.Write(t, g.Laptop, "Ideas.md", "# Ideas\n")
		if code := run([]string{"sync"}, f.env()); code != 0 {
			t.Fatalf("exit code %d, stderr %q", code, f.stderr.String())
		}
		if got := gittest.Git(t, f.vaultDir, "log", "-1", "--format=%s"); !strings.Contains(got, "Ideas.md") {
			t.Errorf("head subject = %q, want the commit of Ideas.md", got)
		}
		if got := remoteSubject(t, g); got != before {
			t.Errorf("remote changed to %q with sync disabled", got)
		}
	})
}

func TestSyncOffline(t *testing.T) {
	f, g := syncFixture(t)
	gittest.Write(t, g.Laptop, "Ideas.md", "# Ideas\n")
	g.MakeRemoteUnreachable(t)
	if code := run([]string{"sync"}, f.env()); code != 1 {
		t.Fatalf("exit code %d, want 1; stderr %q", code, f.stderr.String())
	}
	if !strings.Contains(f.stderr.String(), "offline") {
		t.Errorf("stderr = %q, want offline", f.stderr.String())
	}
	assertUnlocked(t, f.vaultDir)
}

func TestSyncRefuses(t *testing.T) {
	t.Run("git missing", func(t *testing.T) {
		f, _ := syncFixture(t)
		f.gitFound = false
		if code := run([]string{"sync"}, f.env()); code != 1 {
			t.Errorf("exit code %d, want 1", code)
		}
		if !strings.Contains(f.stderr.String(), "git") {
			t.Errorf("stderr = %q", f.stderr.String())
		}
	})
	t.Run("not a repo", func(t *testing.T) {
		gittest.Isolate(t)
		f := newFixture(t)
		if err := os.MkdirAll(f.vaultDir, 0o755); err != nil {
			t.Fatal(err)
		}
		f.writeConfig(t, "")
		if code := run([]string{"--vault", f.vaultDir, "sync"}, f.env()); code != 1 {
			t.Errorf("exit code %d, want 1", code)
		}
		if !strings.Contains(f.stderr.String(), "not a git repository") {
			t.Errorf("stderr = %q", f.stderr.String())
		}
	})
	t.Run("stray argument", func(t *testing.T) {
		f, _ := syncFixture(t)
		if code := run([]string{"sync", "now"}, f.env()); code != 2 {
			t.Errorf("exit code %d, want 2", code)
		}
	})
}

// readLog returns the fixture's log file, written by run's logging.
func readLog(t *testing.T, f *fixture) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(f.dir, "state", "notty.log"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return string(b)
}

// assertSilent fails unless run wrote nothing to stdout and stderr.
func assertSilent(t *testing.T, f *fixture) {
	t.Helper()
	if f.stdout.Len() != 0 || f.stderr.Len() != 0 {
		t.Errorf("stdout %q, stderr %q; want both empty", f.stdout.String(), f.stderr.String())
	}
}

// notty sync --quiet (notty -q's background child) writes nothing to stdout
// or stderr: its messages go to the log, with the usual exit codes.
func TestSyncQuiet(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(t *testing.T, f *fixture, g *gittest.Env)
		wantCode int
		wantLog  []string // substrings of the log
	}{
		{"synced", func(t *testing.T, _ *fixture, g *gittest.Env) {
			gittest.Write(t, g.Laptop, "Ideas.md", "# Ideas\n")
		}, 0, nil},
		{"offline", func(t *testing.T, _ *fixture, g *gittest.Env) {
			gittest.Write(t, g.Laptop, "Ideas.md", "# Ideas\n")
			g.MakeRemoteUnreachable(t)
		}, 1, []string{"level=ERROR", "offline"}},
		{"conflict", func(t *testing.T, _ *fixture, g *gittest.Env) {
			conflictingChange(t, g)
		}, 2, []string{"level=WARN", "merge conflict"}},
		{"refused", func(_ *testing.T, f *fixture, _ *gittest.Env) {
			f.gitFound = false
		}, 1, []string{"level=ERROR", "git is not installed"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, g := syncFixture(t)
			tt.setup(t, f, g)
			if code := run([]string{"sync", "--quiet"}, f.env()); code != tt.wantCode {
				t.Fatalf("exit code %d, want %d; log %q", code, tt.wantCode, readLog(t, f))
			}
			assertSilent(t, f)
			log := readLog(t, f)
			for _, want := range tt.wantLog {
				if !strings.Contains(log, want) {
					t.Errorf("log = %q, want %q in it", log, want)
				}
			}
			if tt.wantCode == 0 {
				if got := remoteSubject(t, g); !strings.Contains(got, "Ideas.md") {
					t.Errorf("remote head subject = %q, want the commit of Ideas.md", got)
				}
			}
		})
	}
}

// --wait waits for a lock held by another process.
func TestSyncWaitsForLock(t *testing.T) {
	f, g := syncFixture(t)
	lock, err := vault.AcquireLock(f.vaultDir, 0)
	if err != nil {
		t.Fatal(err)
	}
	released := make(chan struct{})
	go func() {
		defer close(released)
		time.Sleep(300 * time.Millisecond)
		_ = lock.Release()
	}()
	gittest.Write(t, g.Laptop, "Ideas.md", "# Ideas\n")
	code := run([]string{"sync", "--wait", "10s"}, f.env())
	<-released
	if code != 0 {
		t.Fatalf("exit code %d, stderr %q", code, f.stderr.String())
	}
	if got := remoteSubject(t, g); !strings.Contains(got, "Ideas.md") {
		t.Errorf("remote head subject = %q, want the commit of Ideas.md", got)
	}
	assertUnlocked(t, f.vaultDir)
}

// Running out of --wait is the normal outcome while the TUI is open: an
// error on stderr, but only an Info line in the log when quiet.
func TestSyncWaitTimeout(t *testing.T) {
	for _, quiet := range []bool{false, true} {
		t.Run("quiet="+strconv.FormatBool(quiet), func(t *testing.T) {
			f, _ := syncFixture(t)
			lock, err := vault.AcquireLock(f.vaultDir, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = lock.Release() }()
			args := []string{"sync", "--wait", "300ms"}
			if quiet {
				args = append(args, "--quiet")
			}
			start := time.Now()
			if code := run(args, f.env()); code != 1 {
				t.Fatalf("exit code %d, want 1", code)
			}
			if d := time.Since(start); d < 300*time.Millisecond {
				t.Errorf("gave up after %v, want at least the 300ms wait", d)
			}
			const want = "vault is open in Notty"
			if !quiet {
				if !strings.Contains(f.stderr.String(), want) {
					t.Errorf("stderr = %q, want %q", f.stderr.String(), want)
				}
				return
			}
			assertSilent(t, f)
			log := readLog(t, f)
			if !strings.Contains(log, want) || !strings.Contains(log, "level=INFO") || strings.Contains(log, "level=ERROR") {
				t.Errorf("log = %q, want %q at Info only", log, want)
			}
		})
	}
}

func TestSyncBadWait(t *testing.T) {
	f, _ := syncFixture(t)
	if code := run([]string{"sync", "--wait", "soon"}, f.env()); code != 2 {
		t.Errorf("exit code %d, want 2", code)
	}
}
