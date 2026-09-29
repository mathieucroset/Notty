package setup_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/mathieucroset/notty/internal/gitsync"
	"github.com/mathieucroset/notty/internal/gitsync/gittest"
	"github.com/mathieucroset/notty/internal/setup"
)

// identity gives git a user in the isolated global config. Call it after the
// last gittest call that isolates (New, NewEmptyRemote, Isolate).
func identity(t *testing.T) {
	t.Helper()
	gittest.Git(t, "", "config", "--global", "user.name", "Setup Test")
	gittest.Git(t, "", "config", "--global", "user.email", "setup@example.com")
}

// newRemote creates a bare remote whose default branch is branch, holding
// one commit with files.
func newRemote(t *testing.T, branch string, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	remote := filepath.Join(root, "remote.git")
	gittest.Git(t, "", "init", "-q", "--bare", "-b", branch, remote)
	seed, err := gitsync.Init(filepath.Join(root, "seed"), branch)
	if err != nil {
		t.Fatal(err)
	}
	gittest.SetUser(t, seed, "Seed", "seed@example.com")
	for p, c := range files {
		gittest.Write(t, seed, p, c)
	}
	gittest.CommitAll(t, seed, "Seed · seed")
	if err := seed.RemoteAdd(remote); err != nil {
		t.Fatal(err)
	}
	if err := seed.Push(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	return remote
}

func writeFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for p, c := range files {
		full := filepath.Join(dir, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func readFile(t *testing.T, dir, p string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(p)))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// run inspects the vault and remote, plans and executes.
func run(t *testing.T, req setup.Request, gh setup.GH) (conflicted bool, err error) {
	t.Helper()
	ctx := context.Background()
	vs, err := setup.InspectVault(req.Vault)
	if err != nil {
		t.Fatal(err)
	}
	var rs setup.RemoteState
	if req.Choice == setup.ExistingURL {
		if rs, err = setup.InspectRemote(ctx, req.URL); err != nil {
			t.Fatal(err)
		}
	}
	steps, err := setup.Plan(req, vs, rs)
	if err != nil {
		t.Fatal(err)
	}
	return setup.Execute(ctx, req, steps, gh, nil)
}

func mustRun(t *testing.T, req setup.Request, gh setup.GH) {
	t.Helper()
	conflicted, err := run(t, req, gh)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if conflicted {
		t.Fatal("Execute: unexpected conflict")
	}
}

// tracked lists the files in HEAD.
func tracked(t *testing.T, dir string) []string {
	t.Helper()
	out := gittest.Git(t, dir, "ls-tree", "-r", "--name-only", "HEAD")
	return strings.Split(out, "\n")
}

// assertSynced checks the branch, its upstream, a clean tree, and that the
// remote branch is at HEAD.
func assertSynced(t *testing.T, dir, remote, branch string) {
	t.Helper()
	repo := gitsync.Open(dir)
	if got, err := repo.CurrentBranch(); err != nil || got != branch {
		t.Errorf("branch = %q, %v; want %q", got, err, branch)
	}
	if !repo.HasUpstream() {
		t.Error("no upstream set")
	}
	if up := gittest.Git(t, dir, "rev-parse", "--abbrev-ref", "@{upstream}"); up != "origin/"+branch {
		t.Errorf("upstream = %q, want origin/%s", up, branch)
	}
	assertClean(t, dir)
	head := gittest.Git(t, dir, "rev-parse", "HEAD")
	if r := gittest.Git(t, remote, "rev-parse", "refs/heads/"+branch); r != head {
		t.Errorf("remote %s = %s, local HEAD = %s", branch, r, head)
	}
}

func assertClean(t *testing.T, dir string) {
	t.Helper()
	if st := gittest.Git(t, dir, "status", "--porcelain"); st != "" {
		t.Errorf("working tree not clean:\n%s", st)
	}
}

func assertGitignore(t *testing.T, dir string) {
	t.Helper()
	gi := readFile(t, dir, ".gitignore")
	for _, e := range []string{".notty/recovery/", ".notty/lock", ".DS_Store"} {
		if !strings.Contains(gi, e+"\n") {
			t.Errorf(".gitignore lacks %q:\n%s", e, gi)
		}
	}
}

func headSubject(t *testing.T, dir string) string {
	t.Helper()
	return gittest.Git(t, dir, "log", "-1", "--format=%s")
}

func TestExecuteCloneMissingAddsGitignoreCommit(t *testing.T) {
	gittest.Isolate(t)
	remote := newRemote(t, "main", map[string]string{"README.md": "# hi\n", ".gitignore": "*.bak\n"})
	identity(t)
	vault := filepath.Join(t.TempDir(), "deep", "Notes")

	mustRun(t, setup.Request{Vault: vault, Choice: setup.ExistingURL, URL: remote, Host: "box"}, nil)

	if got := readFile(t, vault, "README.md"); got != "# hi\n" {
		t.Errorf("README = %q", got)
	}
	gi := readFile(t, vault, ".gitignore")
	if !strings.HasPrefix(gi, "*.bak\n") {
		t.Errorf(".gitignore lost the remote's content:\n%s", gi)
	}
	assertGitignore(t, vault)
	if s := headSubject(t, vault); s != "Update .gitignore · box" {
		t.Errorf("HEAD subject = %q", s)
	}
	assertClean(t, vault)
	repo := gitsync.Open(vault)
	if b, _ := repo.CurrentBranch(); b != "main" || !repo.HasUpstream() {
		t.Errorf("branch %q upstream %v", b, repo.HasUpstream())
	}
}

func TestExecuteCloneUsesRemoteDefaultBranch(t *testing.T) {
	gittest.Isolate(t)
	remote := newRemote(t, "trunk", map[string]string{"remote.md": "r\n"})
	identity(t)
	// HEAD points to main, which does not exist; the content is on trunk.
	gittest.Git(t, remote, "symbolic-ref", "HEAD", "refs/heads/main")
	vault := filepath.Join(t.TempDir(), "Notes")

	mustRun(t, setup.Request{Vault: vault, Choice: setup.ExistingURL, URL: remote, Host: "box"}, nil)

	repo := gitsync.Open(vault)
	if b, _ := repo.CurrentBranch(); b != "trunk" || !repo.HasUpstream() {
		t.Errorf("branch = %q, upstream %v; want trunk with upstream", b, repo.HasUpstream())
	}
	if got := readFile(t, vault, "remote.md"); got != "r\n" {
		t.Errorf("remote.md = %q", got)
	}
}

func TestExecuteCloneKeepsGitignoreWhenComplete(t *testing.T) {
	gittest.Isolate(t)
	full := ".DS_Store\nThumbs.db\ndesktop.ini\n*.notty-tmp\n.notty/recovery/\n.notty/lock\n"
	remote := newRemote(t, "main", map[string]string{"README.md": "x\n", ".gitignore": full})
	identity(t)
	vault := filepath.Join(t.TempDir(), "Notes")

	mustRun(t, setup.Request{Vault: vault, Choice: setup.ExistingURL, URL: remote, Host: "box"}, nil)

	if s := headSubject(t, vault); s != "Seed · seed" {
		t.Errorf("HEAD subject = %q, want no extra commit", s)
	}
}

func TestExecuteCloneIntoNottyOnlyFolder(t *testing.T) {
	gittest.Isolate(t)
	remote := newRemote(t, "main", map[string]string{
		"a.md":                   "a\n",
		".notty/state.json":      "remote\n",
		".notty/settings.toml":   "same\n",
		".notty/recovery/old.md": "remote old\n",
		".notty/recovery/dup.md": "remote dup\n",
	})
	identity(t)
	vault := filepath.Join(t.TempDir(), "Notes")
	writeFiles(t, vault, map[string]string{
		".notty/recovery/draft.md": "draft\n",
		".notty/recovery/dup.md":   "PRECIOUS\n",
		".notty/state.json":        "local\n",
		".notty/settings.toml":     "same\n",
		".notty/lock":              "1",
	})

	mustRun(t, setup.Request{Vault: vault, Choice: setup.ExistingURL, URL: remote, Host: "box"}, nil)

	want := map[string]string{
		"a.md":                     "a\n",
		".notty/state.json":        "remote\n",
		".notty/settings.toml":     "same\n",
		".notty/recovery/old.md":   "remote old\n",
		".notty/recovery/dup.md":   "remote dup\n",
		".notty/recovery/draft.md": "draft\n",
		".notty/lock":              "1",
	}
	for p, c := range want {
		if got := readFile(t, vault, p); got != c {
			t.Errorf("%s = %q, want %q", p, got, c)
		}
	}
	// Differing local copies are rescued; identical ones are dropped.
	rescued, _ := filepath.Glob(filepath.Join(vault, ".notty", "recovery", "setup-*"))
	if len(rescued) != 1 {
		t.Fatalf("rescue dirs = %v, want one", rescued)
	}
	for p, c := range map[string]string{"state.json": "local\n", "recovery/dup.md": "PRECIOUS\n"} {
		if got := readFile(t, rescued[0], p); got != c {
			t.Errorf("rescued %s = %q, want %q", p, got, c)
		}
	}
	if _, err := os.Stat(filepath.Join(rescued[0], "settings.toml")); err == nil {
		t.Error("identical settings.toml was rescued")
	}
	if entries, _ := os.ReadDir(filepath.Dir(vault)); len(entries) != 1 {
		t.Errorf("leftover entries next to the vault: %v", entries)
	}
}

func TestExecuteCloneFailureRestoresNotty(t *testing.T) {
	gittest.Isolate(t)
	identity(t)
	vault := filepath.Join(t.TempDir(), "Notes")
	writeFiles(t, vault, map[string]string{".notty/recovery/d.md": "d\n"})
	req := setup.Request{Vault: vault, Choice: setup.ExistingURL, URL: filepath.Join(t.TempDir(), "gone.git"), Host: "box"}
	steps, err := setup.Plan(req, setup.Empty, setup.RemoteState{HasHistory: true, DefaultBranch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := setup.Execute(context.Background(), req, steps, nil, nil); !errors.Is(err, gitsync.ErrNetwork) {
		t.Fatalf("err = %v, want ErrNetwork", err)
	}
	if got := readFile(t, vault, ".notty/recovery/d.md"); got != "d\n" {
		t.Errorf("d.md = %q", got)
	}
	if entries, _ := os.ReadDir(filepath.Dir(vault)); len(entries) != 1 {
		t.Errorf("leftover entries next to the vault: %v", entries)
	}
}

func TestExecuteEmptyRemote(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string // nil: missing folder
		want  []string          // tracked files
	}{
		{name: "missing", want: []string{".gitignore"}},
		{name: "notty only", files: map[string]string{".notty/state.json": "{}\n", ".notty/recovery/x.md": "x"},
			want: []string{".gitignore", ".notty/state.json"}},
		{name: "files", files: map[string]string{"a.md": "a\n", "dir/b.md": "b\n", ".DS_Store": "junk", ".notty/recovery/x.md": "x", ".notty/lock": "1"},
			want: []string{".gitignore", "a.md", "dir/b.md"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			remote := gittest.NewEmptyRemote(t)
			identity(t)
			vault := filepath.Join(t.TempDir(), "Notes")
			if tt.files != nil {
				writeFiles(t, vault, tt.files)
			}

			mustRun(t, setup.Request{Vault: vault, Choice: setup.ExistingURL, URL: remote, Host: "box"}, nil)

			assertSynced(t, vault, remote, "main")
			assertGitignore(t, vault)
			if got := tracked(t, vault); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("tracked = %v, want %v", got, tt.want)
			}
			if s := headSubject(t, vault); s != "Initial commit · box" {
				t.Errorf("HEAD subject = %q", s)
			}
		})
	}
}

func TestExecuteFilesWithHistory(t *testing.T) {
	gittest.Isolate(t)
	remote := newRemote(t, "trunk", map[string]string{"README.md": "# remote\n", ".gitignore": "*.bak\n"})
	identity(t)
	vault := filepath.Join(t.TempDir(), "Notes")
	writeFiles(t, vault, map[string]string{"local.md": "mine\n", ".DS_Store": "junk", ".notty/recovery/r.md": "r"})

	mustRun(t, setup.Request{Vault: vault, Choice: setup.ExistingURL, URL: remote, Host: "box"}, nil)

	assertSynced(t, vault, remote, "trunk")
	assertGitignore(t, vault)
	if gi := readFile(t, vault, ".gitignore"); !strings.HasPrefix(gi, "*.bak\n") {
		t.Errorf(".gitignore = %q, want the remote's lines kept", gi)
	}
	if got, want := tracked(t, vault), []string{".gitignore", "README.md", "local.md"}; !reflect.DeepEqual(got, want) {
		t.Errorf("tracked = %v, want %v", got, want)
	}
	if s := headSubject(t, vault); s != "Update .gitignore · box" {
		t.Errorf("HEAD subject = %q", s)
	}
	// The history contains the merge of the unrelated remote commit.
	if n := gittest.Git(t, vault, "rev-list", "--count", "--merges", "HEAD"); n != "1" {
		t.Errorf("merge commits = %s, want 1", n)
	}
}

func TestExecuteFilesWithHistoryConflict(t *testing.T) {
	gittest.Isolate(t)
	remote := newRemote(t, "main", map[string]string{"README.md": "# remote\n"})
	identity(t)
	before := gittest.Git(t, remote, "rev-parse", "refs/heads/main")
	vault := filepath.Join(t.TempDir(), "Notes")
	writeFiles(t, vault, map[string]string{"README.md": "# local\n"})

	conflicted, err := run(t, setup.Request{Vault: vault, Choice: setup.ExistingURL, URL: remote, Host: "box"}, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !conflicted {
		t.Fatal("conflicted = false, want true")
	}
	repo := gitsync.Open(vault)
	if !repo.MergeInProgress() {
		t.Error("merge not in progress")
	}
	if after := gittest.Git(t, remote, "rev-parse", "refs/heads/main"); after != before {
		t.Error("remote changed although the merge conflicted")
	}
	// .gitignore is written but neither staged nor committed.
	assertGitignore(t, vault)
	if st := gittest.Git(t, vault, "status", "--porcelain", "--", ".gitignore"); st != "?? .gitignore" {
		t.Errorf(".gitignore status = %q, want untracked", st)
	}

	// The resolver resolves while the app holds the lock; the syncer's next
	// cycle then commits everything else.
	writeFiles(t, vault, map[string]string{".notty/lock": "123", "README.md": "# merged\n", ".notty/recovery/r.md": "r"})
	if err := repo.Add("README.md"); err != nil {
		t.Fatal(err)
	}
	if err := repo.CommitMerge("Merge · box"); err != nil {
		t.Fatal(err)
	}
	gittest.CommitAll(t, repo, "Update · box")
	if got, want := tracked(t, vault), []string{".gitignore", "README.md"}; !reflect.DeepEqual(got, want) {
		t.Errorf("tracked = %v, want %v", got, want)
	}
}

func TestExecuteConflictedGitignoreLeftToResolver(t *testing.T) {
	gittest.Isolate(t)
	remote := newRemote(t, "main", map[string]string{".gitignore": "*.bak\n"})
	identity(t)
	vault := filepath.Join(t.TempDir(), "Notes")
	writeFiles(t, vault, map[string]string{".gitignore": "*.tmp\n", "a.md": "a\n"})

	conflicted, err := run(t, setup.Request{Vault: vault, Choice: setup.ExistingURL, URL: remote, Host: "box"}, nil)
	if err != nil || !conflicted {
		t.Fatalf("Execute = %v, %v; want a conflict", conflicted, err)
	}
	gi := readFile(t, vault, ".gitignore")
	if !strings.Contains(gi, "<<<<<<<") || strings.Contains(gi, ".notty/lock") {
		t.Errorf("conflicted .gitignore was rewritten:\n%s", gi)
	}
}

// localOnlyVault makes a vault with the LocalOnly flow plus one pending note.
func localOnlyVault(t *testing.T) string {
	t.Helper()
	vault := filepath.Join(t.TempDir(), "Notes")
	writeFiles(t, vault, map[string]string{"first.md": "1\n"})
	mustRun(t, setup.Request{Vault: vault, Choice: setup.LocalOnly, Host: "box"}, nil)
	writeFiles(t, vault, map[string]string{"pending.md": "p\n"})
	return vault
}

func TestExecuteRepoEmptyRemote(t *testing.T) {
	remote := gittest.NewEmptyRemote(t)
	identity(t)
	vault := localOnlyVault(t)

	mustRun(t, setup.Request{Vault: vault, Choice: setup.ExistingURL, URL: remote, Host: "box"}, nil)

	assertSynced(t, vault, remote, "main")
	if s := headSubject(t, vault); s != "Commit pending changes · box" {
		t.Errorf("HEAD subject = %q", s)
	}
	if got, want := tracked(t, vault), []string{".gitignore", "first.md", "pending.md"}; !reflect.DeepEqual(got, want) {
		t.Errorf("tracked = %v, want %v", got, want)
	}
}

func TestExecuteRepoWithHistory(t *testing.T) {
	for _, branch := range []string{"main", "trunk"} {
		t.Run(branch, func(t *testing.T) {
			gittest.Isolate(t)
			remote := newRemote(t, branch, map[string]string{"remote.md": "r\n"})
			identity(t)
			vault := localOnlyVault(t)

			mustRun(t, setup.Request{Vault: vault, Choice: setup.ExistingURL, URL: remote, Host: "box"}, nil)

			assertSynced(t, vault, remote, branch)
			if got, want := tracked(t, vault), []string{".gitignore", "first.md", "pending.md", "remote.md"}; !reflect.DeepEqual(got, want) {
				t.Errorf("tracked = %v, want %v", got, want)
			}
			if branch != "main" {
				if out := gittest.Git(t, vault, "branch", "--list", "main"); out != "" {
					t.Errorf("old branch main still exists: %q", out)
				}
			}
		})
	}
}

func TestExecuteRenameRefusesToClobberBranch(t *testing.T) {
	gittest.Isolate(t)
	remote := newRemote(t, "trunk", map[string]string{"remote.md": "r\n"})
	identity(t)
	vault := localOnlyVault(t)
	gittest.Git(t, vault, "branch", "trunk")

	_, err := run(t, setup.Request{Vault: vault, Choice: setup.ExistingURL, URL: remote, Host: "box"}, nil)
	var se *setup.StepError
	if !errors.As(err, &se) || se.Step.Kind != setup.StepRenameBranch {
		t.Fatalf("err = %v, want a RenameBranch StepError", err)
	}
}

func TestExecuteLocalOnly(t *testing.T) {
	gittest.Isolate(t)
	identity(t)
	vault := filepath.Join(t.TempDir(), "Notes")

	mustRun(t, setup.Request{Vault: vault, Choice: setup.LocalOnly, Host: "box"}, nil)

	repo := gitsync.Open(vault)
	if b, _ := repo.CurrentBranch(); b != "main" {
		t.Errorf("branch = %q", b)
	}
	if repo.HasRemote() {
		t.Error("local-only vault has a remote")
	}
	assertGitignore(t, vault)
	assertClean(t, vault)
	if s := gittest.Git(t, vault, "log", "--format=%s"); s != "Initial commit · box" {
		t.Errorf("log = %q", s)
	}

	// Already a repo whose .gitignore lacks entries: only the .gitignore
	// commit is added.
	if err := os.WriteFile(filepath.Join(vault, ".gitignore"), []byte("*.bak\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gittest.CommitAll(t, repo, "Shrink · box")
	mustRun(t, setup.Request{Vault: vault, Choice: setup.LocalOnly, Host: "box"}, nil)
	if s := headSubject(t, vault); s != "Update .gitignore · box" {
		t.Errorf("HEAD subject = %q", s)
	}
	assertGitignore(t, vault)
	assertClean(t, vault)
}

// fakeGH simulates `gh repo create --source . --remote origin --push`
// against a local bare remote.
type fakeGH struct {
	t         *testing.T
	remote    string
	available bool
	calls     []string
	subject   string // HEAD subject when CreateRepo ran
	dirty     string // status when CreateRepo ran
}

func (f *fakeGH) Available(context.Context) bool { return f.available }

func (f *fakeGH) CreateRepo(ctx context.Context, dir, name string) error {
	f.calls = append(f.calls, dir+" "+name)
	f.subject = headSubject(f.t, dir)
	f.dirty = gittest.Git(f.t, dir, "status", "--porcelain")
	repo := gitsync.Open(dir)
	if err := repo.RemoteAdd(f.remote); err != nil {
		return err
	}
	return repo.Push(ctx, true)
}

func TestExecuteCreateGitHub(t *testing.T) {
	remote := gittest.NewEmptyRemote(t)
	identity(t)
	vault := filepath.Join(t.TempDir(), "Notes")
	writeFiles(t, vault, map[string]string{"a.md": "a\n", ".DS_Store": "junk"})
	gh := &fakeGH{t: t, remote: remote, available: true}

	req := setup.Request{Vault: vault, Choice: setup.CreateGitHub, RepoName: "notes", Host: "box", GHAvailable: gh.Available(context.Background())}
	mustRun(t, req, gh)

	abs, _ := filepath.Abs(vault)
	if want := []string{abs + " notes"}; !reflect.DeepEqual(gh.calls, want) {
		t.Errorf("CreateRepo calls = %q, want %q", gh.calls, want)
	}
	if gh.subject != "Initial commit · box" || gh.dirty != "" {
		t.Errorf("at CreateRepo: HEAD %q, status %q; want the initial commit and a clean tree", gh.subject, gh.dirty)
	}
	assertSynced(t, vault, remote, "main")
	if got, want := tracked(t, vault), []string{".gitignore", "a.md"}; !reflect.DeepEqual(got, want) {
		t.Errorf("tracked = %v, want %v", got, want)
	}
}

func TestExecuteCreateGitHubNeedsGH(t *testing.T) {
	gittest.Isolate(t)
	identity(t)
	vault := filepath.Join(t.TempDir(), "Notes")
	req := setup.Request{Vault: vault, Choice: setup.CreateGitHub, RepoName: "notes", Host: "box", GHAvailable: true}
	steps, err := setup.Plan(req, setup.Missing, setup.RemoteState{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := setup.Execute(context.Background(), req, steps, nil, nil); err == nil {
		t.Error("Execute with a nil GH: want error")
	}
}

func TestExecuteRetryIsIdempotent(t *testing.T) {
	remote := gittest.NewEmptyRemote(t)
	identity(t)
	vault := filepath.Join(t.TempDir(), "Notes")
	req := setup.Request{Vault: vault, Choice: setup.ExistingURL, URL: remote, Host: "box"}
	steps, err := setup.Plan(req, setup.Missing, setup.RemoteState{})
	if err != nil {
		t.Fatal(err)
	}
	for i := range 2 {
		if _, err := setup.Execute(context.Background(), req, steps, nil, nil); err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
	}
	assertSynced(t, vault, remote, "main")

	// A different origin is never replaced silently.
	other := setup.Request{Vault: vault, Choice: setup.ExistingURL, URL: gittest.NewEmptyRemote(t), Host: "box"}
	identity(t)
	steps, err = setup.Plan(other, setup.Repo, setup.RemoteState{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = setup.Execute(context.Background(), other, steps, nil, nil)
	if !errors.Is(err, setup.ErrRemoteExists) {
		t.Errorf("err = %v, want ErrRemoteExists", err)
	}
}

func TestExecuteProgressAndNetworkError(t *testing.T) {
	gittest.Isolate(t)
	identity(t)
	vault := filepath.Join(t.TempDir(), "Notes")
	gone := filepath.Join(t.TempDir(), "gone.git")
	req := setup.Request{Vault: vault, Choice: setup.ExistingURL, URL: gone, Host: "box"}
	steps, err := setup.Plan(req, setup.Missing, setup.RemoteState{})
	if err != nil {
		t.Fatal(err)
	}
	var seen []int
	_, err = setup.Execute(context.Background(), req, steps, nil, func(i int, s setup.Step) {
		if !reflect.DeepEqual(s, steps[i]) {
			t.Errorf("progress(%d) got step %v", i, s)
		}
		seen = append(seen, i)
	})
	if !errors.Is(err, gitsync.ErrNetwork) {
		t.Fatalf("err = %v, want ErrNetwork", err)
	}
	var se *setup.StepError
	if !errors.As(err, &se) || se.Step.Kind != setup.StepPush || se.Index != len(steps)-1 {
		t.Fatalf("err = %#v, want a StepError for the final push", err)
	}
	if want := []int{0, 1, 2, 3, 4, 5}; !slices.Equal(seen, want) {
		t.Errorf("progress indexes = %v, want %v", seen, want)
	}
}

func TestExecuteCanceled(t *testing.T) {
	gittest.Isolate(t)
	identity(t)
	vault := filepath.Join(t.TempDir(), "Notes")
	req := setup.Request{Vault: vault, Choice: setup.LocalOnly, Host: "box"}
	steps, err := setup.Plan(req, setup.Missing, setup.RemoteState{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := setup.Execute(ctx, req, steps, nil, nil); !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
	if _, err := os.Stat(vault); err == nil {
		t.Error("vault created although the context was canceled")
	}
}

func TestJobRunsOnGivenRepoAndLeavesConflictToSyncer(t *testing.T) {
	gittest.Isolate(t)
	remote := newRemote(t, "main", map[string]string{"first.md": "remote\n"})
	identity(t)
	vault := localOnlyVault(t) // first.md differs: add/add conflict
	req := setup.Request{Vault: vault, Choice: setup.ExistingURL, URL: remote, Host: "box"}
	steps, err := setup.Plan(req, setup.Repo, setup.RemoteState{HasHistory: true, DefaultBranch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	repo := gitsync.Open(vault)
	var n int
	job := setup.Job(context.Background(), steps, nil, func(int, setup.Step) { n++ })
	if err := job(repo); err != nil {
		t.Fatalf("job: %v", err)
	}
	if !repo.MergeInProgress() {
		t.Error("merge not in progress after a conflicting job")
	}
	if n != 5 { // commit, remote add, fetch, rename, merge
		t.Errorf("progress calls = %d, want 5", n)
	}
}

func TestNewGH(t *testing.T) {
	type call struct {
		dir  string
		args []string
	}
	var calls []call
	fail := false
	gh := setup.NewGH(func(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
		calls = append(calls, call{dir, append([]string{name}, args...)})
		if fail {
			return []byte("HTTP 422: name already exists\n"), errors.New("exit status 1")
		}
		return nil, nil
	})
	ctx := context.Background()
	if !gh.Available(ctx) {
		t.Error("Available = false")
	}
	if err := gh.CreateRepo(ctx, "/v", "notes"); err != nil {
		t.Fatal(err)
	}
	want := []call{
		{"", []string{"gh", "auth", "status"}},
		{"/v", []string{"gh", "repo", "create", "notes", "--private", "--source", ".", "--remote", "origin", "--push"}},
	}
	if !reflect.DeepEqual(calls, want) {
		t.Errorf("calls = %q, want %q", calls, want)
	}
	fail = true
	if gh.Available(ctx) {
		t.Error("Available = true when gh auth status fails")
	}
	err := gh.CreateRepo(ctx, "/v", "notes")
	if err == nil || !strings.Contains(err.Error(), "name already exists") {
		t.Errorf("err = %v, want gh's output included", err)
	}
}
