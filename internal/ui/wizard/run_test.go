package wizard

import (
	"context"
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/mathieucroset/notty/internal/config"
	"github.com/mathieucroset/notty/internal/gitsync"
	"github.com/mathieucroset/notty/internal/setup"
	"github.com/mathieucroset/notty/internal/ui/keys"
)

func doneMsg(t *testing.T, out []tea.Msg) DoneMsg {
	t.Helper()
	for _, msg := range out {
		if d, ok := msg.(DoneMsg); ok {
			return d
		}
	}
	t.Fatalf("no DoneMsg in %v", out)
	return DoneMsg{}
}

func TestSetupSyncLocalOnlyRunsAndFinishes(t *testing.T) {
	f := &fakeEnv{vaultState: setup.Repo, gh: true}
	m := start(t, SetupSync, f)
	m, out := press(t, m, "down", "down", "enter")
	d := doneMsg(t, out)
	want := DoneMsg{Vault: "~/Notes", Theme: "catppuccin-mocha", Choice: setup.LocalOnly}
	if d != want {
		t.Errorf("DoneMsg = %+v, want %+v", d, want)
	}
	if len(f.runs) != 1 {
		t.Fatalf("runs = %d, want 1", len(f.runs))
	}
	req := f.runs[0]
	if req.Vault != config.ExpandHome("~/Notes") || req.Choice != setup.LocalOnly || req.Host != "testhost" || !req.GHAvailable {
		t.Errorf("request = %+v", req)
	}
	wantSteps, _ := setup.Plan(req, setup.Repo, setup.RemoteState{})
	if fmt.Sprint(f.runSteps[0]) != fmt.Sprint(wantSteps) {
		t.Errorf("steps = %v, want %v", f.runSteps[0], wantSteps)
	}
	_ = m
}

func TestSetupSyncGitHubRun(t *testing.T) {
	f := &fakeEnv{vaultState: setup.Repo, gh: true}
	m := start(t, SetupSync, f)
	m, _ = press(t, m, "enter", "ctrl+u")
	m = typeText(t, m, "me/journal")
	_, out := press(t, m, "enter")
	d := doneMsg(t, out)
	if d.Choice != setup.CreateGitHub {
		t.Errorf("choice = %v", d.Choice)
	}
	req := f.runs[0]
	if req.RepoName != "me/journal" || req.Choice != setup.CreateGitHub {
		t.Errorf("request = %+v", req)
	}
	last := f.runSteps[0][len(f.runSteps[0])-1]
	if last.Kind != setup.StepGHCreate || last.Args[0] != "me/journal" {
		t.Errorf("last step = %+v", last)
	}
}

func TestSetupSyncURLConflicted(t *testing.T) {
	f := &fakeEnv{vaultState: setup.Repo, remote: setup.RemoteState{HasHistory: true, DefaultBranch: "trunk"}}
	f.run = func(_ context.Context, _ setup.Request, steps []setup.Step, progress func(int, setup.Step)) (bool, error) {
		for i, s := range steps {
			progress(i, s)
			if s.Kind == setup.StepMergeUnrelated {
				return true, nil
			}
		}
		return false, nil
	}
	m := start(t, SetupSync, f)
	m, _ = press(t, m, "enter") // gh unavailable: cursor on URL
	m = typeText(t, m, "/srv/notes.git")
	m, out := press(t, m, "enter")
	if len(out) != 0 || len(f.runs) != 0 {
		t.Fatal("the first enter only checks the remote")
	}
	mustContain(t, m, "Remote has history")
	_, out = press(t, m, "enter")
	d := doneMsg(t, out)
	if !d.Conflicted || d.Choice != setup.ExistingURL {
		t.Errorf("DoneMsg = %+v", d)
	}
	req := f.runs[0]
	if req.URL != "/srv/notes.git" {
		t.Errorf("URL = %q", req.URL)
	}
	var merge bool
	for _, s := range f.runSteps[0] {
		if s.Kind == setup.StepMergeUnrelated && s.Args[0] == "origin/trunk" {
			merge = true
		}
	}
	if !merge {
		t.Errorf("plan has no merge of origin/trunk: %v", f.runSteps[0])
	}
}

func TestIdentityFormOnNoIdentity(t *testing.T) {
	f := &fakeEnv{vaultState: setup.Repo, identityErr: fmt.Errorf("setup: %w", setup.ErrNoIdentity)}
	m := start(t, SetupSync, f)
	m, out := press(t, m, "down", "enter")
	if len(out) != 0 {
		t.Fatalf("out = %v", out)
	}
	if m.Stage() != StageIdentity {
		t.Fatalf("stage = %v, want identity", m.Stage())
	}
	if len(f.runs) != 0 {
		t.Fatal("setup must not run without an identity")
	}
	mustContain(t, m, "Git needs a name and email to save your notes' history.", "Name", "Email")
	if m.KeyContext() != keys.WizardInput {
		t.Errorf("key context = %v", m.KeyContext())
	}
	// An empty name is refused.
	m, _ = press(t, m, "enter")
	if m.run.identityField != 0 {
		t.Fatal("enter with an empty name must stay on the name")
	}
	m = typeText(t, m, "Ada q")
	m, _ = press(t, m, "enter")
	m = typeText(t, m, "ada@example.com")
	m, out = press(t, m, "enter")
	if len(f.identitySet) != 1 || f.identitySet[0] != [2]string{"Ada q", "ada@example.com"} {
		t.Fatalf("SetIdentity calls = %v", f.identitySet)
	}
	if len(f.runs) != 1 {
		t.Fatalf("runs = %d, want 1 after the identity is set", len(f.runs))
	}
	doneMsg(t, out)
}

