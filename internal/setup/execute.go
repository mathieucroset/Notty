package setup

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/mathieucroset/notty/internal/gitsync"
	"github.com/mathieucroset/notty/internal/vault"
)

// Progress is called before step i runs.
type Progress func(i int, s Step)

// StepError reports which step failed. It unwraps to the step's error, so
// errors.Is(err, gitsync.ErrAuth) and friends work on it.
type StepError struct {
	Index int
	Step  Step
	Err   error
}

func (e *StepError) Error() string { return fmt.Sprintf("setup: %s: %v", e.Step.Desc, e.Err) }
func (e *StepError) Unwrap() error { return e.Err }

// ErrRemoteExists is returned by the RemoteAdd step when origin is already
// configured with a different URL.
var ErrRemoteExists = errors.New("setup: the vault already has a different origin remote")

// Execute runs steps (from Plan) in the vault folder req.Vault, which is
// created if missing. It stops at the first failing step and returns a
// *StepError. conflicted is true when the MergeUnrelated step stopped with
// conflicts: the merge stays in progress and the remaining steps are skipped,
// so the caller starts the syncer, which enters Conflict. Re-running the same
// steps after a failure (retry) is safe.
func Execute(ctx context.Context, req Request, steps []Step, gh GH, progress Progress) (conflicted bool, err error) {
	if req.Vault == "" {
		return false, errors.New("setup: execute: no vault folder")
	}
	return ExecuteRepo(ctx, gitsync.Open(req.Vault), steps, gh, progress)
}

// ExecuteRepo is Execute on an existing Repo value (its Dir is the vault
// folder). It is what Job runs on the syncer's queue.
func ExecuteRepo(ctx context.Context, repo *gitsync.Repo, steps []Step, gh GH, progress Progress) (conflicted bool, err error) {
	for i, s := range steps {
		if err := ctx.Err(); err != nil {
			return false, &StepError{Index: i, Step: s, Err: err}
		}
		if progress != nil {
			progress(i, s)
		}
		next, conflicted, err := runStep(ctx, repo, s, gh)
		if err != nil {
			return false, &StepError{Index: i, Step: s, Err: err}
		}
		repo = next
		if conflicted {
			return true, nil
		}
	}
	return false, nil
}

// Job adapts ExecuteRepo to the syncer's RunSetup(func(*gitsync.Repo) error)
// (plan amendment A2). A conflicting merge is not an error: the syncer
// re-runs its start logic afterwards and enters Conflict because the merge is
// in progress. progress is called from the syncer's worker goroutine.
func Job(ctx context.Context, steps []Step, gh GH, progress Progress) func(*gitsync.Repo) error {
	return func(repo *gitsync.Repo) error {
		_, err := ExecuteRepo(ctx, repo, steps, gh, progress)
		return err
	}
}

// runStep runs one step and returns the repo to use from now on (Init and
// Clone produce a new one).
func runStep(ctx context.Context, repo *gitsync.Repo, s Step, gh GH) (*gitsync.Repo, bool, error) {
	arg := ""
	if s.Kind != StepWriteGitignore && s.Kind != StepFetch && s.Kind != StepPush {
		if len(s.Args) != 1 || s.Args[0] == "" {
			return repo, false, fmt.Errorf("step %v needs one argument, got %q", s.Kind, s.Args)
		}
		arg = s.Args[0]
	}
	dir := repo.Dir
	switch s.Kind {
	case StepInit:
		r, err := gitsync.Init(dir, arg)
		if err != nil {
			return repo, false, err
		}
		return r, false, nil
	case StepWriteGitignore:
		_, err := vault.EnsureGitignore(dir)
		return repo, false, err
	case StepCommitAll:
		if err := addAllExceptJunk(ctx, repo); err != nil {
			return repo, false, err
		}
		_, err := repo.Commit(arg)
		return repo, false, err
	case StepClone:
		r, err := cloneInto(ctx, arg, dir)
		if err != nil {
			return repo, false, err
		}
		return r, false, nil
	case StepRemoteAdd:
		if repo.HasRemote() {
			cur, err := localGit(ctx, dir, "config", "--get", "remote.origin.url")
			if err != nil {
				return repo, false, fmt.Errorf("read origin url: %w", err)
			}
			if cur != arg {
				return repo, false, fmt.Errorf("%w (%s)", ErrRemoteExists, cur)
			}
			return repo, false, nil
		}
		return repo, false, repo.RemoteAdd(arg)
	case StepFetch:
		return repo, false, repo.Fetch(ctx)
	case StepRenameBranch:
		cur, err := repo.CurrentBranch()
		if err != nil {
			return repo, false, err
		}
		if cur == arg {
			return repo, false, nil
		}
		if _, err := localGit(ctx, dir, "rev-parse", "-q", "--verify", "refs/heads/"+arg); err == nil {
			return repo, false, fmt.Errorf("a local branch %s already exists next to %s", arg, cur)
		}
		return repo, false, repo.RenameBranch(arg)
	case StepMergeUnrelated:
		err := repo.Merge(arg, true)
		if errors.Is(err, gitsync.ErrConflict) && repo.MergeInProgress() {
			return repo, true, nil
		}
		return repo, false, err
	case StepPush:
		return repo, false, repo.Push(ctx, true)
	case StepGHCreate:
		if gh == nil {
			return repo, false, ErrGHUnavailable
		}
		return repo, false, gh.CreateRepo(ctx, dir, arg)
	case StepEnsureGitignoreCommit:
		if repo.MergeInProgress() {
			return repo, false, nil
		}
		changed, err := vault.EnsureGitignore(dir)
		if err != nil || !changed {
			return repo, false, err
		}
		if err := repo.Add(".gitignore"); err != nil {
			return repo, false, err
		}
		_, err = repo.Commit(arg)
		return repo, false, err
	}
	return repo, false, fmt.Errorf("unknown step %v", s.Kind)
}

