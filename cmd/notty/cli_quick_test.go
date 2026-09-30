package main

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/mathieucroset/notty/internal/gitsync"
	"github.com/mathieucroset/notty/internal/gitsync/gittest"
	"github.com/mathieucroset/notty/internal/vault"
)

func TestQuickLine(t *testing.T) {
	tests := []struct {
		text, context, want string
	}{
		{"fix the build", "notty", "- (notty) fix the build"},
		{"fix the build", "", "- fix the build"},
		{"i should do this 'n that", "My Project", "- (My Project) i should do this 'n that"},
	}
	for _, tt := range tests {
		if got := quickLine(tt.text, tt.context); got != tt.want {
			t.Errorf("quickLine(%q, %q) = %q, want %q", tt.text, tt.context, got, tt.want)
		}
	}
}

func TestQuickText(t *testing.T) {
	tests := []struct {
		args []string
		want string
	}{
		{[]string{"fix", "the", "build"}, "fix the build"},
		{[]string{"i should do this 'n that"}, "i should do this 'n that"},
		{[]string{"  padded  "}, "padded"},
		{[]string{"two\nlines"}, "two lines"},
		{[]string{"crlf\r\nline", "and\rcr"}, "crlf line and cr"},
		{[]string{"tab\tkept"}, "tab\tkept"},
		{[]string{" ", "\n"}, ""},
		{nil, ""},
	}
	for _, tt := range tests {
		if got := quickText(tt.args); got != tt.want {
			t.Errorf("quickText(%q) = %q, want %q", tt.args, got, tt.want)
		}
	}
}

