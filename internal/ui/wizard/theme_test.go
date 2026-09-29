package wizard

import (
	"context"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/mathieucroset/notty/internal/setup"
	"github.com/mathieucroset/notty/internal/ui/theme"
)

func previews(out []tea.Msg) []string {
	var names []string
	for _, msg := range out {
		if p, ok := msg.(ThemePreviewMsg); ok {
			names = append(names, p.Name)
		}
	}
	return names
}

func TestFirstRunHappyPath(t *testing.T) {
	f := &fakeEnv{vaultState: setup.Missing, gh: true}
	m := start(t, FirstRun, f)
	m, _ = press(t, m, "ctrl+u")
	m = typeText(t, m, "~/journal")
	m, out := press(t, m, "enter", "down", "down", "enter")
	if len(out) != 0 {
		t.Fatalf("setup emitted %v before the theme step", out)
	}
	if m.Stage() != StageTheme {
		t.Fatalf("stage = %v, want theme", m.Stage())
	}
	if len(f.runs) != 1 || f.runs[0].Choice != setup.LocalOnly {
		t.Fatalf("runs = %+v", f.runs)
	}
	names := theme.Names()
	mustContain(t, m, "3 Theme", "Pick a theme", "catppuccin-mocha · current")
	for _, n := range names {
		mustContain(t, m, n)
	}

	m, out = press(t, m, "down")
	if got := previews(out); len(got) != 1 || got[0] != names[1] {
		t.Fatalf("previews = %v, want [%s]", got, names[1])
	}
	m, out = press(t, m, "up")
	if got := previews(out); len(got) != 1 || got[0] != names[0] {
		t.Fatalf("previews = %v, want [%s]", got, names[0])
	}
	// No preview when the cursor cannot move.
	m, out = press(t, m, "up")
	if got := previews(out); len(got) != 0 {
		t.Fatalf("previews at the top = %v", got)
	}
	m, _ = press(t, m, "j", "j")
	_, out = press(t, m, "enter")
	d := doneMsg(t, out)
	want := DoneMsg{Vault: "~/journal", Theme: names[2], Choice: setup.LocalOnly}
	if d != want {
		t.Errorf("DoneMsg = %+v, want %+v", d, want)
	}
}

func TestThemeStepStartsOnCurrentTheme(t *testing.T) {
	f := &fakeEnv{vaultState: setup.Empty}
	m := New(FirstRun, "~/Notes", "nord", f.env(), testStyles()).SetSize(100, 40)
	m, _ = drive(t, m, m.Init())
	m, _ = press(t, m, "enter", "down", "enter")
	if m.Stage() != StageTheme {
		t.Fatalf("stage = %v", m.Stage())
	}
	_, out := press(t, m, "enter")
	if d := doneMsg(t, out); d.Theme != "nord" {
		t.Errorf("theme = %q, want nord (the current one)", d.Theme)
	}
}

func TestThemeSampleCard(t *testing.T) {
	f := &fakeEnv{vaultState: setup.Empty}
	m := start(t, FirstRun, f)
	m, _ = press(t, m, "enter", "down", "enter")
	mustContain(t, m, "Weekend plans", "Farmers market", "Call the bakery", "git pull")
	// The card follows the highlighted theme.
	before := m.View()
	m, _ = press(t, m, "down")
	if m.View() == before {
		t.Error("the view must change when previewing another theme")
	}
}

func TestThemeEscRevertsAndGoesBack(t *testing.T) {
	f := &fakeEnv{vaultState: setup.Empty}
	m := start(t, FirstRun, f)
	m, _ = press(t, m, "enter", "down", "enter", "down")
	m, out := press(t, m, "esc")
	if got := previews(out); len(got) != 1 || got[0] != "catppuccin-mocha" {
		t.Errorf("esc previews = %v, want the current theme back", got)
	}
	if m.Stage() != StageSync {
		t.Errorf("stage = %v, want sync", m.Stage())
	}
	// Setup can run again from there (its steps are idempotent).
	m, _ = press(t, m, "enter")
	if m.Stage() != StageTheme || len(f.runs) != 2 {
		t.Errorf("stage %v, runs %d", m.Stage(), len(f.runs))
	}
}

func TestThemeStepConflictNote(t *testing.T) {
	f := &fakeEnv{vaultState: setup.FilesNoRepo, remote: setup.RemoteState{HasHistory: true, DefaultBranch: "main"}}
	f.run = func(context.Context, setup.Request, []setup.Step, func(int, setup.Step)) (bool, error) {
		return true, nil
	}
	m := start(t, FirstRun, f)
	m, _ = press(t, m, "enter", "enter")
	m = typeText(t, m, "/srv/n.git")
	m, _ = press(t, m, "enter", "enter")
	if m.Stage() != StageTheme {
		t.Fatalf("stage = %v", m.Stage())
	}
	mustContain(t, m, "conflicts")
	_, out := press(t, m, "enter")
	if d := doneMsg(t, out); !d.Conflicted || d.Choice != setup.ExistingURL {
		t.Errorf("DoneMsg = %+v", d)
	}
}

func TestThemeViewFitsSize(t *testing.T) {
	for _, sz := range [][2]int{{120, 40}, {70, 24}, {50, 16}, {30, 10}} {
		f := &fakeEnv{vaultState: setup.Empty}
		m := New(FirstRun, "~/Notes", "rose-pine-dawn", f.env(), testStyles()).SetSize(sz[0], sz[1])
		m, _ = press(t, m, "enter", "down", "enter")
		if m.Stage() != StageTheme {
			t.Fatalf("stage = %v", m.Stage())
		}
		checkFits(t, m, sz[0], sz[1], 0)
	}
}
