package setup

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// GH is the GitHub CLI as the wizard uses it.
type GH interface {
	// Available reports whether gh is installed and authenticated
	// (`gh auth status` succeeds).
	Available(ctx context.Context) bool
	// CreateRepo runs `gh repo create <name> --private --source . --remote
	// origin --push` in dir, which creates the repo, adds it as origin and
	// pushes the current branch with its upstream set.
	CreateRepo(ctx context.Context, dir, name string) error
}

// Runner runs the program name with args in dir ("" for the current
// directory) and returns its combined output.
type Runner func(ctx context.Context, dir, name string, args ...string) ([]byte, error)

// availableTimeout bounds `gh auth status`, which contacts GitHub.
const availableTimeout = 15 * time.Second

// NewGH returns a GH that runs gh through run.
func NewGH(run Runner) GH { return ghCLI{run: run} }

// DefaultGH returns a GH that runs the real gh binary.
func DefaultGH() GH { return NewGH(execRunner) }

type ghCLI struct{ run Runner }

func (g ghCLI) Available(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, availableTimeout)
	defer cancel()
	_, err := g.run(ctx, "", "gh", "auth", "status")
	return err == nil
}

// CreateRepo is bounded only by ctx, since the push may be large.
func (g ghCLI) CreateRepo(ctx context.Context, dir, name string) error {
	out, err := g.run(ctx, dir, "gh", "repo", "create", name, "--private", "--source", ".", "--remote", "origin", "--push")
	if err != nil {
		if msg := strings.TrimSpace(string(out)); msg != "" {
			return fmt.Errorf("gh repo create %s: %w: %s", name, err, msg)
		}
		return fmt.Errorf("gh repo create %s: %w", name, err)
	}
	return nil
}

func execRunner(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
	path, err := exec.LookPath(name)
	if err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GH_PROMPT_DISABLED=1", "GIT_TERMINAL_PROMPT=0", "NO_COLOR=1")
	return cmd.CombinedOutput()
}
