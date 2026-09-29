package gitsync_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mathieucroset/notty/internal/gitsync"
	"github.com/mathieucroset/notty/internal/gitsync/gittest"
)

var ctx = context.Background()

func TestInitCreatesBranch(t *testing.T) {
	gittest.Isolate(t)
	dir := filepath.Join(t.TempDir(), "vault", "nested")
	r, err := gitsync.Init(dir, "trunk")
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	if !r.IsRepo() {
		t.Fatalf("IsRepo() = false after Init")
	}
	branch, err := r.CurrentBranch()
	if err != nil || branch != "trunk" {
		t.Fatalf("CurrentBranch() = %q, %v; want trunk", branch, err)
	}
	if r.HasRemote() {
		t.Fatalf("HasRemote() = true on fresh repo")
	}
	if r.HasUpstream() {
		t.Fatalf("HasUpstream() = true on fresh repo")
	}
}

func TestCommitNothingToCommit(t *testing.T) {
	gittest.Isolate(t)
	r, err := gitsync.Init(t.TempDir(), "main")
	if err != nil {
		t.Fatal(err)
	}
	gittest.SetUser(t, r, "T", "t@example.com")

	// Unborn branch, empty index.
	ok, err := r.Commit("empty")
	if err != nil || ok {
		t.Fatalf("Commit on empty repo = %v, %v; want false, nil", ok, err)
	}
	gittest.Write(t, r, "a.md", "a\n")
	gittest.CommitAll(t, r, "Create a.md · test")
	if err := r.AddAll(); err != nil {
		t.Fatal(err)
	}
	ok, err = r.Commit("again")
	if err != nil || ok {
		t.Fatalf("Commit with clean tree = %v, %v; want false, nil", ok, err)
	}
	// Unstaged change only: still nothing to commit.
	gittest.Write(t, r, "a.md", "changed\n")
	ok, err = r.Commit("unstaged")
	if err != nil || ok {
		t.Fatalf("Commit with unstaged change = %v, %v; want false, nil", ok, err)
	}
}

func TestStatus(t *testing.T) {
	env := gittest.New(t)
	r := env.Laptop
	gittest.Write(t, r, "a.md", "a\n")
	gittest.Write(t, r, "b.md", "b\n")
	gittest.CommitAll(t, r, "add")
	gittest.Write(t, r, "a.md", "changed\n")        // modified, unstaged
	gittest.Write(t, r, "dir/new file.md", "new\n") // untracked in a new dir
	if err := r.Remove("b.md"); err != nil {        // staged deletion
		t.Fatal(err)
	}
	gittest.Move(t, r, "README.md", "Docs/README.md")
	if err := r.Add("README.md", "Docs/README.md"); err != nil { // staged rename
		t.Fatal(err)
	}

	entries, err := r.Status()
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	got := map[string]gitsync.StatusEntry{}
	for _, e := range entries {
		got[e.Path] = e
	}
	check := func(path string, kind byte, xy string) {
		t.Helper()
		e, ok := got[path]
		if !ok {
			t.Fatalf("Status missing %q: %+v", path, entries)
		}
		if e.Kind != kind || e.XY != xy {
			t.Errorf("%s: Kind=%c XY=%q, want %c %q", path, e.Kind, e.XY, kind, xy)
		}
	}
	check("a.md", '1', ".M")
	check("b.md", '1', "D.")
	check("dir/new file.md", '?', "")
	check("Docs/README.md", '2', "R.")
	if got["Docs/README.md"].OrigPath != "README.md" {
		t.Errorf("rename OrigPath = %q, want README.md", got["Docs/README.md"].OrigPath)
	}
}