// junkPatterns returns the lines vault.EnsureGitignore guarantees, by
// letting it write a .gitignore in a scratch directory.
var junkPatterns = sync.OnceValues(func() (string, error) {
	tmp, err := os.MkdirTemp("", "notty-setup-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)
	if _, err := vault.EnsureGitignore(tmp); err != nil {
		return "", err
	}
	b, err := os.ReadFile(filepath.Join(tmp, ".gitignore"))
	return string(b), err
})

// addAllExceptJunk runs `git add -A` with Notty's .gitignore entries added
// to .git/info/exclude for the duration, so a commit made before .gitignore
// is written never picks up .notty/lock, recovery files or OS junk.
func addAllExceptJunk(ctx context.Context, repo *gitsync.Repo) (err error) {
	junk, err := junkPatterns()
	if err != nil {
		return fmt.Errorf("gitignore entries: %w", err)
	}
	p, err := localGit(ctx, repo.Dir, "rev-parse", "--git-path", "info/exclude")
	if err != nil {
		return fmt.Errorf("locate info/exclude: %w", err)
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(repo.Dir, p)
	}
	orig, rerr := os.ReadFile(p)
	existed := rerr == nil
	if rerr != nil && !errors.Is(rerr, os.ErrNotExist) {
		return fmt.Errorf("read info/exclude: %w", rerr)
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return fmt.Errorf("write info/exclude: %w", err)
	}
	content := string(orig)
	if content != "" && !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	if err := os.WriteFile(p, []byte(content+junk), 0o644); err != nil {
		return fmt.Errorf("write info/exclude: %w", err)
	}
	defer func() {
		var rerr error
		if existed {
			rerr = os.WriteFile(p, orig, 0o644)
		} else {
			rerr = os.Remove(p)
		}
		if rerr != nil {
			err = errors.Join(err, fmt.Errorf("restore info/exclude: %w", rerr))
		}
	}()
	return repo.AddAll()
}

// cloneInto clones url into dir. dir may be missing, empty, or hold only a
// .notty/ directory (plan A1): that directory is moved aside for the clone,
// then its entries that the clone does not have are moved back; for entries
// in both, the remote's version wins.
func cloneInto(ctx context.Context, url, dir string) (*gitsync.Repo, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) || (err == nil && len(entries) == 0) {
		return gitsync.Clone(ctx, url, dir)
	}
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if !(e.Name() == nottyDir && e.IsDir()) {
			return nil, fmt.Errorf("vault folder %s is not empty", dir)
		}
	}
	aside, err := os.MkdirTemp(filepath.Dir(dir), ".notty-setup-")
	if err != nil {
		return nil, err
	}
	saved := filepath.Join(aside, nottyDir)
	local := filepath.Join(dir, nottyDir)
	if err := os.Rename(local, saved); err != nil {
		_ = os.Remove(aside)
		return nil, err
	}
	repo, cerr := gitsync.Clone(ctx, url, dir)
	if cerr != nil {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, errors.Join(cerr, fmt.Errorf("restore .notty (kept in %s): %w", aside, err))
		}
		if err := os.Rename(saved, local); err != nil {
			return nil, errors.Join(cerr, fmt.Errorf("restore .notty (kept in %s): %w", aside, err))
		}
		_ = os.Remove(aside)
		return nil, cerr
	}
	if err := mergeBack(saved, local); err != nil {
		return nil, fmt.Errorf("restore .notty (kept in %s): %w", aside, err)
	}
	if err := os.RemoveAll(aside); err != nil {
		return nil, fmt.Errorf("remove %s: %w", aside, err)
	}
	return repo, nil
}

// mergeBack moves the entries of src that dst lacks into dst.
func mergeBack(src, dst string) error {
	if _, err := os.Lstat(dst); errors.Is(err, os.ErrNotExist) {
		return os.Rename(src, dst)
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		to := filepath.Join(dst, e.Name())
		if _, err := os.Lstat(to); err == nil {
			continue
		}
		if err := os.Rename(filepath.Join(src, e.Name()), to); err != nil {
			return err
		}
	}
	return nil
}

// localGit runs a read-only local git command that gitsync has no API for
// and returns its trimmed stdout.
func localGit(ctx context.Context, dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	env := slices.DeleteFunc(os.Environ(), func(kv string) bool {
		name, _, _ := strings.Cut(kv, "=")
		return slices.Contains([]string{"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "LC_ALL"}, name)
	})
	cmd.Env = append(env, "LC_ALL=C", "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(out)), nil
}
