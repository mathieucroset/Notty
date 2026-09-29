package setup_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mathieucroset/notty/internal/gitsync"
	"github.com/mathieucroset/notty/internal/gitsync/gittest"
	"github.com/mathieucroset/notty/internal/setup"
)

const url = "git@github.com:me/notes.git"

var (
	noHistory   = setup.RemoteState{}
	withHistory = setup.RemoteState{HasHistory: true, DefaultBranch: "trunk"}
)

func kinds(steps []setup.Step) []setup.StepKind {
	out := make([]setup.StepKind, len(steps))
	for i, s := range steps {
		out[i] = s.Kind
	}
	return out
}

func TestPlanRows(t *testing.T) {
	const (
		Init   = setup.StepInit
		WGI    = setup.StepWriteGitignore
		Commit = setup.StepCommitAll
		Clone  = setup.StepClone
		Remote = setup.StepRemoteAdd
		Fetch  = setup.StepFetch
		Rename = setup.StepRenameBranch
		Merge  = setup.StepMergeUnrelated
		Push   = setup.StepPush
		GH     = setup.StepGHCreate
		EGC    = setup.StepEnsureGitignoreCommit
	)
	gh := setup.Request{Vault: "/v", Choice: setup.CreateGitHub, RepoName: "notes", Host: "box", GHAvailable: true}
	ex := setup.Request{Vault: "/v", Choice: setup.ExistingURL, URL: url, Host: "box"}
	lo := setup.Request{Vault: "/v", Choice: setup.LocalOnly, Host: "box"}

	tests := []struct {
		name string
		req  setup.Request
		vs   setup.VaultState
		rs   setup.RemoteState
		want []setup.StepKind
		// wantArgs optionally checks the Args of the step at a given index.
		wantArgs map[int][]string
	}{
		{name: "gh missing", req: gh, vs: setup.Missing,
			want:     []setup.StepKind{Init, WGI, Commit, EGC, GH},
			wantArgs: map[int][]string{0: {"main"}, 2: {"Initial commit · box"}, 4: {"notes"}}},
		{name: "gh empty", req: gh, vs: setup.Empty, want: []setup.StepKind{Init, WGI, Commit, EGC, GH}},
		{name: "gh files", req: gh, vs: setup.FilesNoRepo, want: []setup.StepKind{Init, WGI, Commit, EGC, GH}},
		{name: "gh repo", req: gh, vs: setup.Repo, want: []setup.StepKind{WGI, Commit, EGC, GH},
			wantArgs: map[int][]string{1: {"Commit pending changes · box"}}},

		{name: "missing + history", req: ex, vs: setup.Missing, rs: withHistory,
			want:     []setup.StepKind{Clone, EGC},
			wantArgs: map[int][]string{0: {url, "trunk"}, 1: {"Update .gitignore · box"}}},
		{name: "empty + history", req: ex, vs: setup.Empty, rs: withHistory, want: []setup.StepKind{Clone, EGC}},
		{name: "missing + empty", req: ex, vs: setup.Missing, rs: noHistory,
			want:     []setup.StepKind{Init, WGI, Commit, Remote, EGC, Push},
			wantArgs: map[int][]string{0: {"main"}, 3: {url}}},
		{name: "empty + empty", req: ex, vs: setup.Empty, rs: noHistory, want: []setup.StepKind{Init, WGI, Commit, Remote, EGC, Push}},
		{name: "files + empty", req: ex, vs: setup.FilesNoRepo, rs: noHistory,
			want:     []setup.StepKind{Init, WGI, Commit, Remote, EGC, Push},
			wantArgs: map[int][]string{0: {"main"}}},
		{name: "files + history", req: ex, vs: setup.FilesNoRepo, rs: withHistory,
			want:     []setup.StepKind{Init, Commit, Remote, Fetch, Merge, EGC, Push},
			wantArgs: map[int][]string{0: {"trunk"}, 4: {"origin/trunk"}}},
		{name: "repo + empty", req: ex, vs: setup.Repo, rs: noHistory,
			want: []setup.StepKind{WGI, Commit, Remote, EGC, Push}},
		{name: "repo + history", req: ex, vs: setup.Repo, rs: withHistory,
			want:     []setup.StepKind{Commit, Remote, Fetch, Rename, Merge, EGC, Push},
			wantArgs: map[int][]string{3: {"trunk"}, 4: {"origin/trunk"}}},

		{name: "local missing", req: lo, vs: setup.Missing, want: []setup.StepKind{Init, WGI, Commit, EGC}},
		{name: "local empty", req: lo, vs: setup.Empty, want: []setup.StepKind{Init, WGI, Commit, EGC}},
		{name: "local files", req: lo, vs: setup.FilesNoRepo, want: []setup.StepKind{Init, WGI, Commit, EGC}},
		{name: "local repo", req: lo, vs: setup.Repo, want: []setup.StepKind{EGC}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			steps, err := setup.Plan(tt.req, tt.vs, tt.rs)
			if err != nil {
				t.Fatalf("Plan: %v", err)
			}
			if got := kinds(steps); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("kinds = %v, want %v", got, tt.want)
			}
			for i, s := range steps {
				if s.Desc == "" {
					t.Errorf("step %d (%v) has no description", i, s.Kind)
				}
			}
			for i, want := range tt.wantArgs {
				if got := steps[i].Args; !reflect.DeepEqual(got, want) {
					t.Errorf("step %d args = %q, want %q", i, got, want)
				}
			}
		})
	}
}

