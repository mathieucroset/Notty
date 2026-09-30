package gitsync

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// StatusEntry is one entry of `git status --porcelain=v2 -z`.
type StatusEntry struct {
	// Kind is the porcelain v2 entry type: '1' ordinary change, '2' rename or
	// copy, 'u' unmerged, '?' untracked, '!' ignored.
	Kind byte
	// XY is the two-letter index/worktree status ("M.", ".M", "R.", "UU", ...);
	// empty for untracked and ignored entries.
	XY       string
	Path     string // slash-separated, relative to the repo root
	OrigPath string // source path for Kind '2'
}

// Change is one entry of DiffNameStatus.
type Change struct {
	Status  byte   // 'A', 'M', 'D' or 'R'
	Path    string // the (new) path, relative to the repo root
	OldPath string // the source path when Status is 'R'
}

// Init creates dir (and parents) if needed and runs `git init -b <branch>`.
func Init(dir, branch string) (*Repo, error) {
	if err := checkArg("branch", branch); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("gitsync: init: %w", err)
	}
	r := Open(dir)
	if _, err := r.git("init", "-q", "-b", branch); err != nil {
		return nil, fmt.Errorf("gitsync: init %s: %w", dir, err)
	}
	return r, nil
}

// Clone clones url into dir (created with its parents if missing; it must be
// empty if it exists).
func Clone(ctx context.Context, url, dir string) (*Repo, error) {
	return CloneBranch(ctx, url, dir, "")
}

// CloneBranch is Clone checking out branch instead of the remote's HEAD
// (which may point to a branch that does not exist). An empty branch means
// the remote's HEAD.
func CloneBranch(ctx context.Context, url, dir, branch string) (*Repo, error) {
	if err := checkArg("url", url); err != nil {
		return nil, err
	}
	args := []string{"clone", "-q"}
	if branch != "" {
		if err := checkArg("branch", branch); err != nil {
			return nil, err
		}
		args = append(args, "-b", branch)
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("gitsync: clone: %w", err)
	}
	parent := filepath.Dir(abs)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return nil, fmt.Errorf("gitsync: clone: %w", err)
	}
	run := newExecRunner(configuredSSHCommand(""))
	if _, err := runGit(ctx, run, parent, true, append(args, "--", url, abs)...); err != nil {
		return nil, fmt.Errorf("gitsync: clone %s: %w", url, err)
	}
	return Open(abs), nil
}

// LsRemote inspects a remote with `git ls-remote --symref <url> HEAD`. An
// empty remote returns (false, "", nil). If the remote's HEAD points to a
// branch that does not exist but other branches do, it still reports history
// and picks main, master or the first branch as the default.
func LsRemote(ctx context.Context, url string) (hasHistory bool, defaultBranch string, err error) {
	if err := checkArg("url", url); err != nil {
		return false, "", err
	}
	run := newExecRunner(configuredSSHCommand(""))
	res, err := runGit(ctx, run, "", true, "ls-remote", "--symref", url, "HEAD")
	if err != nil {
		return false, "", fmt.Errorf("gitsync: ls-remote: %w", err)
	}
	symref, hasHead := parseLsRemoteHead(string(res.Stdout))
	if hasHead {
		return true, symref, nil
	}
	res, err = runGit(ctx, run, "", true, "ls-remote", "--heads", url)
	if err != nil {
		return false, "", fmt.Errorf("gitsync: ls-remote: %w", err)
	}
	var heads []string
	for line := range strings.SplitSeq(string(res.Stdout), "\n") {
		if _, ref, ok := strings.Cut(line, "\t"); ok {
			if name, ok := strings.CutPrefix(ref, "refs/heads/"); ok {
				heads = append(heads, name)
			}
		}
	}
	if len(heads) == 0 {
		return false, "", nil
	}
	for _, want := range []string{symref, "main", "master"} {
		for _, h := range heads {
			if want != "" && h == want {
				return true, h, nil
			}
		}
	}
	return true, heads[0], nil
}

