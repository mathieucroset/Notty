package wizard

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/mathieucroset/notty/internal/gitsync"
	"github.com/mathieucroset/notty/internal/setup"
	"github.com/mathieucroset/notty/internal/ui/keys"
)

func TestVaultStepPrefillAndHint(t *testing.T) {
	f := &fakeEnv{vaultState: setup.Empty}
	m := start(t, FirstRun, f)
	if m.VaultInput() != "~/Notes" {
		t.Fatalf("vault input = %q, want ~/Notes", m.VaultInput())
	}
	mustContain(t, m, "1 Vault", "2 Sync", "3 Theme", "~/Notes", "Empty folder — a new vault will be created")
	if len(f.inspected) == 0 || strings.HasPrefix(f.inspected[len(f.inspected)-1], "~") {
		t.Errorf("Inspect should receive the expanded path, got %v", f.inspected)
	}
	if m.KeyContext() != keys.WizardInput {
		t.Errorf("key context = %v, want WizardInput", m.KeyContext())
	}
}

func TestVaultHints(t *testing.T) {
	cases := []struct {
		state setup.VaultState
		notes int
		want  string
	}{
		{setup.Missing, 0, "a new vault will be created"},
		{setup.Empty, 0, "Empty folder — a new vault will be created"},
		{setup.FilesNoRepo, 12, "Folder has 12 notes — they'll be kept"},
		{setup.FilesNoRepo, 1, "Folder has 1 note — it'll be kept"},
		{setup.Repo, 0, "Existing git repo"},
	}
	for _, c := range cases {
		f := &fakeEnv{vaultState: c.state, notes: c.notes}
		m := start(t, FirstRun, f)
		mustContain(t, m, c.want)
	}
}

func TestVaultHintFollowsTyping(t *testing.T) {
	f := &fakeEnv{vaultState: setup.Empty}
	m := start(t, FirstRun, f)
	f.vaultState = setup.Repo
	m, _ = press(t, m, "ctrl+u")
	m = typeText(t, m, "/tmp/other")
	if m.VaultInput() != "/tmp/other" {
		t.Fatalf("vault input = %q", m.VaultInput())
	}
	mustContain(t, m, "Existing git repo")
	if got := f.inspected[len(f.inspected)-1]; got != "/tmp/other" {
		t.Errorf("last inspected = %q", got)
	}
}

func TestVaultInspectError(t *testing.T) {
	f := &fakeEnv{vaultErr: errBoom}
	m := start(t, FirstRun, f)
	mustContain(t, m, "boom")
	m, _ = press(t, m, "enter")
	if m.Stage() != StageVault {
		t.Errorf("enter with an inspect error should stay on the vault step, got %v", m.Stage())
	}
}

func TestVaultEmptyPathRejected(t *testing.T) {
	f := &fakeEnv{vaultState: setup.Empty}
	m := start(t, FirstRun, f)
	m, _ = press(t, m, "ctrl+u", "enter")
	if m.Stage() != StageVault {
		t.Fatalf("step = %v, want vault", m.Stage())
	}
	mustContain(t, m, "Enter a folder")
}

func TestTypingQInsertsQ(t *testing.T) {
	f := &fakeEnv{vaultState: setup.Empty, gh: true}
	m := start(t, FirstRun, f)
	m, out := press(t, m, "ctrl+u", "q")
	if len(out) != 0 {
		t.Fatalf("q emitted %v", out)
	}
	if m.VaultInput() != "q" {
		t.Fatalf("vault input = %q, want q", m.VaultInput())
	}
	// URL input.
	m, _ = press(t, m, "enter", "down", "enter")
	m = typeText(t, m, "qq")
	if m.urlInput.Value() != "qq" {
		t.Errorf("url input = %q, want qq", m.urlInput.Value())
	}
	// Repo name input.
	m, _ = press(t, m, "esc", "up", "enter", "ctrl+u", "q")
	if m.repoInput.Value() != "q" {
		t.Errorf("repo input = %q, want q", m.repoInput.Value())
	}
}

func TestFirstRunEscOnVaultAsksToQuit(t *testing.T) {
	f := &fakeEnv{vaultState: setup.Empty}
	m := start(t, FirstRun, f)
	m, out := press(t, m, "esc")
	if len(out) != 0 {
		t.Fatalf("esc emitted %v", out)
	}
	mustContain(t, m, "Quit Notty? y/n")
	// n dismisses.
	m, out = press(t, m, "n")
	if len(out) != 0 {
		t.Fatalf("n emitted %v", out)
	}
	mustNotContain(t, m, "Quit Notty?")
	if m.VaultInput() != "~/Notes" {
		t.Errorf("n must not be typed into the input: %q", m.VaultInput())
	}
	// esc dismisses too.
	m, _ = press(t, m, "esc", "esc")
	mustNotContain(t, m, "Quit Notty?")
	// y quits.
	_, out = press(t, m, "esc", "y")
	if len(out) != 1 {
		t.Fatalf("y emitted %v, want QuitMsg", out)
	}
	if _, ok := out[0].(QuitMsg); !ok {
		t.Fatalf("y emitted %T, want QuitMsg", out[0])
	}
}

