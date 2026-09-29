package gitsync

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestClassify(t *testing.T) {
	const net, local = true, false
	tests := []struct {
		name    string
		args    []string
		network bool
		stdout  string
		stderr  string
		want    error
	}{
		{"resolve host", []string{"fetch"}, net, "", "fatal: unable to access 'https://github.com/x/y.git/': Could not resolve host: github.com", ErrNetwork},
		{"connection refused", []string{"fetch"}, net, "", "ssh: connect to host github.com port 22: Connection refused\nfatal: Could not read from remote repository.", ErrNetwork},
		{"unreachable", []string{"push"}, net, "", "ssh: connect to host github.com port 22: Network is unreachable", ErrNetwork},
		{"timed out", []string{"fetch"}, net, "", "ssh: connect to host github.com port 22: Operation timed out", ErrNetwork},
		{"could not read without auth", []string{"fetch"}, net, "", "fatal: Could not read from remote repository.\n\nPlease make sure you have the correct access rights\nand the repository exists.", ErrNetwork},
		{"missing local remote", []string{"fetch", "origin"}, net, "", "fatal: '/tmp/x/remote.git' does not appear to be a git repository\nfatal: Could not read from remote repository.", ErrNetwork},
		{"missing local clone source", []string{"clone"}, net, "", "fatal: repository '/tmp/x/remote.git' does not exist", ErrNetwork},
		{"unable to access", []string{"fetch"}, net, "", "fatal: unable to access 'https://example.com/': Failed to connect", ErrNetwork},
		{"publickey", []string{"fetch"}, net, "", "git@github.com: Permission denied (publickey).\nfatal: Could not read from remote repository.", ErrAuth},
		{"auth failed", []string{"push"}, net, "", "remote: Invalid username or password.\nfatal: Authentication failed for 'https://github.com/x/y.git/'", ErrAuth},
		{"403", []string{"push"}, net, "", "fatal: unable to access 'https://github.com/x/y.git/': The requested URL returned error: 403", ErrAuth},
		{"401", []string{"fetch"}, net, "", "fatal: unable to access 'https://example.com/x.git/': The requested URL returned error: 401", ErrAuth},
		{"terminal prompt", []string{"fetch"}, net, "", "fatal: could not read Username for 'https://github.com': terminal prompts disabled", ErrAuth},
		{"host key", []string{"fetch"}, net, "", "Host key verification failed.\nfatal: Could not read from remote repository.", ErrAuth},
		{"ssh repository not found", []string{"fetch"}, net, "", "ERROR: Repository not found.\nfatal: Could not read from remote repository.", ErrAuth},
		{"https repository not found", []string{"clone"}, net, "", "remote: Repository not found.\nfatal: repository 'https://github.com/x/private.git/' not found", ErrAuth},
		{"quoted repository not found", []string{"ls-remote"}, net, "", "fatal: repository 'https://example.com/x.git/' not found", ErrAuth},
		{"permission to denied", []string{"push"}, net, "", "remote: Permission to owner/repo.git denied to someone.\nfatal: unable to access 'https://github.com/owner/repo.git/': The requested URL returned error: 403", ErrAuth},
		{"access denied", []string{"fetch"}, net, "", "remote: Access denied\nfatal: unable to access 'https://gitlab.example.com/x.git/'", ErrAuth},
		{"merge conflict", []string{"merge", "--no-edit", "origin/main"}, local, "Auto-merging a.md\nCONFLICT (content): Merge conflict in a.md\nAutomatic merge failed; fix conflicts and then commit the result.", "", ErrConflict},
		{"merge conflict with -c options", []string{"-c", "core.hooksPath=/dev/null", "merge", "origin/main"}, local, "CONFLICT (content): Merge conflict in a.md", "", ErrConflict},
		{"conflict word outside merge", []string{"add", "CONFLICT (x).md"}, local, "", "fatal: pathspec 'CONFLICT (x).md' did not match any files", nil},
		{"local changes", []string{"merge", "origin/main"}, local, "", "error: Your local changes to the following files would be overwritten by merge:\n\ta.md\nPlease commit your changes or stash them before you merge.\nAborting", ErrLocalChanges},
		{"sha containing 403 is not auth", []string{"push"}, net, "", "error: failed to push some refs; object a403b1c missing", nil},
		{"local command never auth", []string{"add", "Access denied.md"}, local, "", "fatal: pathspec 'Access denied.md' did not match any files", nil},
		{"local command never network", []string{"show", ":2:x"}, local, "", "fatal: path 'timed out.md' does not exist", nil},
		{"local permission denied is not auth", []string{"commit"}, local, "", "error: open(\".git/index.lock\"): Permission denied (publickey)", nil},
		{"unclassified", []string{"commit"}, local, "", "fatal: something else", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classify(tt.args, tt.network, tt.stdout, tt.stderr)
			if got != tt.want {
				t.Fatalf("classify() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSubcommand(t *testing.T) {
	tests := []struct {
		args []string
		want string
	}{
		{[]string{"merge", "x"}, "merge"},
		{[]string{"-c", "a=b", "-c", "c=d", "commit", "-m", "x"}, "commit"},
		{nil, ""},
	}
	for _, tt := range tests {
		if got := subcommand(tt.args); got != tt.want {
			t.Errorf("subcommand(%q) = %q, want %q", tt.args, got, tt.want)
		}
	}
}

func TestGitErrorWrapsKindAndStderr(t *testing.T) {
	err := error(&GitError{Args: []string{"fetch", "origin"}, Stderr: "fatal: Could not resolve host: x", Kind: ErrNetwork, Err: errors.New("exit status 128")})
	if !errors.Is(err, ErrNetwork) {
		t.Fatalf("errors.Is(err, ErrNetwork) = false")
	}
	if errors.Is(err, ErrAuth) {
		t.Fatalf("errors.Is(err, ErrAuth) = true")
	}
	msg := err.Error()
	for _, want := range []string{"git fetch", "Could not resolve host"} {
		if !strings.Contains(msg, want) {
			t.Errorf("Error() = %q, missing %q", msg, want)
		}
	}
	var ge *GitError
	if !errors.As(fmt.Errorf("sync: %w", err), &ge) || ge.Kind != ErrNetwork {
		t.Fatalf("errors.As through wrapping failed")
	}
}

func TestGitEnv(t *testing.T) {
	t.Run("adds defaults", func(t *testing.T) {
		env := gitEnv([]string{"HOME=/h", "LC_ALL=fr_FR.UTF-8"})
		for _, want := range []string{"HOME=/h", "GIT_TERMINAL_PROMPT=0", "LC_ALL=C", "GIT_SSH_COMMAND=ssh -o BatchMode=yes"} {
			if !slices.Contains(env, want) {
				t.Errorf("env missing %q: %v", want, env)
			}
		}
		if slices.Contains(env, "LC_ALL=fr_FR.UTF-8") {
			t.Errorf("env kept caller LC_ALL: %v", env)
		}
	})
	t.Run("keeps user ssh command", func(t *testing.T) {
		env := gitEnv([]string{"GIT_SSH_COMMAND=ssh -i key"})
		if !slices.Contains(env, "GIT_SSH_COMMAND=ssh -i key") {
			t.Errorf("user GIT_SSH_COMMAND dropped: %v", env)
		}
		if slices.Contains(env, "GIT_SSH_COMMAND=ssh -o BatchMode=yes") {
			t.Errorf("user GIT_SSH_COMMAND overridden: %v", env)
		}
	})
}

func TestRunGitMissing(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if Available() {
		t.Fatalf("Available() = true with empty PATH")
	}
	_, err := execRunner(context.Background(), "", "version")
	if !errors.Is(err, ErrGitMissing) {
		t.Fatalf("err = %v, want ErrGitMissing", err)
	}
}

func TestRunTimeoutIsNetworkForNetworkOps(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	<-ctx.Done()
	err := wrapRunError([]string{"fetch"}, true, ctx, result{}, ctx.Err())
	if !errors.Is(err, ErrNetwork) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want ErrNetwork wrapping DeadlineExceeded", err)
	}
	err = wrapRunError([]string{"status"}, false, ctx, result{}, ctx.Err())
	if errors.Is(err, ErrNetwork) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("local err = %v, want only DeadlineExceeded", err)
	}
}

func TestOpenAndIsRepo(t *testing.T) {
	if !Available() {
		t.Skip("git not installed")
	}
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CEILING_DIRECTORIES", filepath.Dir(t.TempDir()))
	dir := t.TempDir()
	r := Open(dir)
	if r.Dir != dir {
		t.Errorf("Dir = %q, want %q", r.Dir, dir)
	}
	if r.Host == "" || strings.Contains(r.Host, ".") {
		t.Errorf("Host = %q, want non-empty short hostname", r.Host)
	}
	if r.IsRepo() {
		t.Fatalf("IsRepo() = true for empty dir")
	}
	if _, err := execRunner(context.Background(), dir, "init", "-q", "-b", "main"); err != nil {
		t.Fatalf("git init: %v", err)
	}
	if !r.IsRepo() {
		t.Fatalf("IsRepo() = false after git init")
	}
	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if Open(sub).IsRepo() {
		t.Fatalf("IsRepo() = true for a subdirectory; want only the top level")
	}
}

func TestShortHost(t *testing.T) {
	tests := map[string]string{"laptop.local": "laptop", "desktop": "desktop", "": "unknown"}
	for in, want := range tests {
		if got := shortHost(in); got != want {
			t.Errorf("shortHost(%q) = %q, want %q", in, got, want)
		}
	}
}