// parseLsRemoteHead returns the branch HEAD points to (if advertised) and
// whether HEAD resolves to a commit.
func parseLsRemoteHead(out string) (branch string, hasHead bool) {
	for line := range strings.SplitSeq(out, "\n") {
		left, ref, ok := strings.Cut(line, "\t")
		if !ok || ref != "HEAD" {
			continue
		}
		if target, ok := strings.CutPrefix(left, "ref: "); ok {
			branch = strings.TrimPrefix(target, "refs/heads/")
		} else if left != "" {
			hasHead = true
		}
	}
	return branch, hasHead
}

// RemoteAdd adds url as the "origin" remote.
func (r *Repo) RemoteAdd(url string) error {
	if err := checkArg("url", url); err != nil {
		return err
	}
	if _, err := r.git("remote", "add", remoteName, url); err != nil {
		return fmt.Errorf("gitsync: remote add: %w", err)
	}
	return nil
}

// HasRemote reports whether the "origin" remote is configured.
func (r *Repo) HasRemote() bool {
	_, err := r.git("remote", "get-url", remoteName)
	return err == nil
}

// CheckIdentity reports whether git can author and commit in dir: it runs
// `git var GIT_AUTHOR_IDENT` and `GIT_COMMITTER_IDENT` there when dir is a
// repository, else outside any repository (the global identity). It returns
// an error wrapping ErrNoIdentity when git has no usable name or email.
func CheckIdentity(ctx context.Context, dir string) error {
	if dir == "" || !Open(dir).IsRepo() {
		dir = os.TempDir()
	}
	for _, v := range []string{"GIT_AUTHOR_IDENT", "GIT_COMMITTER_IDENT"} {
		if _, err := runGit(ctx, execRunner, dir, false, "var", v); err != nil {
			return fmt.Errorf("gitsync: check identity: %w", err)
		}
	}
	return nil
}

// SetGlobalIdentity sets user.name and user.email in the global git config.
func SetGlobalIdentity(ctx context.Context, name, email string) error {
	if strings.TrimSpace(name) == "" || strings.TrimSpace(email) == "" {
		return errors.New("gitsync: set identity: name and email are required")
	}
	for _, kv := range [][2]string{{"user.name", name}, {"user.email", email}} {
		if _, err := runGit(ctx, execRunner, os.TempDir(), false, "config", "--global", kv[0], kv[1]); err != nil {
			return fmt.Errorf("gitsync: set identity: %w", err)
		}
	}
	return nil
}

// RemoteURL returns the configured URL of origin, or an error wrapping
// ErrNoRemote when origin is not configured.
func (r *Repo) RemoteURL() (string, error) {
	res, err := r.git("config", "--get", "remote."+remoteName+".url")
	if exitCode(err) == 1 {
		return "", fmt.Errorf("gitsync: remote url: %w", ErrNoRemote)
	}
	if err != nil {
		return "", fmt.Errorf("gitsync: remote url: %w", err)
	}
	return strings.TrimSpace(string(res.Stdout)), nil
}

// GitPath returns the absolute path of p inside the repository's git
// directory (`git rev-parse --git-path`), e.g. "info/exclude".
func (r *Repo) GitPath(p string) (string, error) {
	if err := checkArg("git path", p); err != nil {
		return "", err
	}
	res, err := r.git("rev-parse", "--git-path", p)
	if err != nil {
		return "", fmt.Errorf("gitsync: git path %s: %w", p, err)
	}
	out := strings.TrimSpace(string(res.Stdout))
	if !filepath.IsAbs(out) {
		out = filepath.Join(r.Dir, out)
	}
	return out, nil
}

// CurrentBranch returns the checked-out branch name (also for an unborn
// branch). It fails on a detached HEAD.
func (r *Repo) CurrentBranch() (string, error) {
	res, err := r.git("symbolic-ref", "--short", "-q", "HEAD")
	if err != nil {
		return "", fmt.Errorf("gitsync: current branch: %w", err)
	}
	return strings.TrimSpace(string(res.Stdout)), nil
}

// RenameBranch renames the current branch (`git branch -m <name>`). It
// fails, changing nothing, if a branch with that name already exists.
func (r *Repo) RenameBranch(name string) error {
	if err := checkArg("branch", name); err != nil {
		return err
	}
	if _, err := r.git("branch", "-m", name); err != nil {
		return fmt.Errorf("gitsync: rename branch to %s: %w", name, err)
	}
	return nil
}

