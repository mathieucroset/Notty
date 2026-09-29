// Package gitsync is a typed, safe wrapper around the system git binary.
//
// Every operation runs `git -C <dir> ...` with a timeout (10s for local
// operations, 30s for network operations), a non-interactive environment and
// LC_ALL=C so stderr can be classified into sentinel errors (ErrNetwork,
// ErrAuth, ErrConflict, ErrLocalChanges, ...). See spec §7.
package gitsync

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Sentinel errors. Errors returned by Repo methods wrap one of these when the
// failure could be classified; test with errors.Is.
var (
	// ErrNetwork means the remote could not be reached (offline, DNS, refused,
	// timeout, or a local-path remote that no longer exists).
	ErrNetwork = errors.New("gitsync: network error")
	// ErrAuth means the remote rejected our credentials.
	ErrAuth = errors.New("gitsync: authentication failed")
	// ErrConflict means a merge stopped with conflicts; the merge is left in
	// progress. It is also returned when an operation is refused because a
	// merge is in progress.
	ErrConflict = errors.New("gitsync: merge conflict")
	// ErrNoRemote means the repository has no "origin" remote.
	ErrNoRemote = errors.New("gitsync: no remote configured")
	// ErrGitMissing means the git binary is not on PATH.
	ErrGitMissing = errors.New("gitsync: git binary not found")
	// ErrLocalChanges means git refused to merge because uncommitted local
	// changes would be overwritten.
	ErrLocalChanges = errors.New("gitsync: local changes would be overwritten by merge")
)

const (
	localTimeout   = 10 * time.Second
	networkTimeout = 30 * time.Second
	remoteName     = "origin"
)

// GitError is returned when a git command fails. Kind is the classified
// sentinel (nil when unclassified); Err is the underlying exec/context error.
type GitError struct {
	Args   []string
	Stderr string
	Kind   error
	Err    error
}

func (e *GitError) Error() string {
	var b strings.Builder
	b.WriteString("git")
	if sub := subcommand(e.Args); sub != "" {
		b.WriteString(" ")
		b.WriteString(sub)
	}
	if e.Kind != nil {
		b.WriteString(": ")
		b.WriteString(e.Kind.Error())
	}
	if e.Stderr != "" {
		b.WriteString(": ")
		b.WriteString(e.Stderr)
	} else if e.Err != nil {
		b.WriteString(": ")
		b.WriteString(e.Err.Error())
	}
	return b.String()
}

// Unwrap exposes both the classified sentinel and the underlying error, so
// errors.Is(err, ErrNetwork) and errors.Is(err, context.DeadlineExceeded)
// both work.
func (e *GitError) Unwrap() []error {
	var errs []error
	if e.Kind != nil {
		errs = append(errs, e.Kind)
	}
	if e.Err != nil {
		errs = append(errs, e.Err)
	}
	return errs
}

// result is the captured output of one git invocation.
type result struct {
	Stdout []byte
	Stderr []byte
}

// runner runs git in dir ("" means the process working directory) and
// returns its output. A non-zero exit is returned as err (with output still
// filled in); classification happens in the caller.
type runner func(ctx context.Context, dir string, args ...string) (result, error)

// Repo is a git working tree.
type Repo struct {
	Dir  string // absolute path of the working tree root
	Host string // short hostname used in commit messages
	run  runner

	mu        sync.Mutex
	lastMerge mergeRecord // output of the last Merge run through this Repo
}

// mergeRecord remembers git's output for a conflicted merge, keyed by the
// HEAD and MERGE_HEAD it left behind so it is never applied to another merge.
type mergeRecord struct {
	head, mergeHead string
	output          string
}

// Available reports whether a git binary is on PATH.
func Available() bool {
	_, err := exec.LookPath("git")
	return err == nil
}

// Open returns a Repo for dir without checking that it is a repository (see
// IsRepo). Host defaults to the short hostname of this machine.
func Open(dir string) *Repo {
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	h, _ := os.Hostname()
	return &Repo{Dir: dir, Host: shortHost(h), run: execRunner}
}

func shortHost(h string) string {
	if i := strings.IndexByte(h, '.'); i >= 0 {
		h = h[:i]
	}
	if h == "" {
		return "unknown"
	}
	return h
}

// IsRepo reports whether Dir is the top level of a git working tree. A
// directory nested inside some other repository is not a repo for Notty.
func (r *Repo) IsRepo() bool {
	res, err := r.runner()(context.Background(), r.Dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return false
	}
	top := strings.TrimSpace(string(res.Stdout))
	return samePath(top, r.Dir)
}

func samePath(a, b string) bool {
	ra, err1 := filepath.EvalSymlinks(a)
	rb, err2 := filepath.EvalSymlinks(b)
	if err1 != nil || err2 != nil {
		return filepath.Clean(a) == filepath.Clean(b)
	}
	return ra == rb
}

func (r *Repo) runner() runner {
	if r.run == nil {
		return execRunner
	}
	return r.run
}

// git runs a local git command in the repo with the local timeout.
func (r *Repo) git(args ...string) (result, error) {
	return runGit(context.Background(), r.runner(), r.Dir, false, args...)
}

// gitNet runs a network git command in the repo with the network timeout.
func (r *Repo) gitNet(ctx context.Context, args ...string) (result, error) {
	return runGit(ctx, r.runner(), r.Dir, true, args...)
}