func TestAddAndRemove(t *testing.T) {
	env := gittest.New(t)
	r := env.Laptop
	gittest.Write(t, r, "keep.md", "k\n")
	gittest.Write(t, r, "gone.md", "g\n")
	gittest.CommitAll(t, r, "two notes")

	if err := r.Remove("gone.md", "never-existed.md"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := os.Stat(filepath.Join(r.Dir, "gone.md")); !os.IsNotExist(err) {
		t.Fatalf("Remove left the working file: %v", err)
	}
	gittest.Write(t, r, "untracked.md", "u\n")
	if err := r.Remove("untracked.md"); err != nil {
		t.Fatalf("Remove untracked: %v", err)
	}
	if _, err := os.Stat(filepath.Join(r.Dir, "untracked.md")); !os.IsNotExist(err) {
		t.Fatalf("Remove left an untracked working file")
	}
	if err := r.Remove("../escape.md"); err == nil {
		t.Fatalf("Remove accepted a path outside the repo")
	}
	ok, err := r.Commit("remove gone")
	if err != nil || !ok {
		t.Fatalf("Commit after Remove = %v, %v", ok, err)
	}
	if got := gittest.Git(t, r.Dir, "ls-files"); got != "README.md\nkeep.md" {
		t.Fatalf("tracked files = %q", got)
	}

	// Add stages only the given paths, including deletions.
	gittest.Write(t, r, "keep.md", "k2\n")
	gittest.Write(t, r, "other.md", "o\n")
	if err := os.Remove(filepath.Join(r.Dir, "README.md")); err != nil {
		t.Fatal(err)
	}
	if err := r.Add("keep.md", "README.md"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if got := gittest.Git(t, r.Dir, "diff", "--cached", "--name-status"); got != "D\tREADME.md\nM\tkeep.md" {
		t.Fatalf("staged = %q", got)
	}
	if err := r.Add(); err != nil {
		t.Fatalf("Add() with no paths: %v", err)
	}
}

func TestAddRemoveLiteralPaths(t *testing.T) {
	env := gittest.New(t)
	r := env.Laptop
	for _, p := range []string{"a[1].md", "a1.md", ":odd.md", "*.md"} {
		gittest.Write(t, r, p, p+"\n")
	}
	if err := r.Add("a[1].md", ":odd.md"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if got := gittest.Git(t, r.Dir, "diff", "--cached", "--name-only"); got != ":odd.md\na[1].md" {
		t.Fatalf("staged = %q, want only the literal paths", got)
	}
	gittest.CommitAll(t, r, "all")

	if err := r.Remove("a[1].md", "*.md"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if got := gittest.Git(t, r.Dir, "ls-files"); got != ":odd.md\nREADME.md\na1.md" {
		t.Fatalf("tracked after Remove = %q, want a1.md and README.md kept", got)
	}
	for _, p := range []string{"a1.md", ":odd.md", "README.md"} {
		if _, err := os.Stat(filepath.Join(r.Dir, p)); err != nil {
			t.Errorf("%s deleted by Remove of a glob-like path: %v", p, err)
		}
	}
	if err := r.Remove(":odd.md"); err != nil {
		t.Fatalf("Remove(:odd.md): %v", err)
	}
	if got := gittest.Git(t, r.Dir, "ls-files"); got != "README.md\na1.md" {
		t.Fatalf("tracked after Remove(:odd.md) = %q", got)
	}
}

func TestCommitsIgnoreHooksAndSigning(t *testing.T) {
	env := gittest.New(t)
	hooks := t.TempDir()
	for _, h := range []string{"pre-commit", "prepare-commit-msg", "commit-msg", "pre-merge-commit", "post-commit"} {
		script := "#!/bin/sh\necho hook " + h + " ran >&2\nexit 1\n"
		for _, dir := range []string{hooks, filepath.Join(env.Laptop.Dir, ".git", "hooks")} {
			if err := os.WriteFile(filepath.Join(dir, h), []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}
		}
	}
	gittest.Git(t, env.Laptop.Dir, "config", "core.hooksPath", hooks)
	gittest.Git(t, env.Laptop.Dir, "config", "commit.gpgsign", "true")
	gittest.Git(t, env.Laptop.Dir, "config", "gpg.program", "false")

	// Plain commit.
	gittest.Write(t, env.Laptop, "a.md", "a\n")
	gittest.CommitAll(t, env.Laptop, "Create a.md · laptop")

	// Clean non-fast-forward merge creates a merge commit.
	gittest.Write(t, env.Desktop, "b.md", "b\n")
	gittest.CommitAll(t, env.Desktop, "Create b.md · desktop")
	gittest.Push(t, env.Desktop)
	gittest.Sync(t, env.Laptop)

	// Conflicted merge concluded with CommitMerge.
	gittest.Write(t, env.Desktop, "README.md", "desktop\n")
	gittest.CommitAll(t, env.Desktop, "desk")
	gittest.Push(t, env.Desktop)
	gittest.Write(t, env.Laptop, "README.md", "laptop\n")
	gittest.CommitAll(t, env.Laptop, "lap")
	if err := env.Laptop.Fetch(ctx); err != nil {
		t.Fatal(err)
	}
	if err := env.Laptop.Merge("origin/main", false); !errors.Is(err, gitsync.ErrConflict) {
		t.Fatalf("Merge err = %v, want ErrConflict", err)
	}
	gittest.Write(t, env.Laptop, "README.md", "resolved\n")
	if err := env.Laptop.Add("README.md"); err != nil {
		t.Fatal(err)
	}
	if err := env.Laptop.CommitMerge("Merge · laptop"); err != nil {
		t.Fatalf("CommitMerge with failing hooks/signing: %v", err)
	}
	if got := gittest.Git(t, env.Laptop.Dir, "log", "-1", "--format=%s"); got != "Merge · laptop" {
		t.Fatalf("last commit = %q", got)
	}
}

func TestFetchMergePushRoundTrip(t *testing.T) {
	env := gittest.New(t)
	lap, desk := env.Laptop, env.Desktop

	ahead, behind, err := desk.AheadBehind()
	if err != nil || ahead != 0 || behind != 0 {
		t.Fatalf("desktop AheadBehind initially = %d, %d, %v", ahead, behind, err)
	}

	gittest.Write(t, desk, "Work/Standup notes.md", "# Standup\n")
	gittest.CommitAll(t, desk, "Create Standup notes.md · desktop")
	ahead, behind, err = desk.AheadBehind()
	if err != nil || ahead != 1 || behind != 0 {
		t.Fatalf("desktop AheadBehind after commit = %d, %d, %v; want 1, 0", ahead, behind, err)
	}
	if err := desk.Push(ctx, false); err != nil {
		t.Fatalf("desktop Push: %v", err)
	}
	ahead, behind, _ = desk.AheadBehind()
	if ahead != 0 || behind != 0 {
		t.Fatalf("desktop AheadBehind after push = %d, %d", ahead, behind)
	}

	// Laptop has not fetched yet: nothing known.
	ahead, behind, _ = lap.AheadBehind()
	if ahead != 0 || behind != 0 {
		t.Fatalf("laptop AheadBehind before fetch = %d, %d", ahead, behind)
	}
	if err := lap.Fetch(ctx); err != nil {
		t.Fatalf("laptop Fetch: %v", err)
	}
	ahead, behind, _ = lap.AheadBehind()
	if ahead != 0 || behind != 1 {
		t.Fatalf("laptop AheadBehind after fetch = %d, %d; want 0, 1", ahead, behind)
	}
	gittest.Write(t, lap, "ideas.md", "ideas\n")
	gittest.CommitAll(t, lap, "Create ideas.md · laptop")
	ahead, behind, _ = lap.AheadBehind()
	if ahead != 1 || behind != 1 {
		t.Fatalf("laptop AheadBehind diverged = %d, %d; want 1, 1", ahead, behind)
	}
	if err := lap.Merge("origin/main", false); err != nil {
		t.Fatalf("laptop Merge: %v", err)
	}
	if lap.MergeInProgress() {
		t.Fatalf("MergeInProgress after clean merge")
	}
	if got := gittest.Read(t, lap, "Work/Standup notes.md"); got != "# Standup\n" {
		t.Fatalf("merged file = %q", got)
	}
	ahead, behind, _ = lap.AheadBehind()
	if ahead != 2 || behind != 0 {
		t.Fatalf("laptop AheadBehind after merge = %d, %d; want 2, 0", ahead, behind)
	}
	gittest.Push(t, lap)

	// Desktop fast-forwards; merging again is a no-op.
	gittest.Sync(t, desk)
	if got := gittest.Read(t, desk, "ideas.md"); got != "ideas\n" {
		t.Fatalf("desktop ideas.md = %q", got)
	}
	changes, err := desk.DiffNameStatus("ORIG_HEAD", "")
	if err != nil {
		t.Fatalf("DiffNameStatus: %v", err)
	}
	if !slices.Contains(changes, gitsync.Change{Status: 'A', Path: "ideas.md"}) {
		t.Fatalf("DiffNameStatus(ORIG_HEAD, \"\") after fast-forward = %+v, want ideas.md added", changes)
	}
	if err := desk.Merge("origin/main", false); err != nil {
		t.Fatalf("up-to-date Merge: %v", err)
	}
}

func TestAheadBehindWithoutRemoteBranch(t *testing.T) {
	gittest.Isolate(t)
	r, err := gitsync.Init(t.TempDir(), "main")
	if err != nil {
		t.Fatal(err)
	}
	gittest.SetUser(t, r, "T", "t@example.com")
	ahead, behind, err := r.AheadBehind()
	if err != nil || ahead != 0 || behind != 0 {
		t.Fatalf("unborn AheadBehind = %d, %d, %v", ahead, behind, err)
	}
	for i, name := range []string{"a.md", "b.md"} {
		gittest.Write(t, r, name, name)
		gittest.CommitAll(t, r, name)
		ahead, behind, err = r.AheadBehind()
		if err != nil || ahead != i+1 || behind != 0 {
			t.Fatalf("AheadBehind = %d, %d, %v; want %d, 0", ahead, behind, err, i+1)
		}
	}
	// With a remote that has no such branch yet, and first push with -u.
	remote := gittest.NewEmptyRemote(t)
	if err := r.RemoteAdd(remote); err != nil {
		t.Fatalf("RemoteAdd: %v", err)
	}
	if !r.HasRemote() {
		t.Fatalf("HasRemote() = false after RemoteAdd")
	}
	if err := r.Fetch(ctx); err != nil {
		t.Fatalf("Fetch from empty remote: %v", err)
	}
	if ahead, _, _ := r.AheadBehind(); ahead != 2 {
		t.Fatalf("AheadBehind with empty remote: ahead = %d, want 2", ahead)
	}
	if err := r.Push(ctx, true); err != nil {
		t.Fatalf("Push -u: %v", err)
	}
	if !r.HasUpstream() {
		t.Fatalf("HasUpstream() = false after Push(setUpstream)")
	}
	if ahead, behind, _ := r.AheadBehind(); ahead != 0 || behind != 0 {
		t.Fatalf("AheadBehind after push = %d, %d", ahead, behind)
	}
}

func TestNoRemote(t *testing.T) {
	gittest.Isolate(t)
	r, err := gitsync.Init(t.TempDir(), "main")
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Fetch(ctx); !errors.Is(err, gitsync.ErrNoRemote) {
		t.Fatalf("Fetch err = %v, want ErrNoRemote", err)
	}
	if err := r.Push(ctx, true); !errors.Is(err, gitsync.ErrNoRemote) {
		t.Fatalf("Push err = %v, want ErrNoRemote", err)
	}
}

func TestUnreachableRemoteIsNetworkError(t *testing.T) {
	env := gittest.New(t)
	gittest.Write(t, env.Laptop, "a.md", "a\n")
	gittest.CommitAll(t, env.Laptop, "a")
	env.MakeRemoteUnreachable(t)

	err := env.Laptop.Fetch(ctx)
	if !errors.Is(err, gitsync.ErrNetwork) {
		t.Fatalf("Fetch err = %v, want ErrNetwork", err)
	}
	var ge *gitsync.GitError
	if !errors.As(err, &ge) || ge.Stderr == "" {
		t.Fatalf("Fetch err should be a *GitError with stderr, got %#v", err)
	}
	if err := env.Laptop.Push(ctx, false); !errors.Is(err, gitsync.ErrNetwork) {
		t.Fatalf("Push err = %v, want ErrNetwork", err)
	}
	if _, _, err := gitsync.LsRemote(ctx, env.Remote); !errors.Is(err, gitsync.ErrNetwork) {
		t.Fatalf("LsRemote err = %v, want ErrNetwork", err)
	}
	// Pending count still works offline.
	if ahead, _, err := env.Laptop.AheadBehind(); err != nil || ahead != 1 {
		t.Fatalf("AheadBehind offline = %d, %v", ahead, err)
	}

	env.RestoreRemote(t)
	if err := env.Laptop.Push(ctx, false); err != nil {
		t.Fatalf("Push after restore: %v", err)
	}
}

func TestLsRemote(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		remote := gittest.NewEmptyRemote(t)
		has, branch, err := gitsync.LsRemote(ctx, remote)
		if err != nil || has || branch != "" {
			t.Fatalf("LsRemote(empty) = %v, %q, %v; want false, \"\", nil", has, branch, err)
		}
	})
	t.Run("with history", func(t *testing.T) {
		env := gittest.New(t)
		has, branch, err := gitsync.LsRemote(ctx, env.Remote)
		if err != nil || !has || branch != "main" {
			t.Fatalf("LsRemote = %v, %q, %v; want true, main, nil", has, branch, err)
		}
	})
	t.Run("non-main default", func(t *testing.T) {
		gittest.Isolate(t)
		remote := filepath.Join(t.TempDir(), "r.git")
		gittest.Git(t, "", "init", "-q", "--bare", "-b", "trunk", remote)
		r, err := gitsync.Init(t.TempDir(), "trunk")
		if err != nil {
			t.Fatal(err)
		}
		gittest.SetUser(t, r, "T", "t@example.com")
		gittest.Write(t, r, "a.md", "a")
		gittest.CommitAll(t, r, "a")
		if err := r.RemoteAdd(remote); err != nil {
			t.Fatal(err)
		}
		if err := r.Push(ctx, true); err != nil {
			t.Fatal(err)
		}
		has, branch, err := gitsync.LsRemote(ctx, remote)
		if err != nil || !has || branch != "trunk" {
			t.Fatalf("LsRemote = %v, %q, %v; want true, trunk, nil", has, branch, err)
		}
	})
	t.Run("HEAD points to a missing branch", func(t *testing.T) {
		// Remote HEAD -> main, but only "notes" was pushed.
		remote := gittest.NewEmptyRemote(t)
		r, err := gitsync.Init(t.TempDir(), "notes")
		if err != nil {
			t.Fatal(err)
		}
		gittest.SetUser(t, r, "T", "t@example.com")
		gittest.Write(t, r, "a.md", "a")
		gittest.CommitAll(t, r, "a")
		if err := r.RemoteAdd(remote); err != nil {
			t.Fatal(err)
		}
		if err := r.Push(ctx, true); err != nil {
			t.Fatal(err)
		}
		has, branch, err := gitsync.LsRemote(ctx, remote)
		if err != nil || !has || branch != "notes" {
			t.Fatalf("LsRemote = %v, %q, %v; want true, notes, nil", has, branch, err)
		}
	})
}

func TestClone(t *testing.T) {
	env := gittest.New(t)
	dir := filepath.Join(t.TempDir(), "a", "b", "Notes")
	r, err := gitsync.Clone(ctx, env.Remote, dir)
	if err != nil {
		t.Fatalf("Clone: %v", err)
	}
	if r.Dir != dir || !r.IsRepo() || !r.HasRemote() || !r.HasUpstream() {
		t.Fatalf("clone state: dir=%q repo=%v remote=%v upstream=%v", r.Dir, r.IsRepo(), r.HasRemote(), r.HasUpstream())
	}
	if got := gittest.Read(t, r, "README.md"); got != "# Notes\n" {
		t.Fatalf("README.md = %q", got)
	}
	// Cloning into an existing empty directory works too.
	empty := t.TempDir()
	if _, err := gitsync.Clone(ctx, env.Remote, empty); err != nil {
		t.Fatalf("Clone into empty dir: %v", err)
	}
	env.MakeRemoteUnreachable(t)
	if _, err := gitsync.Clone(ctx, env.Remote, filepath.Join(t.TempDir(), "x")); !errors.Is(err, gitsync.ErrNetwork) {
		t.Fatalf("Clone of unreachable remote err = %v, want ErrNetwork", err)
	}
}

func TestRenameBranch(t *testing.T) {
	gittest.Isolate(t)
	r, err := gitsync.Init(t.TempDir(), "main")
	if err != nil {
		t.Fatal(err)
	}
	gittest.SetUser(t, r, "T", "t@example.com")
	// Unborn branch can be renamed.
	if err := r.RenameBranch("master"); err != nil {
		t.Fatalf("RenameBranch unborn: %v", err)
	}
	if b, _ := r.CurrentBranch(); b != "master" {
		t.Fatalf("CurrentBranch = %q, want master", b)
	}
	gittest.Write(t, r, "a.md", "a")
	gittest.CommitAll(t, r, "a")
	if err := r.RenameBranch("main"); err != nil {
		t.Fatalf("RenameBranch: %v", err)
	}
	if b, _ := r.CurrentBranch(); b != "main" {
		t.Fatalf("CurrentBranch = %q, want main", b)
	}
	if got := gittest.Git(t, r.Dir, "log", "--format=%s"); got != "a" {
		t.Fatalf("history lost after rename: %q", got)
	}
}

func TestMergeUnrelatedHistories(t *testing.T) {
	env := gittest.New(t)
	r, err := gitsync.Init(t.TempDir(), "main")
	if err != nil {
		t.Fatal(err)
	}
	gittest.SetUser(t, r, "T", "t@example.com")
	gittest.Write(t, r, "local.md", "local\n")
	gittest.CommitAll(t, r, "Initial commit · here")
	if err := r.RemoteAdd(env.Remote); err != nil {
		t.Fatal(err)
	}
	if err := r.Fetch(ctx); err != nil {
		t.Fatal(err)
	}
	if err := r.Merge("origin/main", false); err == nil {
		t.Fatalf("Merge of unrelated histories without allowUnrelated succeeded")
	}
	if err := r.Merge("origin/main", true); err != nil {
		t.Fatalf("Merge allowUnrelated: %v", err)
	}
	if gittest.Read(t, r, "README.md") != "# Notes\n" || gittest.Read(t, r, "local.md") != "local\n" {
		t.Fatalf("unrelated merge did not combine files")
	}
	if err := r.Push(ctx, true); err != nil {
		t.Fatalf("Push -u after unrelated merge: %v", err)
	}
}

func TestMergeRefusedByLocalChanges(t *testing.T) {
	env := gittest.New(t)
	gittest.Write(t, env.Desktop, "README.md", "remote edit\n")
	gittest.CommitAll(t, env.Desktop, "edit")
	gittest.Push(t, env.Desktop)

	gittest.Write(t, env.Laptop, "README.md", "uncommitted local edit\n")
	if err := env.Laptop.Fetch(ctx); err != nil {
		t.Fatal(err)
	}
	err := env.Laptop.Merge("origin/main", false)
	if !errors.Is(err, gitsync.ErrLocalChanges) {
		t.Fatalf("Merge err = %v, want ErrLocalChanges", err)
	}
	if env.Laptop.MergeInProgress() {
		t.Fatalf("refused merge left a merge in progress")
	}
}

func TestMergeConflictAndAbort(t *testing.T) {
	env := gittest.New(t)
	gittest.Write(t, env.Desktop, "README.md", "desktop\n")
	gittest.CommitAll(t, env.Desktop, "desk")
	gittest.Push(t, env.Desktop)
	gittest.Write(t, env.Laptop, "README.md", "laptop\n")
	gittest.CommitAll(t, env.Laptop, "lap")
	if err := env.Laptop.Fetch(ctx); err != nil {
		t.Fatal(err)
	}
	err := env.Laptop.Merge("origin/main", false)
	if !errors.Is(err, gitsync.ErrConflict) {
		t.Fatalf("Merge err = %v, want ErrConflict", err)
	}
	if !env.Laptop.MergeInProgress() {
		t.Fatalf("MergeInProgress() = false after conflicted merge")
	}
	if err := env.Laptop.AddAll(); !errors.Is(err, gitsync.ErrConflict) {
		t.Fatalf("AddAll during merge err = %v, want ErrConflict refusal", err)
	}
	if err := env.Laptop.Merge("origin/main", false); !errors.Is(err, gitsync.ErrConflict) {
		t.Fatalf("Merge during merge err = %v, want ErrConflict", err)
	}
	if err := env.Laptop.CommitMerge("Merge · laptop"); err == nil {
		t.Fatalf("CommitMerge with unresolved conflicts succeeded")
	}
	if err := env.Laptop.AbortMerge(); err != nil {
		t.Fatalf("AbortMerge: %v", err)
	}
	if env.Laptop.MergeInProgress() {
		t.Fatalf("MergeInProgress() after abort")
	}
	if got := gittest.Read(t, env.Laptop, "README.md"); got != "laptop\n" {
		t.Fatalf("README after abort = %q", got)
	}
}

func TestCommitMerge(t *testing.T) {
	env := gittest.New(t)
	gittest.Write(t, env.Desktop, "README.md", "desktop\n")
	gittest.CommitAll(t, env.Desktop, "desk")
	gittest.Push(t, env.Desktop)
	gittest.Write(t, env.Laptop, "README.md", "laptop\n")
	gittest.CommitAll(t, env.Laptop, "lap")
	if err := env.Laptop.Fetch(ctx); err != nil {
		t.Fatal(err)
	}
	if err := env.Laptop.Merge("origin/main", false); !errors.Is(err, gitsync.ErrConflict) {
		t.Fatalf("Merge err = %v", err)
	}
	gittest.Write(t, env.Laptop, "README.md", "resolved\n")
	if err := env.Laptop.Add("README.md"); err != nil {
		t.Fatal(err)
	}
	if err := env.Laptop.CommitMerge("Merge · laptop"); err != nil {
		t.Fatalf("CommitMerge: %v", err)
	}
	if env.Laptop.MergeInProgress() {
		t.Fatalf("MergeInProgress after CommitMerge")
	}
	if got := strings.Fields(gittest.Git(t, env.Laptop.Dir, "rev-list", "--parents", "-n1", "HEAD")); len(got) != 3 {
		t.Fatalf("last commit is not a two-parent merge: %q", got)
	}
	if got := gittest.Git(t, env.Laptop.Dir, "log", "-1", "--format=%s"); got != "Merge · laptop" {
		t.Fatalf("merge subject = %q", got)
	}
	if ahead, behind, _ := env.Laptop.AheadBehind(); ahead != 2 || behind != 0 {
		t.Fatalf("AheadBehind after merge = %d, %d", ahead, behind)
	}
}

func TestDiffNameStatus(t *testing.T) {
	env := gittest.New(t)
	r := env.Laptop
	gittest.Write(t, r, "a.md", "alpha content that is long enough to be detected as a rename\n")
	gittest.Write(t, r, "b.md", "b\n")
	gittest.Write(t, r, "c.md", "c\n")
	gittest.CommitAll(t, r, "base")
	base := gittest.Git(t, r.Dir, "rev-parse", "HEAD")
	gittest.Move(t, r, "a.md", ".trash/1/a.md")
	gittest.Write(t, r, "b.md", "b changed\n")
	if err := os.Remove(filepath.Join(r.Dir, "c.md")); err != nil {
		t.Fatal(err)
	}
	gittest.Write(t, r, "d.md", "d\n")
	gittest.CommitAll(t, r, "changes")

	want := []gitsync.Change{
		{Status: 'R', Path: ".trash/1/a.md", OldPath: "a.md"},
		{Status: 'M', Path: "b.md"},
		{Status: 'D', Path: "c.md"},
		{Status: 'A', Path: "d.md"},
	}
	got, err := r.DiffNameStatus(base, "HEAD")
	if err != nil {
		t.Fatalf("DiffNameStatus: %v", err)
	}
	sortChanges(got)
	if !slices.Equal(got, want) {
		t.Fatalf("DiffNameStatus = %+v\nwant %+v", got, want)
	}

	// Working tree comparison sees uncommitted edits to tracked files.
	gittest.Write(t, r, "d.md", "d2\n")
	got, err = r.DiffNameStatus("HEAD", "")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, []gitsync.Change{{Status: 'M', Path: "d.md"}}) {
		t.Fatalf("DiffNameStatus(HEAD, \"\") = %+v", got)
	}

	mb, err := r.MergeBase(base, "HEAD")
	if err != nil || mb != base {
		t.Fatalf("MergeBase = %q, %v; want %q", mb, err, base)
	}
}

func sortChanges(c []gitsync.Change) {
	slices.SortFunc(c, func(a, b gitsync.Change) int {
		switch {
		case a.Path < b.Path:
			return -1
		case a.Path > b.Path:
			return 1
		}
		return 0
	})
}