// nottyExcludes are Notty's own working files, which never belong in a
// commit: save temp files and the watcher's barrier sentinels (*.notty-tmp),
// the running-instance lock and the crash-recovery buffers. The vault's
// .gitignore lists them too (vault.GitignoreEntries), but that file is
// shared and a pulled version may drop them.
var nottyExcludes = []string{"*.notty-tmp", ".notty/lock", ".notty/recovery/"}

// excludeHeader introduces the lines ensureExcludes adds.
const excludeHeader = "# Notty's working files, never committed (added by Notty)"

// ensureExcludes adds the missing nottyExcludes to .git/info/exclude, which
// git reads besides .gitignore and which is never shared.
func (r *Repo) ensureExcludes() error {
	p, err := r.GitPath("info/exclude")
	if err != nil {
		return err
	}
	data, err := os.ReadFile(p)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("gitsync: read info/exclude: %w", err)
	}
	have := map[string]bool{}
	for line := range strings.Lines(string(data)) {
		have[strings.TrimSpace(line)] = true
	}
	var add strings.Builder
	for _, e := range nottyExcludes {
		if !have[e] {
			add.WriteString(e + "\n")
		}
	}
	if add.Len() == 0 {
		return nil
	}
	content := string(data)
	if content != "" && !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	if !have[excludeHeader] {
		content += excludeHeader + "\n"
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return fmt.Errorf("gitsync: write info/exclude: %w", err)
	}
	if err := os.WriteFile(p, []byte(content+add.String()), 0o644); err != nil {
		return fmt.Errorf("gitsync: write info/exclude: %w", err)
	}
	return nil
}

// AddAll stages every change (`git add -A`) except Notty's own working
// files (see nottyExcludes), which it keeps listed in .git/info/exclude. It
// refuses while a merge is in progress (spec §7: only the resolver stages
// files then); the error wraps ErrConflict.
func (r *Repo) AddAll() error {
	if r.MergeInProgress() {
		return fmt.Errorf("gitsync: add all refused during a merge: %w", ErrConflict)
	}
	if err := r.ensureExcludes(); err != nil {
		return err
	}
	if _, err := r.git("add", "-A"); err != nil {
		return fmt.Errorf("gitsync: add all: %w", err)
	}
	return nil
}

// Add stages the given paths, including deletions of tracked paths.
func (r *Repo) Add(paths ...string) error {
	if len(paths) == 0 {
		return nil
	}
	if err := checkPaths(paths); err != nil {
		return err
	}
	args := append([]string{"add", "-A", "--"}, paths...)
	if _, err := r.git(args...); err != nil {
		return fmt.Errorf("gitsync: add: %w", err)
	}
	return nil
}

// Remove removes paths from the index (all stages, so an unmerged path is
// resolved as deleted) and deletes the working files if present. Paths that
// are neither tracked nor present are ignored.
func (r *Repo) Remove(paths ...string) error {
	if len(paths) == 0 {
		return nil
	}
	if err := checkPaths(paths); err != nil {
		return err
	}
	args := append([]string{"rm", "-q", "--cached", "-f", "--ignore-unmatch", "--"}, paths...)
	if _, err := r.git(args...); err != nil {
		return fmt.Errorf("gitsync: remove: %w", err)
	}
	for _, p := range paths {
		if err := os.Remove(filepath.Join(r.Dir, filepath.FromSlash(p))); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("gitsync: remove %s: %w", p, err)
		}
	}
	return nil
}

// Commit commits what is staged. It returns (false, nil) when nothing is
// staged. It refuses while a merge is in progress (use CommitMerge); the error
// wraps ErrConflict.
func (r *Repo) Commit(msg string) (committed bool, err error) {
	if r.MergeInProgress() {
		return false, fmt.Errorf("gitsync: commit refused during a merge: %w", ErrConflict)
	}
	if _, err := r.git("diff", "--cached", "--quiet", "--no-ext-diff"); err == nil {
		return false, nil
	} else if exitCode(err) != 1 {
		return false, fmt.Errorf("gitsync: commit: %w", err)
	}
	if _, err := r.git(noHooks("commit", "-q", "-m", msg)...); err != nil {
		return false, fmt.Errorf("gitsync: commit: %w", err)
	}
	return true, nil
}

