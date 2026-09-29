package setup

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/mathieucroset/notty/internal/gitsync"
)

// GH is the GitHub CLI as the wizard uses it.
type GH interface {
	// Available reports whether gh is installed and authenticated with
	// github.com (`gh auth status --hostname github.com` succeeds).
	Available(ctx context.Context) bool
	// CreateRepo runs `gh repo create <name> --private --source . --remote
	// origin --push` in dir, which creates the repo, adds it as origin and
	// pushes the current branch with its upstream set. Recognized failures
	// wrap gitsync.ErrAuth or gitsync.ErrNetwork.
	CreateRepo(ctx context.Context, dir, name string) error
}

// Runner runs the program name with args in dir ("" for the current
// directory) and returns its combined output.
type Runner func(ctx context.Context, dir, name string, args ...string) ([]byte, error)

const (
	// availableTimeout bounds `gh auth status`, which contacts GitHub.
	availableTimeout = 15 * time.Second
	// createTimeout bounds `gh repo create`, including its initial push.
	createTimeout = 2 * time.Minute
)

// NewGH returns a GH that runs gh through run.
func NewGH(run Runner) GH { return ghCLI{run: run} }

// DefaultGH returns a GH that runs the real gh binary.
func DefaultGH() GH { return NewGH(execRunner) }

type ghCLI struct{ run Runner }

func (g ghCLI) Available(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, availableTimeout)
	defer cancel()
	_, err := g.run(ctx, "", "gh", "auth", "status", "--hostname", "github.com")
	return err == nil
}

func (g ghCLI) CreateRepo(ctx context.Context, dir, name string) error {
	ctx, cancel := context.WithTimeout(ctx, createTimeout)
	defer cancel()
	out, err := g.run(ctx, dir, "gh", "repo", "create", name, "--private", "--source", ".", "--remote", "origin", "--push")
	if err == nil {
		return nil
	}
	msg := strings.TrimSpace(string(out))
	kind := classifyGH(msg)
	if kind == nil && errors.Is(ctx.Err(), context.DeadlineExceeded) {
		kind = gitsync.ErrNetwork
	}
	switch {
	case kind != nil && msg != "":
		return fmt.Errorf("gh repo create %s: %w: %w: %s", name, kind, err, msg)
	case kind != nil:
		return fmt.Errorf("gh repo create %s: %w: %w", name, kind, err)
	case msg != "":
		return fmt.Errorf("gh repo create %s: %w: %s", name, err, msg)
	}
	return fmt.Errorf("gh repo create %s: %w", name, err)
}

var (
	ghAuthPatterns = []string{
		"not logged in", "gh auth login", "authentication", "bad credentials",
		"http 401", "permission denied (publickey)", "host key verification failed",
		"could not read username",
	}
	ghNetworkPatterns = []string{
		"could not resolve", "no such host", "timeout", "timed out",
		"connection refused", "network is unreachable", "error connecting to",
	}
)

// classifyGH maps gh's output to gitsync.ErrAuth or gitsync.ErrNetwork, or
// nil when unrecognized.
func classifyGH(out string) error {
	lower := strings.ToLower(out)
	for _, p := range ghAuthPatterns {
		if strings.Contains(lower, p) {
			return gitsync.ErrAuth
		}
	}
	for _, p := range ghNetworkPatterns {
		if strings.Contains(lower, p) {
			return gitsync.ErrNetwork
		}
	}
	return nil
}

// execRunner runs the real program with gitsync's non-interactive
// environment (gh runs git for the push), in its own process group so a
// timeout also kills the git and ssh it spawned.
func execRunner(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
	path, err := exec.LookPath(name)
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Dir = dir
	cmd.Env = append(gitsync.HelperEnv(dir), "GH_PROMPT_DISABLED=1", "GH_NO_UPDATE_NOTIFIER=1", "NO_COLOR=1")
	gitsync.KillGroupOnCancel(cmd)
	cmd.WaitDelay = 2 * time.Second
	return cmd.CombinedOutput()
}