func TestIdentitySkippedWhenPresent(t *testing.T) {
	f := &fakeEnv{vaultState: setup.Repo}
	m := start(t, SetupSync, f)
	_, out := press(t, m, "down", "enter")
	doneMsg(t, out)
	if len(f.identitySet) != 0 {
		t.Error("SetIdentity must not be called when git has an identity")
	}
}

func TestIdentityEscGoesBack(t *testing.T) {
	f := &fakeEnv{vaultState: setup.Repo, identityErr: setup.ErrNoIdentity}
	m := start(t, SetupSync, f)
	m, _ = press(t, m, "down", "enter", "esc")
	if m.Stage() != StageSync {
		t.Errorf("stage = %v, want sync", m.Stage())
	}
}

func TestRunErrorThenRetryReplans(t *testing.T) {
	f := &fakeEnv{vaultState: setup.Empty}
	calls := 0
	f.run = func(_ context.Context, _ setup.Request, steps []setup.Step, progress func(int, setup.Step)) (bool, error) {
		calls++
		for i, s := range steps {
			progress(i, s)
			if calls == 1 && i == 1 {
				return false, &setup.StepError{Index: i, Step: s, Err: gitsync.ErrNetwork}
			}
		}
		return false, nil
	}
	m := start(t, SetupSync, f)
	m, out := press(t, m, "down", "enter")
	if len(out) != 0 {
		t.Fatalf("a failed run emitted %v", out)
	}
	if m.Stage() != StageRun {
		t.Fatalf("stage = %v", m.Stage())
	}
	desc := f.runSteps[0][1].Desc
	mustContain(t, m, desc, "Couldn't reach the remote.", "r retry · esc back", "✓ "+f.runSteps[0][0].Desc, "✗ "+desc)
	inspected := len(f.inspected)

	// The vault changed meanwhile: retry re-inspects and re-plans.
	f.vaultState = setup.Repo
	m, out = press(t, m, "r")
	doneMsg(t, out)
	if len(f.inspected) <= inspected {
		t.Error("retry must re-inspect the vault")
	}
	if len(f.runSteps) != 2 {
		t.Fatalf("runs = %d, want 2", len(f.runSteps))
	}
	req := f.runs[1]
	want, _ := setup.Plan(req, setup.Repo, setup.RemoteState{})
	if fmt.Sprint(f.runSteps[1]) != fmt.Sprint(want) {
		t.Errorf("retry steps = %v, want %v", f.runSteps[1], want)
	}
	_ = m
}

func TestRunErrorEscGoesBackToSync(t *testing.T) {
	f := &fakeEnv{vaultState: setup.Empty}
	f.run = func(context.Context, setup.Request, []setup.Step, func(int, setup.Step)) (bool, error) {
		return false, errBoom
	}
	m := start(t, SetupSync, f)
	m, _ = press(t, m, "down", "enter")
	mustContain(t, m, "boom")
	m, out := press(t, m, "esc")
	if len(out) != 0 || m.Stage() != StageSync || m.sub != subList {
		t.Errorf("esc after an error: out %v stage %v sub %v", out, m.Stage(), m.sub)
	}
}

func TestRunAuthErrorText(t *testing.T) {
	f := &fakeEnv{vaultState: setup.Repo, gh: true}
	f.run = func(_ context.Context, _ setup.Request, steps []setup.Step, progress func(int, setup.Step)) (bool, error) {
		last := len(steps) - 1
		progress(last, steps[last])
		return false, &setup.StepError{Index: last, Step: steps[last], Err: fmt.Errorf("gh: %w", gitsync.ErrAuth)}
	}
	m := start(t, SetupSync, f)
	m, _ = press(t, m, "enter", "enter")
	mustContain(t, m, textAuth)
}

func TestRunPlanErrorIsShown(t *testing.T) {
	f := &fakeEnv{vaultErr: errBoom}
	m := start(t, SetupSync, f)
	m, _ = press(t, m, "down", "enter")
	mustContain(t, m, "boom", "r retry")
	if len(f.runs) != 0 {
		t.Error("nothing must run when planning fails")
	}
}