// Status returns the parsed `git status --porcelain=v2 -z` entries, listing
// untracked files individually.
func (r *Repo) Status() ([]StatusEntry, error) {
	res, err := r.git("status", "--porcelain=v2", "-z", "--untracked-files=all")
	if err != nil {
		return nil, fmt.Errorf("gitsync: status: %w", err)
	}
	entries, err := parseStatus(string(res.Stdout))
	if err != nil {
		return nil, fmt.Errorf("gitsync: status: %w", err)
	}
	return entries, nil
}

func parseStatus(out string) ([]StatusEntry, error) {
	var entries []StatusEntry
	recs := strings.Split(out, "\x00")
	for i := 0; i < len(recs); i++ {
		rec := recs[i]
		if rec == "" {
			continue
		}
		switch rec[0] {
		case '1', '2', 'u':
			// Fields before the path: 1 -> 8, 2 -> 9, u -> 10.
			n := map[byte]int{'1': 8, '2': 9, 'u': 10}[rec[0]]
			f := strings.SplitN(rec, " ", n+1)
			if len(f) != n+1 {
				return nil, fmt.Errorf("malformed status record %q", rec)
			}
			e := StatusEntry{Kind: rec[0], XY: f[1], Path: f[n]}
			if rec[0] == '2' {
				i++
				if i >= len(recs) {
					return nil, fmt.Errorf("rename record without source: %q", rec)
				}
				e.OrigPath = recs[i]
			}
			entries = append(entries, e)
		case '?', '!':
			if len(rec) < 3 {
				return nil, fmt.Errorf("malformed status record %q", rec)
			}
			entries = append(entries, StatusEntry{Kind: rec[0], Path: rec[2:]})
		case '#':
			// header lines (only with --branch)
		default:
			return nil, fmt.Errorf("unknown status record %q", rec)
		}
	}
	return entries, nil
}

// Fetch fetches origin. It returns ErrNoRemote without a remote.
func (r *Repo) Fetch(ctx context.Context) error {
	if !r.HasRemote() {
		return fmt.Errorf("gitsync: fetch: %w", ErrNoRemote)
	}
	if _, err := r.gitNet(ctx, "fetch", "-q", remoteName); err != nil {
		return fmt.Errorf("gitsync: fetch: %w", err)
	}
	return nil
}

// Push pushes the current branch to origin; setUpstream adds -u. It returns
// ErrNoRemote without a remote.
func (r *Repo) Push(ctx context.Context, setUpstream bool) error {
	if !r.HasRemote() {
		return fmt.Errorf("gitsync: push: %w", ErrNoRemote)
	}
	branch, err := r.CurrentBranch()
	if err != nil {
		return fmt.Errorf("gitsync: push: %w", err)
	}
	args := []string{"push", "-q"}
	if setUpstream {
		args = append(args, "-u")
	}
	args = append(args, remoteName, "refs/heads/"+branch+":refs/heads/"+branch)
	if _, err := r.gitNet(ctx, args...); err != nil {
		return fmt.Errorf("gitsync: push: %w", err)
	}
	return nil
}

// HasUpstream reports whether the current branch has an upstream configured.
func (r *Repo) HasUpstream() bool {
	_, err := r.git("rev-parse", "-q", "--abbrev-ref", "--symbolic-full-name", "@{upstream}")
	return err == nil
}

// AheadBehind counts commits between HEAD and origin/<branch>. When that
// remote-tracking branch does not exist (no remote, or never pushed), ahead is
// the number of local commits and behind is 0. An unborn branch is (0, 0).
func (r *Repo) AheadBehind() (ahead, behind int, err error) {
	if !r.hasCommit("HEAD") {
		return 0, 0, nil
	}
	branch, err := r.CurrentBranch()
	if err != nil {
		return 0, 0, fmt.Errorf("gitsync: ahead/behind: %w", err)
	}
	tracking := "refs/remotes/" + remoteName + "/" + branch
	if !r.hasCommit(tracking) {
		res, err := r.git("rev-list", "--count", "HEAD")
		if err != nil {
			return 0, 0, fmt.Errorf("gitsync: ahead/behind: %w", err)
		}
		n, err := strconv.Atoi(strings.TrimSpace(string(res.Stdout)))
		if err != nil {
			return 0, 0, fmt.Errorf("gitsync: ahead/behind: parse count: %w", err)
		}
		return n, 0, nil
	}
	res, err := r.git("rev-list", "--left-right", "--count", "HEAD..."+tracking)
	if err != nil {
		return 0, 0, fmt.Errorf("gitsync: ahead/behind: %w", err)
	}
	f := strings.Fields(string(res.Stdout))
	if len(f) != 2 {
		return 0, 0, fmt.Errorf("gitsync: ahead/behind: unexpected output %q", res.Stdout)
	}
	if ahead, err = strconv.Atoi(f[0]); err == nil {
		behind, err = strconv.Atoi(f[1])
	}
	if err != nil {
		return 0, 0, fmt.Errorf("gitsync: ahead/behind: parse counts: %w", err)
	}
	return ahead, behind, nil
}