func TestSyncStepChoices(t *testing.T) {
	f := &fakeEnv{vaultState: setup.Empty, gh: true}
	m := start(t, FirstRun, f)
	m, _ = press(t, m, "enter")
	if m.Stage() != StageSync {
		t.Fatalf("step = %v, want sync", m.Stage())
	}
	if m.KeyContext() != keys.Wizard {
		t.Errorf("key context = %v, want Wizard", m.KeyContext())
	}
	mustContain(t, m, "Create a private GitHub repo", "Use an existing repo URL", "Local only (set up sync later)")
	mustNotContain(t, m, "install and log in to gh")
	if m.Choice() != setup.CreateGitHub {
		t.Errorf("default choice = %v, want CreateGitHub", m.Choice())
	}
	m, _ = press(t, m, "down", "down", "down")
	if m.Choice() != setup.LocalOnly {
		t.Errorf("choice = %v, want LocalOnly (clamped)", m.Choice())
	}
	m, _ = press(t, m, "k")
	if m.Choice() != setup.ExistingURL {
		t.Errorf("choice = %v, want ExistingURL", m.Choice())
	}
	// esc goes back to the vault step, keeping the path.
	m, _ = press(t, m, "esc")
	if m.Stage() != StageVault || m.VaultInput() != "~/Notes" {
		t.Errorf("esc: step %v, vault %q", m.Stage(), m.VaultInput())
	}
}

func TestGitHubDisabledWithoutGH(t *testing.T) {
	f := &fakeEnv{vaultState: setup.Empty, gh: false}
	m := start(t, FirstRun, f)
	m, _ = press(t, m, "enter")
	mustContain(t, m, "install and log in to gh to enable")
	if m.Choice() == setup.CreateGitHub {
		t.Fatal("cursor must not start on the disabled GitHub option")
	}
	m, _ = press(t, m, "up", "up")
	if m.Choice() == setup.CreateGitHub {
		t.Fatal("cursor must skip the disabled GitHub option")
	}
	// Even if forced onto it, enter does nothing.
	m.choice = setup.CreateGitHub
	m, _ = press(t, m, "enter")
	if m.sub != subList || m.Stage() != StageSync {
		t.Errorf("enter on the disabled option: sub %v step %v", m.sub, m.Stage())
	}
}

func TestGitHubRepoNameInput(t *testing.T) {
	f := &fakeEnv{vaultState: setup.Empty, gh: true}
	m := start(t, FirstRun, f)
	m, _ = press(t, m, "enter", "enter")
	if m.sub != subRepo {
		t.Fatalf("sub = %v, want repo input", m.sub)
	}
	if m.repoInput.Value() != "notes" {
		t.Errorf("repo default = %q, want notes", m.repoInput.Value())
	}
	if m.KeyContext() != keys.WizardInput {
		t.Errorf("key context = %v", m.KeyContext())
	}
	m, _ = press(t, m, "ctrl+u")
	m = typeText(t, m, "bad name")
	m, _ = press(t, m, "enter")
	if m.sub != subRepo {
		t.Fatal("an invalid repo name must not continue")
	}
	mustContain(t, m, "letters, numbers")
	m, _ = press(t, m, "esc")
	if m.sub != subList || m.Stage() != StageSync {
		t.Errorf("esc from repo input: sub %v step %v", m.sub, m.Stage())
	}
}

func TestURLPathInspectsRemote(t *testing.T) {
	cases := []struct {
		name  string
		state setup.RemoteState
		err   error
		want  string
	}{
		{"empty", setup.RemoteState{}, nil, "Remote is empty — your notes will be pushed"},
		{"history", setup.RemoteState{HasHistory: true, DefaultBranch: "main"}, nil, "Remote has history — it will be merged into your vault"},
		{"auth", setup.RemoteState{}, gitsync.ErrAuth, "Couldn't access the repo. Check `ssh -T git@github.com` or `gh auth status`."},
		{"network", setup.RemoteState{}, gitsync.ErrNetwork, "Couldn't reach the remote."},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := &fakeEnv{vaultState: setup.Empty, remote: c.state, remoteErr: c.err}
			m := start(t, FirstRun, f)
			m, _ = press(t, m, "enter", "enter") // gh unavailable: cursor on URL
			if m.sub != subURL {
				t.Fatalf("sub = %v, want url input", m.sub)
			}
			m = typeText(t, m, "git@example.com:me/notes.git")
			m, _ = press(t, m, "enter")
			if len(f.remotes) != 1 || f.remotes[0] != "git@example.com:me/notes.git" {
				t.Fatalf("InspectRemote calls = %v", f.remotes)
			}
			mustContain(t, m, c.want)
			if m.sub != subURL {
				t.Errorf("the remote check must not leave the URL input")
			}
		})
	}
}