func TestRunNoIdentityErrorOpensForm(t *testing.T) {
	f := &fakeEnv{vaultState: setup.Repo}
	f.run = func(context.Context, setup.Request, []setup.Step, func(int, setup.Step)) (bool, error) {
		return false, fmt.Errorf("setup: %w", setup.ErrNoIdentity)
	}
	m := start(t, SetupSync, f)
	m, _ = press(t, m, "down", "enter")
	if m.Stage() != StageIdentity {
		t.Errorf("stage = %v, want identity", m.Stage())
	}
}

func TestRunIgnoresKeysWhileRunning(t *testing.T) {
	f := &fakeEnv{vaultState: setup.Repo}
	m := start(t, SetupSync, f)
	m, _ = press(t, m, "down")
	m, cmd := m.Update(key("enter"))
	m, _ = press(t, m, "esc", "r")
	if m.Stage() != StageRun {
		t.Fatalf("keys while running changed the stage to %v", m.Stage())
	}
	_, out := drive(t, m, cmd)
	doneMsg(t, out)
}

// stepOnce runs cmd, which must produce exactly one internal message, and
// feeds it to the model.
func stepOnce(t *testing.T, m Model, cmd tea.Cmd) (Model, tea.Cmd) {
	t.Helper()
	msgs := exec(cmd)
	if len(msgs) != 1 {
		t.Fatalf("stepOnce: got %d messages: %v", len(msgs), msgs)
	}
	return m.Update(msgs[0])
}

func stepLine(t *testing.T, m Model, desc string) string {
	t.Helper()
	for _, l := range strings.Split(plain(m), "\n") {
		if strings.Contains(l, desc) {
			return l
		}
	}
	t.Fatalf("no line with %q:\n%s", desc, plain(m))
	return ""
}

func TestProgressMarkersFollowCallback(t *testing.T) {
	f := &fakeEnv{vaultState: setup.Missing}
	gate := make(chan struct{})
	f.run = func(_ context.Context, _ setup.Request, steps []setup.Step, progress func(int, setup.Step)) (bool, error) {
		for i, s := range steps {
			progress(i, s)
			<-gate
		}
		return false, nil
	}
	m := New(SetupSync, "/v", "nord", f.env(), testStyles()).SetSize(100, 40)
	m, _ = send(t, m, ghMsg{ok: false})
	m, _ = press(t, m, "down")
	m, cmd := m.Update(key("enter"))
	m, cmd = stepOnce(t, m, cmd) // identity checked
	mustContain(t, m, "Getting ready")
	m, cmd = stepOnce(t, m, cmd) // planned
	steps, _ := setup.Plan(setup.Request{Vault: "/v", Choice: setup.LocalOnly, Host: "testhost"}, setup.Missing, setup.RemoteState{})
	if len(steps) < 3 {
		t.Fatalf("expected at least 3 steps, got %v", steps)
	}
	for _, s := range steps {
		if l := stepLine(t, m, s.Desc); !strings.Contains(l, "○") {
			t.Errorf("before progress, %q should be pending: %q", s.Desc, l)
		}
	}
	m, cmd = stepOnce(t, m, cmd) // progress 0
	if l := stepLine(t, m, steps[0].Desc); strings.Contains(l, "○") || strings.Contains(l, "✓") {
		t.Errorf("running step marker: %q", l)
	}
	if l := stepLine(t, m, steps[1].Desc); !strings.Contains(l, "○") {
		t.Errorf("step 1 should be pending: %q", l)
	}
	gate <- struct{}{}
	m, cmd = stepOnce(t, m, cmd) // progress 1
	if l := stepLine(t, m, steps[0].Desc); !strings.Contains(l, "✓") {
		t.Errorf("step 0 should be done: %q", l)
	}
	for range steps[1:] {
		gate <- struct{}{}
		m, cmd = stepOnce(t, m, cmd)
	}
	d := doneMsg(t, exec(cmd))
	if d.Vault != "/v" || d.Theme != "nord" {
		t.Errorf("DoneMsg = %+v", d)
	}
}

func TestRunViewFitsSize(t *testing.T) {
	f := &fakeEnv{vaultState: setup.FilesNoRepo}
	f.run = func(_ context.Context, _ setup.Request, steps []setup.Step, progress func(int, setup.Step)) (bool, error) {
		progress(0, steps[0])
		return false, &setup.StepError{Index: 0, Step: steps[0], Err: fmt.Errorf("%s", strings.Repeat("a long git error message ", 20))}
	}
	for _, sz := range [][2]int{{120, 40}, {70, 24}, {40, 12}} {
		m := New(SetupSync, "~/Notes", "nord", f.env(), testStyles()).SetSize(sz[0], sz[1])
		m, _ = press(t, m, "down", "enter")
		checkFits(t, m, sz[0], sz[1], 0)
		f2 := &fakeEnv{identityErr: setup.ErrNoIdentity}
		m2 := New(SetupSync, "~/Notes", "nord", f2.env(), testStyles()).SetSize(sz[0], sz[1])
		m2, _ = press(t, m2, "down", "enter")
		checkFits(t, m2, sz[0], sz[1], 1)
	}
}