func (r *Repo) hasCommit(rev string) bool {
	_, err := r.git("rev-parse", "-q", "--verify", rev+"^{commit}")
	return err == nil
}

// Merge merges ref into the current branch without opening an editor.
// Fast-forwards are allowed and rename detection is always on. It returns an
// error wrapping ErrConflict when the merge stopped with conflicts (the merge
// stays in progress) or when a merge is already in progress, and
// ErrLocalChanges when git refused to start because uncommitted changes would
// be overwritten.
func (r *Repo) Merge(ref string, allowUnrelated bool) error {
	return r.MergeWithMessage(ref, allowUnrelated, "")
}

// MergeWithMessage is Merge with msg as the merge commit message (also left
// in MERGE_MSG when the merge conflicts). An empty msg means git's default.
func (r *Repo) MergeWithMessage(ref string, allowUnrelated bool, msg string) error {
	if err := checkArg("ref", ref); err != nil {
		return err
	}
	if r.MergeInProgress() {
		return fmt.Errorf("gitsync: merge %s refused, a merge is already in progress: %w", ref, ErrConflict)
	}
	args := []string{"merge", "--no-edit", "--ff", "--no-autostash", "-Xfind-renames"}
	if allowUnrelated {
		args = append(args, "--allow-unrelated-histories")
	}
	if msg != "" {
		args = append(args, "-m", msg)
	}
	args = append(args, ref)
	res, err := r.git(noHooks(args...)...)
	if errors.Is(err, ErrConflict) {
		r.recordMerge(string(res.Stdout) + "\n" + string(res.Stderr))
	} else {
		r.clearMergeRecord()
	}
	if err != nil {
		return fmt.Errorf("gitsync: merge %s: %w", ref, err)
	}
	return nil
}

// recordMerge keeps the output of a conflicted merge for PathConflicts.
func (r *Repo) recordMerge(output string) {
	rec := mergeRecord{head: r.revParse("HEAD"), mergeHead: r.revParse("MERGE_HEAD"), output: output}
	if rec.mergeHead == "" {
		rec = mergeRecord{}
	}
	r.mu.Lock()
	r.lastMerge = rec
	r.mu.Unlock()
}

// recordedMergeOutput returns the recorded merge output if it belongs to the
// merge currently in progress, else "".
func (r *Repo) recordedMergeOutput() string {
	r.mu.Lock()
	rec := r.lastMerge
	r.mu.Unlock()
	if rec.mergeHead == "" || rec.mergeHead != r.revParse("MERGE_HEAD") || rec.head != r.revParse("HEAD") {
		return ""
	}
	return rec.output
}

func (r *Repo) clearMergeRecord() {
	r.mu.Lock()
	r.lastMerge = mergeRecord{}
	r.mu.Unlock()
}

// revParse resolves rev to an object ID, or "" if it does not exist.
func (r *Repo) revParse(rev string) string {
	res, err := r.git("rev-parse", "-q", "--verify", rev)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(res.Stdout))
}

// MergeInProgress reports whether MERGE_HEAD exists.
func (r *Repo) MergeInProgress() bool {
	_, err := r.git("rev-parse", "-q", "--verify", "MERGE_HEAD")
	return err == nil
}

