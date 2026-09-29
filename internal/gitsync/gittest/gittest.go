// Package gittest builds real git fixtures for tests: a bare "remote" and two
// clones ("laptop" and "desktop") with distinct identities. Every helper
// isolates git from the user's global and system configuration.
package gittest

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/mathieucroset/notty/internal/gitsync"
)

// Env is a bare remote with branch main plus two clones of it that start
// from one initial commit containing README.md.
type Env struct {
	Remote  string // path of the bare repository
	Laptop  *gitsync.Repo
	Desktop *gitsync.Repo
}

// Isolate points git at an empty HOME and a private global config (with
// init.defaultBranch=main and signing disabled) and disables the system
// config, for the rest of the test. It uses t.Setenv, so tests calling it
// cannot run in parallel.
func Isolate(t testing.TB) {
	t.Helper()
	if !gitsync.Available() {
		t.Skip("git not installed")
	}
	home := t.TempDir()
	cfg := filepath.Join(home, "gitconfig")
	const global = "[init]\n\tdefaultBranch = main\n[commit]\n\tgpgsign = false\n[tag]\n\tgpgsign = false\n"
	if err := os.WriteFile(cfg, []byte(global), 0o644); err != nil {
		t.Fatalf("gittest: write global config: %v", err)
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("GIT_CONFIG_GLOBAL", cfg)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(home))
	for _, k := range []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_OBJECT_DIRECTORY",
		"GIT_CONFIG_PARAMETERS", "GIT_CONFIG_COUNT", "GIT_TEMPLATE_DIR", "GIT_SSH_COMMAND", "GIT_SSH", "GIT_AUTHOR_NAME", "GIT_AUTHOR_EMAIL", "GIT_COMMITTER_NAME", "GIT_COMMITTER_EMAIL", "GIT_AUTHOR_DATE", "GIT_COMMITTER_DATE"} {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}
}

// New creates the remote and both clones. The laptop makes the initial
// commit and pushes it with an upstream; the desktop clones afterwards.
func New(t testing.TB) *Env {
	t.Helper()
	Isolate(t)
	root := t.TempDir()
	remote := filepath.Join(root, "remote.git")
	Git(t, "", "init", "-q", "--bare", "-b", "main", remote)

	ctx := context.Background()
	laptop, err := gitsync.Clone(ctx, remote, filepath.Join(root, "laptop"))
	if err != nil {
		t.Fatalf("gittest: clone laptop: %v", err)
	}
	laptop.Host = "laptop"
	SetUser(t, laptop, "Laptop User", "laptop@example.com")
	Write(t, laptop, "README.md", "# Notes\n")
	CommitAll(t, laptop, "Initial commit · laptop")
	if err := laptop.Push(ctx, true); err != nil {
		t.Fatalf("gittest: initial push: %v", err)
	}

	desktop, err := gitsync.Clone(ctx, remote, filepath.Join(root, "desktop"))
	if err != nil {
		t.Fatalf("gittest: clone desktop: %v", err)
	}
	desktop.Host = "desktop"
	SetUser(t, desktop, "Desktop User", "desktop@example.com")
	return &Env{Remote: remote, Laptop: laptop, Desktop: desktop}
}

// NewEmptyRemote creates a bare repository with no commits and returns its
// path.
func NewEmptyRemote(t testing.TB) string {
	t.Helper()
	Isolate(t)
	remote := filepath.Join(t.TempDir(), "empty.git")
	Git(t, "", "init", "-q", "--bare", "-b", "main", remote)
	return remote
}

// SetUser sets user.name and user.email in the repository's own config.
func SetUser(t testing.TB, repo *gitsync.Repo, name, email string) {
	t.Helper()
	Git(t, repo.Dir, "config", "user.name", name)
	Git(t, repo.Dir, "config", "user.email", email)
}