func TestPlanDescriptions(t *testing.T) {
	steps, err := setup.Plan(setup.Request{Vault: "/v", Choice: setup.CreateGitHub, RepoName: "notes", Host: "box", GHAvailable: true}, setup.Empty, noHistory)
	if err != nil {
		t.Fatal(err)
	}
	if got := steps[len(steps)-1].Desc; got != "Creating private GitHub repo notes" {
		t.Errorf("gh desc = %q", got)
	}
}

func TestPlanErrors(t *testing.T) {
	tests := []struct {
		name string
		req  setup.Request
		vs   setup.VaultState
		rs   setup.RemoteState
		want error
	}{
		{name: "gh unavailable", req: setup.Request{Vault: "/v", Choice: setup.CreateGitHub, RepoName: "notes"}, vs: setup.Empty, want: setup.ErrGHUnavailable},
		{name: "no repo name", req: setup.Request{Vault: "/v", Choice: setup.CreateGitHub, GHAvailable: true}, vs: setup.Empty},
		{name: "bad repo name", req: setup.Request{Vault: "/v", Choice: setup.CreateGitHub, RepoName: "--help", GHAvailable: true}, vs: setup.Empty},
		{name: "spaced repo name", req: setup.Request{Vault: "/v", Choice: setup.CreateGitHub, RepoName: "my notes", GHAvailable: true}, vs: setup.Empty},
		{name: "no url", req: setup.Request{Vault: "/v", Choice: setup.ExistingURL}, vs: setup.Empty},
		{name: "dash url", req: setup.Request{Vault: "/v", Choice: setup.ExistingURL, URL: "-u"}, vs: setup.Empty},
		{name: "history without branch", req: setup.Request{Vault: "/v", Choice: setup.ExistingURL, URL: url}, vs: setup.Empty, rs: setup.RemoteState{HasHistory: true}},
		{name: "no vault", req: setup.Request{Choice: setup.LocalOnly}, vs: setup.Empty},
		{name: "bad choice", req: setup.Request{Vault: "/v", Choice: setup.Choice(99)}, vs: setup.Empty},
		{name: "bad state", req: setup.Request{Vault: "/v", Choice: setup.LocalOnly}, vs: setup.VaultState(99)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			steps, err := setup.Plan(tt.req, tt.vs, tt.rs)
			if err == nil {
				t.Fatalf("Plan = %v, want error", kinds(steps))
			}
			if tt.want != nil && !errors.Is(err, tt.want) {
				t.Errorf("err = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestPlanDefaultsHost(t *testing.T) {
	steps, err := setup.Plan(setup.Request{Vault: "/v", Choice: setup.LocalOnly}, setup.Empty, noHistory)
	if err != nil {
		t.Fatal(err)
	}
	msg := steps[2].Args[0]
	if !strings.HasPrefix(msg, "Initial commit · ") || strings.HasSuffix(msg, "· ") {
		t.Errorf("commit message = %q, want a host suffix", msg)
	}
}

func TestInspectVault(t *testing.T) {
	gittest.Isolate(t)
	root := t.TempDir()
	mk := func(name string, files ...string) string {
		dir := filepath.Join(root, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		for _, f := range files {
			p := filepath.Join(dir, filepath.FromSlash(f))
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		return dir
	}
	repo := mk("repo")
	if _, err := gitsync.Init(repo, "main"); err != nil {
		t.Fatal(err)
	}
	nested := mk("repo/sub", "a.md") // inside another repo: not a repo of its own
	file := filepath.Join(root, "file")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		path string
		want setup.VaultState
	}{
		{"missing", filepath.Join(root, "nope"), setup.Missing},
		{"empty", mk("empty"), setup.Empty},
		{"notty only", mk("notty", ".notty/recovery/x.md", ".notty/state.json"), setup.Empty},
		{"empty notty dir", mk("notty2", ".notty/.keep"), setup.Empty},
		{"files", mk("files", "a.md"), setup.FilesNoRepo},
		{"hidden file", mk("hidden", ".DS_Store"), setup.FilesNoRepo},
		{"notty and files", mk("both", ".notty/state.json", "b.md"), setup.FilesNoRepo},
		{"repo", repo, setup.Repo},
		{"nested", nested, setup.FilesNoRepo},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := setup.InspectVault(tt.path)
			if err != nil {
				t.Fatalf("InspectVault: %v", err)
			}
			if got != tt.want {
				t.Errorf("state = %v, want %v", got, tt.want)
			}
		})
	}
	if _, err := setup.InspectVault(file); err == nil {
		t.Error("InspectVault on a file: want error")
	}
}

func TestInspectRemote(t *testing.T) {
	empty := gittest.NewEmptyRemote(t)
	env := gittest.New(t)
	ctx := context.Background()

	rs, err := setup.InspectRemote(ctx, empty)
	if err != nil {
		t.Fatal(err)
	}
	if rs != (setup.RemoteState{}) {
		t.Errorf("empty remote = %+v", rs)
	}
	rs, err = setup.InspectRemote(ctx, env.Remote)
	if err != nil {
		t.Fatal(err)
	}
	if rs != (setup.RemoteState{HasHistory: true, DefaultBranch: "main"}) {
		t.Errorf("remote with history = %+v", rs)
	}
	if _, err := setup.InspectRemote(ctx, filepath.Join(t.TempDir(), "gone.git")); err == nil {
		t.Error("missing remote: want error")
	}
}