// CommitMerge concludes the merge in progress with msg. It fails if no merge
// is in progress or unresolved conflicts remain.
func (r *Repo) CommitMerge(msg string) error {
	if !r.MergeInProgress() {
		return errors.New("gitsync: commit merge: no merge in progress")
	}
	if _, err := r.git(noHooks("commit", "-q", "-m", msg)...); err != nil {
		return fmt.Errorf("gitsync: commit merge: %w", err)
	}
	r.clearMergeRecord()
	return nil
}

// AbortMerge runs `git merge --abort`.
func (r *Repo) AbortMerge() error {
	if _, err := r.git("merge", "--abort"); err != nil {
		return fmt.Errorf("gitsync: abort merge: %w", err)
	}
	r.clearMergeRecord()
	return nil
}

// DiffNameStatus lists the files that differ between from and to, with
// rename detection. An empty to compares from with the working tree (tracked
// files only). Type changes and unmerged entries are reported as 'M', copies
// as 'A'.
func (r *Repo) DiffNameStatus(from, to string) ([]Change, error) {
	if err := checkArg("rev", from); err != nil {
		return nil, err
	}
	args := []string{"diff", "--name-status", "-z", "-M", "--no-ext-diff", "--no-color", from}
	if to != "" {
		if err := checkArg("rev", to); err != nil {
			return nil, err
		}
		args = append(args, to)
	}
	args = append(args, "--")
	res, err := r.git(args...)
	if err != nil {
		return nil, fmt.Errorf("gitsync: diff %s %s: %w", from, to, err)
	}
	changes, err := parseNameStatus(string(res.Stdout))
	if err != nil {
		return nil, fmt.Errorf("gitsync: diff %s %s: %w", from, to, err)
	}
	return changes, nil
}

func parseNameStatus(out string) ([]Change, error) {
	var changes []Change
	tok := strings.Split(out, "\x00")
	for i := 0; i < len(tok); i++ {
		st := tok[i]
		if st == "" {
			continue
		}
		next := func() (string, error) {
			i++
			if i >= len(tok) || tok[i] == "" {
				return "", fmt.Errorf("truncated name-status output after %q", st)
			}
			return tok[i], nil
		}
		switch st[0] {
		case 'R', 'C':
			old, err := next()
			if err != nil {
				return nil, err
			}
			nw, err := next()
			if err != nil {
				return nil, err
			}
			if st[0] == 'R' {
				changes = append(changes, Change{Status: 'R', Path: nw, OldPath: old})
			} else {
				changes = append(changes, Change{Status: 'A', Path: nw})
			}
		case 'A', 'M', 'D', 'T', 'U', 'X':
			p, err := next()
			if err != nil {
				return nil, err
			}
			s := st[0]
			if s != 'A' && s != 'D' {
				s = 'M'
			}
			changes = append(changes, Change{Status: s, Path: p})
		default:
			return nil, fmt.Errorf("unknown name-status %q", st)
		}
	}
	return changes, nil
}

// MergeBase returns the best common ancestor of a and b.
func (r *Repo) MergeBase(a, b string) (string, error) {
	if err := checkArg("rev", a); err != nil {
		return "", err
	}
	if err := checkArg("rev", b); err != nil {
		return "", err
	}
	res, err := r.git("merge-base", a, b)
	if err != nil {
		return "", fmt.Errorf("gitsync: merge-base %s %s: %w", a, b, err)
	}
	return strings.TrimSpace(string(res.Stdout)), nil
}

// noHooks prefixes a commit-creating git command with options that disable
// hooks and signing, so no hook or pinentry prompt can block or draw over the
// TUI.
func noHooks(args ...string) []string {
	return append([]string{"-c", "core.hooksPath=" + os.DevNull, "-c", "commit.gpgsign=false"}, args...)
}

// checkArg rejects empty values and values that git would parse as options.
func checkArg(name, v string) error {
	if v == "" || strings.HasPrefix(v, "-") {
		return fmt.Errorf("gitsync: invalid %s %q", name, v)
	}
	return nil
}

// checkPaths rejects paths that are absolute or escape the repository.
func checkPaths(paths []string) error {
	for _, p := range paths {
		if !filepath.IsLocal(filepath.FromSlash(p)) {
			return fmt.Errorf("gitsync: path %q is outside the repository", p)
		}
	}
	return nil
}

// exitCode returns the process exit code wrapped in err, or -1.
func exitCode(err error) int {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return -1
}

