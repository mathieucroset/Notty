package setup

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
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
			return conflicted, &StepError{Index: i, Step: s, Err: err}
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
	want := 1
	switch s.Kind {
	case StepWriteGitignore, StepFetch, StepPush:
		want = 0
	case StepClone, StepMergeUnrelated:
		want = 2
	}
	if len(s.Args) != want || slices.Contains(s.Args, "") {
		return repo, false, fmt.Errorf("step %v needs %d non-empty arguments, got %q", s.Kind, want, s.Args)
	}
	arg := ""
	if want > 0 {
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
		if err := addAllExceptJunk(repo); err != nil {
			return repo, false, err
		}
		_, err := repo.Commit(arg)
		return repo, false, err
	case StepClone:
		if existing := gitsync.Open(dir); existing.IsRepo() {
			// A retry after the clone succeeded.
			cur, err := existing.RemoteURL()
			if err != nil || !SameURL(cur, arg) {
				return repo, false, fmt.Errorf("%w (%s)", ErrRemoteExists, cur)
			}
			return existing, false, nil
		}
		r, err := cloneInto(ctx, arg, s.Args[1], dir)
		if err != nil {
			return repo, false, err
		}
		return r, false, nil
	case StepRemoteAdd:
		cur, err := repo.RemoteURL()
		switch {
		case errors.Is(err, gitsync.ErrNoRemote):
			return repo, false, repo.RemoteAdd(arg)
		case err != nil:
			return repo, false, err
		case !SameURL(cur, arg):
			return repo, false, fmt.Errorf("%w (%s)", ErrRemoteExists, cur)
		}
		return repo, false, nil
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
		return repo, false, repo.RenameBranch(arg) // refuses to overwrite a branch
	case StepMergeUnrelated:
		err := repo.MergeWithMessage(arg, true, s.Args[1])
		if errors.Is(err, gitsync.ErrConflict) && repo.MergeInProgress() {
			return repo, true, writeGitignoreDuringMerge(repo)
		}
		return repo, false, err
	case StepPush:
		return repo, false, repo.Push(ctx, true)
	case StepGHCreate:
		if repo.HasRemote() {
			// A retry after gh created the repo and added origin.
			return repo, false, repo.Push(ctx, true)
		}
		if gh == nil {
			return repo, false, ErrGHUnavailable
		}
		return repo, false, gh.CreateRepo(ctx, dir, arg)
	case StepEnsureGitignoreCommit:
		if repo.MergeInProgress() {
			return repo, false, nil
		}
		if _, err := vault.EnsureGitignore(dir); err != nil {
			return repo, false, err
		}
		// Decide from git, not from whether the file was just written, so a
		// retry commits a .gitignore written by an earlier failed attempt.
		entries, err := repo.Status()
		if err != nil {
			return repo, false, err
		}
		if !slices.ContainsFunc(entries, func(e gitsync.StatusEntry) bool { return e.Path == ".gitignore" && e.Kind != '!' }) {
			return repo, false, nil
		}
		if err := repo.Add(".gitignore"); err != nil {
			return repo, false, err
		}
		_, err = repo.Commit(arg)
		return repo, false, err
	}
	return repo, false, fmt.Errorf("unknown step %v", s.Kind)
}

// writeGitignoreDuringMerge makes sure .gitignore lists Notty's entries
// while a setup merge waits for the resolver, so the first commits after the
// resolution never pick up .notty/lock or recovery files. It does not stage
// or commit, and leaves a conflicted .gitignore alone for the resolver.
func writeGitignoreDuringMerge(repo *gitsync.Repo) error {
	conflicts, err := repo.ConflictedFiles()
	if err != nil {
		return err
	}
	for _, c := range conflicts {
		if c.Path == ".gitignore" {
			return nil
		}
	}
	_, err = vault.EnsureGitignore(repo.Dir)
	return err
}

// SameURL reports whether two remote URLs name the same repository,
// ignoring surrounding space, trailing slashes and a ".git" suffix.
func SameURL(a, b string) bool { return normalizeURL(a) == normalizeURL(b) }

func normalizeURL(u string) string {
	u = strings.TrimRight(strings.TrimSpace(u), "/")
	return strings.TrimRight(strings.TrimSuffix(u, ".git"), "/")
}

// addAllExceptJunk runs `git add -A` with Notty's .gitignore entries added
// to .git/info/exclude for the duration, so a commit made before .gitignore
// is written never picks up .notty/lock, recovery files or OS junk.
func addAllExceptJunk(repo *gitsync.Repo) (err error) {
	junk := strings.Join(vault.GitignoreEntries(), "\n") + "\n"
	p, err := repo.GitPath("info/exclude")
	if err != nil {
		return err
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
// .notty/ directory (plan A1). That directory is moved aside for the clone
// and merged back afterwards: entries the clone lacks are moved back, and a
// local file that differs from the clone's copy is kept under
// .notty/recovery/setup-<timestamp>/ (the remote's copy stays in place), so
// no local file is ever lost. If anything is left over, the error names the
// directory holding it.
func cloneInto(ctx context.Context, url, branch, dir string) (*gitsync.Repo, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) || (err == nil && len(entries) == 0) {
		return gitsync.CloneBranch(ctx, url, dir, branch)
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
	repo, cerr := gitsync.CloneBranch(ctx, url, dir, branch)
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
	rescue := filepath.Join(local, "recovery", "setup-"+time.Now().Format("20060102-150405"))
	if err := mergeBack(saved, local, rescue); err != nil {
		return nil, fmt.Errorf("restore .notty (local files kept in %s): %w", aside, err)
	}
	if err := os.Remove(aside); err != nil {
		return nil, fmt.Errorf("restore .notty (local files kept in %s): %w", aside, err)
	}
	return repo, nil
}

// mergeBack moves everything under src into dst. Directories present on
// both sides are merged recursively. A file present on both sides keeps
// dst's version; src's is dropped when identical, else moved to the same
// relative path under rescue. src and its emptied subdirectories are removed.
func mergeBack(src, dst, rescue string) error {
	if _, err := os.Lstat(dst); errors.Is(err, os.ErrNotExist) {
		return os.Rename(src, dst)
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		from, to := filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())
		fi, err := os.Lstat(to)
		switch {
		case errors.Is(err, os.ErrNotExist):
			err = os.Rename(from, to)
		case err != nil:
		case e.IsDir() && fi.IsDir():
			err = mergeBack(from, to, filepath.Join(rescue, e.Name()))
		case e.Type().IsRegular() && fi.Mode().IsRegular() && sameContent(from, to):
			err = os.Remove(from)
		default:
			err = moveUnique(from, filepath.Join(rescue, e.Name()))
		}
		if err != nil {
			return err
		}
	}
	return os.Remove(src)
}

func sameContent(a, b string) bool {
	x, err1 := os.ReadFile(a)
	y, err2 := os.ReadFile(b)
	return err1 == nil && err2 == nil && string(x) == string(y)
}

// moveUnique renames from to to (creating parents), adding a numeric suffix
// if to already exists.
func moveUnique(from, to string) error {
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return err
	}
	target := to
	for i := 2; ; i++ {
		if _, err := os.Lstat(target); errors.Is(err, os.ErrNotExist) {
			break
		}
		target = fmt.Sprintf("%s.%d", to, i)
	}
	return os.Rename(from, target)
}