// quickContext is the name of the working directory, except in the home
// directory, a filesystem root and the vault root, compared after
// resolving symlinks.
func TestQuickContext(t *testing.T) {
	dir := t.TempDir()
	home := filepath.Join(dir, "home")
	root := filepath.Join(dir, "Notes")
	proj := filepath.Join(home, "code", "notty")
	for _, d := range []string{proj, root} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	tests := []struct {
		name, cwd, want string
	}{
		{"project", proj, "notty"},
		{"vault subfolder", filepath.Join(root, "Work"), "Work"},
		{"home", home, ""},
		{"home with trailing separator", home + string(filepath.Separator), ""},
		{"vault root", root, ""},
		{"unknown cwd", "", ""},
	}
	if runtime.GOOS == "windows" {
		tests = append(tests,
			struct{ name, cwd, want string }{"volume root", `C:\`, ""},
			struct{ name, cwd, want string }{"home in another case", strings.ToUpper(home), ""},
		)
	} else {
		tests = append(tests, struct{ name, cwd, want string }{"filesystem root", "/", ""})
	}
	links := map[string]string{"linked-home": home, "linked-vault": root, "linked-proj": proj}
	symlinks := true
	for name, target := range links {
		if err := os.Symlink(target, filepath.Join(dir, name)); err != nil {
			symlinks = false
			break
		}
	}
	if symlinks {
		tests = append(tests,
			struct{ name, cwd, want string }{"symlink to home", filepath.Join(dir, "linked-home"), ""},
			struct{ name, cwd, want string }{"symlink to the vault", filepath.Join(dir, "linked-vault"), ""},
			struct{ name, cwd, want string }{"symlink to a project", filepath.Join(dir, "linked-proj"), "linked-proj"},
		)
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := quickContext(tt.cwd, home, root); got != tt.want {
				t.Errorf("quickContext(%q) = %q, want %q", tt.cwd, got, tt.want)
			}
		})
	}
}

// quickFixture is a fixture with a configured vault directory that is not
// a git repository, a pinned home directory and a project working
// directory.
func quickFixture(t *testing.T) *fixture {
	t.Helper()
	gittest.Isolate(t)
	f := newFixture(t)
	if err := os.MkdirAll(f.vaultDir, 0o755); err != nil {
		t.Fatal(err)
	}
	f.writeConfig(t, "vault = \""+filepath.ToSlash(f.vaultDir)+"\"\n")
	pinHome(t, f)
	return f
}

// pinHome makes <dir>/home the home directory and <dir>/proj the working
// directory.
func pinHome(t *testing.T, f *fixture) {
	t.Helper()
	home := filepath.Join(f.dir, "home")
	f.cwd = filepath.Join(f.dir, "proj")
	for _, d := range []string{home, f.cwd} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	setHome(t, home)
}

func readVault(t *testing.T, f *fixture, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(f.vaultDir, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(b)
}

func TestQuickAppendsToInbox(t *testing.T) {
	tests := []struct {
		name    string
		args    func(f *fixture) []string
		cwd     func(f *fixture) string // nil: the project directory
		wantRel string
		want    string
	}{
		{"words joined", func(*fixture) []string { return []string{"-q", "fix", "the", "build"} }, nil,
			"Inbox.md", "# Inbox\n\n- (proj) fix the build\n"},
		{"long form", func(*fixture) []string { return []string{"--quick", "i should do this 'n that"} }, nil,
			"Inbox.md", "# Inbox\n\n- (proj) i should do this 'n that\n"},
		{"folder before the text", func(*fixture) []string { return []string{"-q", "--folder", "Work", "fix", "it"} }, nil,
			"Work/Inbox.md", "# Inbox\n\n- (proj) fix it\n"},
		{"folder after the text", func(*fixture) []string { return []string{"-q", "fix", "it", "--folder", "Work/Clients"} }, nil,
			"Work/Clients/Inbox.md", "# Inbox\n\n- (proj) fix it\n"},
		{"vault flag after the text", func(f *fixture) []string { return []string{"-q", "fix", "it", "--vault", f.vaultDir} }, nil,
			"Inbox.md", "# Inbox\n\n- (proj) fix it\n"},
		{"global vault flag", func(f *fixture) []string { return []string{"--vault", f.vaultDir, "-q", "fix"} }, nil,
			"Inbox.md", "# Inbox\n\n- (proj) fix\n"},
		{"text after a double dash", func(*fixture) []string { return []string{"-q", "--", "-5", "degrees", "--folder", "x"} }, nil,
			"Inbox.md", "# Inbox\n\n- (proj) -5 degrees --folder x\n"},
		{"subcommand name as text", func(*fixture) []string { return []string{"-q", "sync", "the", "notes"} }, nil,
			"Inbox.md", "# Inbox\n\n- (proj) sync the notes\n"},
		{"newlines flattened", func(*fixture) []string { return []string{"-q", " two\nlines "} }, nil,
			"Inbox.md", "# Inbox\n\n- (proj) two lines\n"},
		{"in the home directory", func(*fixture) []string { return []string{"-q", "call mum"} },
			func(f *fixture) string { return filepath.Join(f.dir, "home") },
			"Inbox.md", "# Inbox\n\n- call mum\n"},
		{"in the vault root", func(*fixture) []string { return []string{"-q", "call mum"} },
			func(f *fixture) string { return f.vaultDir },
			"Inbox.md", "# Inbox\n\n- call mum\n"},
		{"in a symlink to the vault", func(*fixture) []string { return []string{"-q", "call mum"} },
			func(f *fixture) string { return filepath.Join(f.dir, "vault-link") },
			"Inbox.md", "# Inbox\n\n- call mum\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := quickFixture(t)
			if tt.cwd != nil {
				f.cwd = tt.cwd(f)
			}
			if strings.HasSuffix(f.cwd, "vault-link") {
				if err := os.Symlink(f.vaultDir, f.cwd); err != nil {
					t.Skipf("symlinks unsupported: %v", err)
				}
			}
			if code := run(tt.args(f), f.env()); code != 0 {
				t.Fatalf("exit code %d, stderr %q", code, f.stderr.String())
			}
			if got := readVault(t, f, tt.wantRel); got != tt.want {
				t.Errorf("%s = %q, want %q", tt.wantRel, got, tt.want)
			}
			if got, want := f.stdout.String(), "✓ Added to "+tt.wantRel+"\n"; got != want {
				t.Errorf("stdout = %q, want %q", got, want)
			}
			if f.got != nil {
				t.Error("notty -q started the TUI")
			}
			if len(f.started) != 0 {
				t.Errorf("background sync started without a git repository: %q", f.started)
			}
		})
	}
}

func TestQuickAppendsToExistingInbox(t *testing.T) {
	f := quickFixture(t)
	if err := os.WriteFile(filepath.Join(f.vaultDir, "Inbox.md"), []byte("# Inbox\n\n- (x) first"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code := run([]string{"-q", "second"}, f.env()); code != 0 {
		t.Fatalf("exit code %d, stderr %q", code, f.stderr.String())
	}
	if got, want := readVault(t, f, "Inbox.md"), "# Inbox\n\n- (x) first\n- (proj) second\n"; got != want {
		t.Errorf("Inbox.md = %q, want %q", got, want)
	}
}

// --folder names an existing folder in any case, as a case-insensitive
// filesystem would, and the Inbox path is reported in the folder's case.
func TestQuickFolderMatchesExistingCase(t *testing.T) {
	f := quickFixture(t)
	if err := os.MkdirAll(filepath.Join(f.vaultDir, "Work", "Clients"), 0o755); err != nil {
		t.Fatal(err)
	}
	if code := run([]string{"-q", "hi", "--folder", "work/clients"}, f.env()); code != 0 {
		t.Fatalf("exit code %d, stderr %q", code, f.stderr.String())
	}
	if got := readVault(t, f, "Work/Clients/Inbox.md"); got != "# Inbox\n\n- (proj) hi\n" {
		t.Errorf("Work/Clients/Inbox.md = %q", got)
	}
	if got := f.stdout.String(); got != "✓ Added to Work/Clients/Inbox.md\n" {
		t.Errorf("stdout = %q", got)
	}
	entries, err := os.ReadDir(f.vaultDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("vault root holds %d entries, want only Work", len(entries))
	}
}

func TestQuickErrors(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		setup    func(t *testing.T, f *fixture)
		wantCode int
		wantErr  string
	}{
		{"no text", []string{"-q"}, nil, 2, "usage:"},
		{"blank text", []string{"-q", " ", "\n"}, nil, 2, "usage:"},
		{"folder only", []string{"-q", "--folder", "Work"}, nil, 2, "usage:"},
		{"folder without -q", []string{"--folder", "Work"}, nil, 2, "usage:"},
		{"unknown flag after the text", []string{"-q", "hi", "--nope"}, nil, 2, ""},
		{"folder escaping the vault", []string{"-q", "hi", "--folder", "../out"}, nil, 2, "invalid name"},
		{"hidden folder", []string{"-q", "hi", "--folder", ".trash"}, nil, 2, "invalid name"},
		{"reserved folder", []string{"-q", "hi", "--folder", "attachments"}, nil, 2, "reserved"},
		{"folder in the way of a file", []string{"-q", "hi", "--folder", "Work/Sub"}, func(t *testing.T, f *fixture) {
			if err := os.WriteFile(filepath.Join(f.vaultDir, "Work"), []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
		}, 1, "not a folder"},
		{"no config file", []string{"-q", "hi"}, func(t *testing.T, f *fixture) {
			if err := os.Remove(f.configPath); err != nil {
				t.Fatal(err)
			}
		}, 1, errNeedsSetup.Error()},
		{"vault missing", []string{"-q", "hi"}, func(t *testing.T, f *fixture) {
			if err := os.Remove(f.vaultDir); err != nil {
				t.Fatal(err)
			}
		}, 1, errNeedsSetup.Error()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := quickFixture(t)
			if tt.setup != nil {
				tt.setup(t, f)
			}
			if code := run(tt.args, f.env()); code != tt.wantCode {
				t.Fatalf("exit code %d, want %d; stderr %q", code, tt.wantCode, f.stderr.String())
			}
			if !strings.Contains(f.stderr.String(), tt.wantErr) {
				t.Errorf("stderr = %q, want %q", f.stderr.String(), tt.wantErr)
			}
			if _, err := os.Stat(filepath.Join(f.vaultDir, "Inbox.md")); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("Inbox.md written: %v", err)
			}
			if f.stdout.Len() != 0 {
				t.Errorf("stdout = %q", f.stdout.String())
			}
			if len(f.started) != 0 || f.got != nil {
				t.Errorf("started %q, TUI %v", f.started, f.got != nil)
			}
		})
	}
}

// quickRepoFixture is a quick fixture whose vault is the gittest laptop
// clone.
func quickRepoFixture(t *testing.T) (*fixture, *gittest.Env) {
	t.Helper()
	g := gittest.New(t)
	f := newFixture(t)
	f.vaultDir = g.Laptop.Dir
	f.writeConfig(t, "vault = \""+filepath.ToSlash(f.vaultDir)+"\"\n")
	pinHome(t, f)
	return f, g
}

// In a git vault, notty -q starts `notty --vault <root> sync --quiet --wait
// 60s` in the background and returns at once, even while Notty is open
// (the child waits for the lock; the TUI syncs the change anyway).
func TestQuickStartsBackgroundSync(t *testing.T) {
	tests := []struct {
		name     string
		lockHeld bool
		startErr error
		wantOut  string
		wantErr  string
	}{
		{"vault free", false, nil, "✓ Added to Inbox.md (syncing in the background)\n", ""},
		{"notty open", true, nil, "✓ Added to Inbox.md (syncing in the background)\n", ""},
		{"start fails", false, errors.New("no exec"), "✓ Added to Inbox.md\n", "no exec"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, _ := quickRepoFixture(t)
			if tt.lockHeld {
				lock, err := vault.AcquireLock(f.vaultDir, 0)
				if err != nil {
					t.Fatal(err)
				}
				defer func() { _ = lock.Release() }()
			}
			f.startErr = tt.startErr
			if code := run([]string{"-q", "fix", "it"}, f.env()); code != 0 {
				t.Fatalf("exit code %d, stderr %q", code, f.stderr.String())
			}
			want := [][]string{{"--vault", f.vaultDir, "sync", "--quiet", "--wait", "60s"}}
			if !reflect.DeepEqual(f.started, want) {
				t.Errorf("started %q, want %q", f.started, want)
			}
			if got := f.stdout.String(); got != tt.wantOut {
				t.Errorf("stdout = %q, want %q", got, tt.wantOut)
			}
			if !strings.Contains(f.stderr.String(), tt.wantErr) {
				t.Errorf("stderr = %q, want %q", f.stderr.String(), tt.wantErr)
			}
			if got := readVault(t, f, "Inbox.md"); got != "# Inbox\n\n- (proj) fix it\n" {
				t.Errorf("Inbox.md = %q", got)
			}
		})
	}
}

func TestQuickWithoutGitStartsNothing(t *testing.T) {
	f, _ := quickRepoFixture(t)
	f.gitFound = false
	if code := run([]string{"-q", "fix", "it"}, f.env()); code != 0 {
		t.Fatalf("exit code %d, stderr %q", code, f.stderr.String())
	}
	if len(f.started) != 0 {
		t.Errorf("started %q without git", f.started)
	}
	if got := f.stdout.String(); got != "✓ Added to Inbox.md\n" {
		t.Errorf("stdout = %q", got)
	}
}

// During a merge whose conflicts cannot be listed, the Inbox may be one of
// them: the capture is refused rather than risk writing into it.
func TestQuickRefusesWhenConflictsUnknown(t *testing.T) {
	f, g := quickRepoFixture(t)
	head := gittest.Git(t, g.Laptop.Dir, "rev-parse", "HEAD")
	gitDir := filepath.Join(g.Laptop.Dir, ".git")
	if err := os.WriteFile(filepath.Join(gitDir, "MERGE_HEAD"), []byte(head+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "index"), []byte("not an index"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code := run([]string{"-q", "hi"}, f.env()); code != 1 {
		t.Fatalf("exit code %d, want 1; stderr %q", code, f.stderr.String())
	}
	if !strings.Contains(f.stderr.String(), "could not check") {
		t.Errorf("stderr = %q", f.stderr.String())
	}
	if _, err := os.Stat(filepath.Join(f.vaultDir, "Inbox.md")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Inbox.md written: %v", err)
	}
	if len(f.started) != 0 {
		t.Errorf("started %q", f.started)
	}
}

// An Inbox left conflicted by an unfinished merge is not touched: the
// capture is refused until the conflict is resolved in Notty.
func TestQuickRefusesConflictedInbox(t *testing.T) {
	tests := []struct {
		name, folder, conflicted string
		wantCode                 int
	}{
		{"conflicted inbox", "", "Inbox.md", 1},
		{"conflicted folder inbox", "Work", "Work/Inbox.md", 1},
		{"conflicted folder inbox typed in another case", "work", "Work/Inbox.md", 1},
		{"another inbox conflicted", "", "Work/Inbox.md", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f, g := quickRepoFixture(t)
			gittest.Write(t, g.Desktop, tt.conflicted, "# Inbox\n\n- desktop\n")
			gittest.CommitAll(t, g.Desktop, "desktop inbox")
			gittest.Push(t, g.Desktop)
			gittest.Write(t, g.Laptop, tt.conflicted, "# Inbox\n\n- laptop\n")
			gittest.CommitAll(t, g.Laptop, "laptop inbox")
			if err := g.Laptop.Fetch(t.Context()); err != nil {
				t.Fatal(err)
			}
			if err := g.Laptop.Merge("origin/main", false); !errors.Is(err, gitsync.ErrConflict) {
				t.Fatalf("merge: %v, want a conflict", err)
			}
			args := []string{"-q", "hi"}
			if tt.folder != "" {
				args = append(args, "--folder", tt.folder)
			}
			before := readVault(t, f, tt.conflicted)
			code := run(args, f.env())
			if code != tt.wantCode {
				t.Fatalf("exit code %d, want %d; stderr %q", code, tt.wantCode, f.stderr.String())
			}
			if tt.wantCode == 0 {
				return
			}
			want := tt.conflicted + " has an unresolved sync conflict; open Notty to resolve it"
			if !strings.Contains(f.stderr.String(), want) {
				t.Errorf("stderr = %q, want %q", f.stderr.String(), want)
			}
			if got := readVault(t, f, tt.conflicted); got != before {
				t.Errorf("%s changed to %q", tt.conflicted, got)
			}
			if len(f.started) != 0 {
				t.Errorf("started %q", f.started)
			}
		})
	}
}