// LogEntry is one commit in a file's history.
type LogEntry struct {
	Rev     string
	Date    time.Time // author date
	Subject string
	Host    string // parsed from a " · <host>" subject suffix, else ""
	Added   int    // lines added to the file (0 for binary files and merges)
	Deleted int    // lines deleted from the file
}

// Log returns the history of path, newest first, following renames.
func (r *Repo) Log(path string) ([]LogEntry, error) {
	if err := checkPaths([]string{path}); err != nil {
		return nil, err
	}
	res, err := r.git("log", "--follow", "-M", "--numstat", "--no-color", "--no-show-signature",
		"--format=%x1e%H%x1f%aI%x1f%s", "--", path)
	if err != nil {
		return nil, fmt.Errorf("gitsync: log %s: %w", path, err)
	}
	entries, err := parseLog(string(res.Stdout))
	if err != nil {
		return nil, fmt.Errorf("gitsync: log %s: %w", path, err)
	}
	return entries, nil
}

func parseLog(out string) ([]LogEntry, error) {
	var entries []LogEntry
	for rec := range strings.SplitSeq(out, "\x1e") {
		if strings.TrimSpace(rec) == "" {
			continue
		}
		header, body, _ := strings.Cut(rec, "\n")
		f := strings.SplitN(header, "\x1f", 3)
		if len(f) != 3 {
			return nil, fmt.Errorf("malformed log record %q", header)
		}
		date, err := time.Parse(time.RFC3339, f[1])
		if err != nil {
			return nil, fmt.Errorf("parse date %q: %w", f[1], err)
		}
		e := LogEntry{Rev: f[0], Date: date, Subject: f[2], Host: HostFromSubject(f[2])}
		for line := range strings.SplitSeq(body, "\n") {
			nums := strings.SplitN(line, "\t", 3)
			if len(nums) != 3 {
				continue
			}
			// Binary files report "-".
			a, _ := strconv.Atoi(nums[0])
			d, _ := strconv.Atoi(nums[1])
			e.Added += a
			e.Deleted += d
		}
		entries = append(entries, e)
	}
	return entries, nil
}

// HostFromSubject returns the host of a Notty commit subject such as
// "Update a.md · laptop", or "" when the subject has no " · " suffix.
func HostFromSubject(subject string) string {
	i := strings.LastIndex(subject, " · ")
	if i < 0 {
		return ""
	}
	host := strings.TrimSpace(subject[i+len(" · "):])
	if strings.ContainsAny(host, " \t") {
		return ""
	}
	return host
}

// ShowAt returns the content of path at revision rev.
func (r *Repo) ShowAt(rev, path string) ([]byte, error) {
	if err := checkArg("rev", rev); err != nil {
		return nil, err
	}
	if err := checkPaths([]string{path}); err != nil {
		return nil, err
	}
	return r.catBlob(rev + ":" + path)
}

// LastCommitAdding returns the newest non-merge commit reachable from HEAD
// that added path, or "" if there is none. Rename detection is off, so moving
// a file (such as into .trash/) counts as adding its destination.
func (r *Repo) LastCommitAdding(path string) (string, error) {
	if err := checkPaths([]string{path}); err != nil {
		return "", err
	}
	res, err := r.git("log", "-1", "--no-merges", "--no-renames", "--diff-filter=A",
		"--no-show-signature", "--format=%H", "HEAD", "--", path)
	if err != nil {
		return "", fmt.Errorf("gitsync: last commit adding %s: %w", path, err)
	}
	return strings.TrimSpace(string(res.Stdout)), nil
}

// LastCommitHostFor returns the host of the newest commit that touched path
// and is reachable from before (inclusive), or "" if there is none or its
// subject names no host.
func (r *Repo) LastCommitHostFor(path, before string) (string, error) {
	if err := checkArg("rev", before); err != nil {
		return "", err
	}
	if err := checkPaths([]string{path}); err != nil {
		return "", err
	}
	res, err := r.git("log", "-1", "--no-show-signature", "--format=%s", before, "--", path)
	if err != nil {
		return "", fmt.Errorf("gitsync: last commit for %s: %w", path, err)
	}
	return HostFromSubject(strings.TrimSpace(string(res.Stdout))), nil
}