// runGit applies the timeout, runs git and classifies failures.
func runGit(ctx context.Context, run runner, dir string, network bool, args ...string) (result, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	timeout := localTimeout
	if network {
		timeout = networkTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	res, err := run(ctx, dir, args...)
	if err != nil {
		return res, wrapRunError(args, network, ctx, res, err)
	}
	return res, nil
}

// wrapRunError turns a runner failure into a *GitError with a classified Kind.
func wrapRunError(args []string, network bool, ctx context.Context, res result, err error) error {
	if errors.Is(err, ErrGitMissing) {
		return err
	}
	stderr := strings.TrimSpace(string(res.Stderr))
	ge := &GitError{Args: args, Stderr: stderr, Err: err}
	if ctxErr := ctx.Err(); ctxErr != nil {
		ge.Err = ctxErr
		if network && errors.Is(ctxErr, context.DeadlineExceeded) {
			ge.Kind = ErrNetwork
		}
		return ge
	}
	ge.Kind = classify(args, network, string(res.Stdout), stderr)
	return ge
}

var (
	authPatterns = []string{
		"permission denied (publickey)",
		"authentication failed",
		"could not read username",
		"could not read password",
		"invalid username or password",
		"host key verification failed",
		"repository not found",
		"access denied",
	}
	reAuth = []*regexp.Regexp{
		regexp.MustCompile(`\b40[13]\b`),
		regexp.MustCompile(`permission to .* denied`),
		regexp.MustCompile(`repository '[^']*' not found`),
	}
	// A local-path remote that was moved or deleted, as reported by clone.
	reMissingLocalRepo = regexp.MustCompile(`repository '[^']*' does not exist`)
	networkPatterns    = []string{
		"could not resolve host",
		"connection refused",
		"network is unreachable",
		"timed out",
		"unable to access",
		"does not appear to be a git repository",
		"could not read from remote repository",
	}
)

// classify maps git output to a sentinel error, or nil if unrecognized.
// Merge outcomes (ErrConflict, ErrLocalChanges) apply to merge commands only;
// ErrAuth and ErrNetwork apply to network commands only, so a file name in a
// local command's error can never look like a network failure. Auth is
// checked before network because an auth failure over ssh also prints
// "Could not read from remote repository".
func classify(args []string, network bool, stdout, stderr string) error {
	lower := strings.ToLower(stderr)
	if subcommand(args) == "merge" {
		if strings.Contains(lower, "would be overwritten by merge") {
			return ErrLocalChanges
		}
		if hasConflictLine(stdout) || hasConflictLine(stderr) {
			return ErrConflict
		}
	}
	if !network {
		return nil
	}
	for _, p := range authPatterns {
		if strings.Contains(lower, p) {
			return ErrAuth
		}
	}
	for _, re := range reAuth {
		if re.MatchString(lower) {
			return ErrAuth
		}
	}
	if reMissingLocalRepo.MatchString(lower) {
		return ErrNetwork
	}
	for _, p := range networkPatterns {
		if strings.Contains(lower, p) {
			return ErrNetwork
		}
	}
	return nil
}

// subcommand returns the git subcommand in args, skipping leading "-c k=v"
// options.
func subcommand(args []string) string {
	for i := 0; i < len(args); i++ {
		if args[i] == "-c" {
			i++
			continue
		}
		return args[i]
	}
	return ""
}

func hasConflictLine(s string) bool {
	for line := range strings.SplitSeq(s, "\n") {
		if strings.HasPrefix(line, "CONFLICT (") {
			return true
		}
	}
	return false
}

// gitEnv returns the environment for git: the caller's environment plus
// GIT_TERMINAL_PROMPT=0, LC_ALL=C and a batch-mode GIT_SSH_COMMAND unless the
// user already set one.
func gitEnv(base []string) []string {
	env := make([]string, 0, len(base)+3)
	hasSSH := false
	for _, kv := range base {
		switch {
		case strings.HasPrefix(kv, "GIT_TERMINAL_PROMPT="), strings.HasPrefix(kv, "LC_ALL="):
			continue
		case strings.HasPrefix(kv, "GIT_SSH_COMMAND="):
			hasSSH = true
		}
		env = append(env, kv)
	}
	env = append(env, "GIT_TERMINAL_PROMPT=0", "LC_ALL=C")
	if !hasSSH {
		env = append(env, "GIT_SSH_COMMAND=ssh -o BatchMode=yes")
	}
	return env
}

// execRunner runs the real git binary.
func execRunner(ctx context.Context, dir string, args ...string) (result, error) {
	path, err := exec.LookPath("git")
	if err != nil {
		return result{}, fmt.Errorf("gitsync: %w", ErrGitMissing)
	}
	full := args
	if dir != "" {
		full = append([]string{"-C", dir}, args...)
	}
	cmd := exec.CommandContext(ctx, path, full...)
	cmd.Env = gitEnv(os.Environ())
	cmd.Stdin = nil
	// ssh children may hold the pipes open after git is killed on timeout.
	cmd.WaitDelay = 2 * time.Second
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	res := result{Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return res, fmt.Errorf("gitsync: %w", ErrGitMissing)
		}
		return res, err
	}
	return res, nil
}