// Write writes content to path (relative to the repo), creating parent
// directories.
func Write(t testing.TB, repo *gitsync.Repo, path, content string) {
	t.Helper()
	full := filepath.Join(repo.Dir, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("gittest: mkdir for %s: %v", path, err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("gittest: write %s: %v", path, err)
	}
}

// Move renames a file inside the repo (a filesystem move, like Notty's trash),
// creating the destination's parent directories.
func Move(t testing.TB, repo *gitsync.Repo, from, to string) {
	t.Helper()
	dst := filepath.Join(repo.Dir, filepath.FromSlash(to))
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatalf("gittest: mkdir for %s: %v", to, err)
	}
	if err := os.Rename(filepath.Join(repo.Dir, filepath.FromSlash(from)), dst); err != nil {
		t.Fatalf("gittest: move %s -> %s: %v", from, to, err)
	}
}

// Read returns the content of path in the repo's working tree.
func Read(t testing.TB, repo *gitsync.Repo, path string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repo.Dir, filepath.FromSlash(path)))
	if err != nil {
		t.Fatalf("gittest: read %s: %v", path, err)
	}
	return string(b)
}

// CommitAll stages everything and commits it; it fails the test if there was
// nothing to commit.
func CommitAll(t testing.TB, repo *gitsync.Repo, msg string) {
	t.Helper()
	if err := repo.AddAll(); err != nil {
		t.Fatalf("gittest: add all: %v", err)
	}
	ok, err := repo.Commit(msg)
	if err != nil {
		t.Fatalf("gittest: commit %q: %v", msg, err)
	}
	if !ok {
		t.Fatalf("gittest: commit %q: nothing to commit", msg)
	}
}

// Push pushes the repo's current branch to origin.
func Push(t testing.TB, repo *gitsync.Repo) {
	t.Helper()
	if err := repo.Push(context.Background(), false); err != nil {
		t.Fatalf("gittest: push: %v", err)
	}
}

// Sync fetches origin and merges origin/<branch> into repo, failing on any
// error (including conflicts).
func Sync(t testing.TB, repo *gitsync.Repo) {
	t.Helper()
	if err := repo.Fetch(context.Background()); err != nil {
		t.Fatalf("gittest: fetch: %v", err)
	}
	branch, err := repo.CurrentBranch()
	if err != nil {
		t.Fatalf("gittest: current branch: %v", err)
	}
	if err := repo.Merge("origin/"+branch, false); err != nil {
		t.Fatalf("gittest: merge: %v", err)
	}
}

// MakeRemoteUnreachable renames the bare repository so that network
// operations fail as if the remote were offline. The rename is undone by
// RestoreRemote or at test cleanup.
func (e *Env) MakeRemoteUnreachable(t testing.TB) {
	t.Helper()
	hidden := e.Remote + ".offline"
	if err := os.Rename(e.Remote, hidden); err != nil {
		t.Fatalf("gittest: hide remote: %v", err)
	}
	t.Cleanup(func() {
		if _, err := os.Stat(hidden); err == nil {
			_ = os.Rename(hidden, e.Remote)
		}
	})
}

// RestoreRemote undoes MakeRemoteUnreachable.
func (e *Env) RestoreRemote(t testing.TB) {
	t.Helper()
	if err := os.Rename(e.Remote+".offline", e.Remote); err != nil {
		t.Fatalf("gittest: restore remote: %v", err)
	}
}

// Git runs a raw git command (in dir, or the current directory when dir is
// empty) for fixture setup and returns its trimmed stdout. It fails the test
// on error.
func Git(t testing.TB, dir string, args ...string) string {
	t.Helper()
	full := args
	if dir != "" {
		full = append([]string{"-C", dir}, args...)
	}
	cmd := exec.Command("git", full...)
	cmd.Env = append(os.Environ(), "LC_ALL=C", "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.Output()
	if err != nil {
		stderr := ""
		if ee, ok := err.(*exec.ExitError); ok {
			stderr = string(ee.Stderr)
		}
		t.Fatalf("gittest: git %v: %v\n%s", args, err, stderr)
	}
	return string(trimNewline(out))
}

func trimNewline(b []byte) []byte {
	for len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == '\r') {
		b = b[:len(b)-1]
	}
	return b
}