func TestURLEditClearsRemoteResult(t *testing.T) {
	f := &fakeEnv{vaultState: setup.Empty}
	m := start(t, FirstRun, f)
	m, _ = press(t, m, "enter", "enter")
	m = typeText(t, m, "/srv/a.git")
	m, _ = press(t, m, "enter")
	mustContain(t, m, "Remote is empty")
	m = typeText(t, m, "x")
	mustNotContain(t, m, "Remote is empty")
}

func TestURLEmptyRejected(t *testing.T) {
	f := &fakeEnv{vaultState: setup.Empty}
	m := start(t, FirstRun, f)
	m, _ = press(t, m, "enter", "enter", "enter")
	if len(f.remotes) != 0 {
		t.Fatal("an empty URL must not be inspected")
	}
	mustContain(t, m, "Enter a repository URL")
}

func TestSetupSyncStartsAtSyncAndCancels(t *testing.T) {
	f := &fakeEnv{vaultState: setup.Repo, gh: true}
	m := start(t, SetupSync, f)
	if m.Stage() != StageSync {
		t.Fatalf("SetupSync starts at %v, want sync", m.Stage())
	}
	mustContain(t, m, "~/Notes", "Create a private GitHub repo")
	mustNotContain(t, m, "3 Theme")
	// esc inside an input goes back to the list first.
	m, out := press(t, m, "enter", "esc")
	if len(out) != 0 || m.sub != subList {
		t.Fatalf("esc from input: out %v sub %v", out, m.sub)
	}
	m, out = press(t, m, "esc")
	if len(out) != 1 {
		t.Fatalf("esc on the first step emitted %v, want CancelMsg", out)
	}
	if _, ok := out[0].(CancelMsg); !ok {
		t.Fatalf("got %T, want CancelMsg", out[0])
	}
}

func TestViewFitsSize(t *testing.T) {
	sizes := [][2]int{{120, 40}, {70, 24}, {40, 12}, {20, 6}}
	f := &fakeEnv{vaultState: setup.FilesNoRepo, notes: 3, gh: false}
	for _, sz := range sizes {
		m := New(FirstRun, "~/a/very/long/path/that/goes/on/and/on/and/on/Notes", "nord", f.env(), testStyles()).SetSize(sz[0], sz[1])
		m, _ = drive(t, m, m.Init())
		screens := []Model{m}
		m2, _ := press(t, m, "enter")
		screens = append(screens, m2)
		m3, _ := press(t, m2, "enter")
		screens = append(screens, m3)
		for i, s := range screens {
			checkFits(t, s, sz[0], sz[1], i)
		}
	}
}

func TestLateGHResultKeepsMovedCursor(t *testing.T) {
	f := &fakeEnv{vaultState: setup.Empty}
	m := New(FirstRun, "~/Notes", "nord", f.env(), testStyles()).SetSize(100, 40)
	m, _ = press(t, m, "enter")
	mustContain(t, m, "checking for gh")
	m, _ = press(t, m, "down")
	m, _ = send(t, m, ghMsg{ok: true})
	if m.Choice() != setup.LocalOnly {
		t.Errorf("choice = %v, want LocalOnly kept", m.Choice())
	}
	m2 := New(FirstRun, "~/Notes", "nord", f.env(), testStyles()).SetSize(100, 40)
	m2, _ = send(t, m2, ghMsg{ok: true})
	if m2.Choice() != setup.CreateGitHub {
		t.Errorf("choice = %v, want CreateGitHub once gh is found", m2.Choice())
	}
}

func checkFits(t *testing.T, m Model, w, h, i int) {
	t.Helper()
	v := m.View()
	lines := strings.Split(v, "\n")
	if len(lines) > h {
		t.Errorf("%dx%d screen %d: %d lines", w, h, i, len(lines))
	}
	for _, l := range lines {
		if lw := ansi.StringWidth(l); lw > w {
			t.Errorf("%dx%d screen %d: line width %d: %q", w, h, i, lw, ansi.Strip(l))
		}
	}
}
