package wizard

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/mathieucroset/notty/internal/setup"
	"github.com/mathieucroset/notty/internal/ui/theme"
)

func previewPalettes(out []tea.Msg) []theme.Palette {
	var ps []theme.Palette
	for _, msg := range out {
		if p, ok := msg.(ThemePreviewMsg); ok {
			ps = append(ps, p.Palette)
		}
	}
	return ps
}

func previews(out []tea.Msg) []string {
	var names []string
	for _, msg := range out {
		if p, ok := msg.(ThemePreviewMsg); ok {
			names = append(names, p.Palette.Name)
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
	want := DoneMsg{Vault: "~/journal", Palette: builtin(names[2]), Choice: setup.LocalOnly}
	if d != want {
		t.Errorf("DoneMsg = %+v, want %+v", d, want)
	}
}

func TestThemeStepStartsOnCurrentTheme(t *testing.T) {
	tests := []struct {
		name    string
		current string // a built-in, or "good" (a user theme)
	}{
		{"built-in", "nord"},
		// The current user theme is not read again: deleting its file
		// before enter changes nothing.
		{"user theme, file deleted meanwhile", "good"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cat, dir := userCatalog(t)
			cur, err := cat.Resolve(tt.current)
			if err != nil {
				t.Fatal(err)
			}
			f := &fakeEnv{vaultState: setup.Empty}
			m := New(FirstRun, "~/Notes", cur, cat, f.env(), testStyles()).SetSize(100, 40)
			m, _ = drive(t, m, m.Init())
			m, _ = press(t, m, "enter", "down", "enter")
			if m.Stage() != StageTheme {
				t.Fatalf("stage = %v", m.Stage())
			}
			mustContain(t, m, tt.current+" · current")
			if err := os.Remove(filepath.Join(dir, "good.toml")); err != nil {
				t.Fatal(err)
			}
			_, out := press(t, m, "enter")
			if d := doneMsg(t, out); d.Palette.Key() != cur.Key() {
				t.Errorf("theme = %q, want %q (the current one)", d.Palette.Key(), cur.Key())
			}
		})
	}
}

// goodTheme is a valid user theme file.
const goodTheme = `base = "#141318"
surface = "#201f24"
overlay = "#36343a"
text = "#e6e1e9"
subtext = "#cac4cf"
muted = "#948f99"
accent = "#cfbcff"
accent2 = "#f2b7c2"
error = "#ffb4ab"
`

// userCatalog is a catalog with two user themes: good, and bad (which
// does not load).
func userCatalog(t *testing.T) (theme.Catalog, string) {
	t.Helper()
	dir := t.TempDir()
	for name, content := range map[string]string{"good": goodTheme, "bad": "x"} {
		if err := os.WriteFile(filepath.Join(dir, name+theme.ThemeExt), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return theme.Catalog{Dir: dir}, dir
}

// themeStep opens the theme step of a first-run wizard on cat, current
// theme catppuccin-mocha, with the cursor moved to the theme target.
func themeStep(t *testing.T, cat theme.Catalog, target string) (Model, []tea.Msg) {
	t.Helper()
	f := &fakeEnv{vaultState: setup.Empty}
	m := New(FirstRun, "~/Notes", builtin("catppuccin-mocha"), cat, f.env(), testStyles()).SetSize(120, 40)
	m, _ = drive(t, m, m.Init())
	m, _ = press(t, m, "enter", "down", "enter")
	if m.Stage() != StageTheme {
		t.Fatalf("stage = %v", m.Stage())
	}
	names := cat.Names()
	i := slices.Index(names, target)
	if i < 0 {
		t.Fatalf("%q not in %v", target, names)
	}
	var out []tea.Msg
	for range i - slices.Index(names, "catppuccin-mocha") {
		var o []tea.Msg
		m, o = press(t, m, "down")
		out = o
	}
	return m, out // the messages of the last move
}

func TestThemeStepUserThemes(t *testing.T) {
	tests := []struct {
		name   string
		target string
		ok     bool
	}{
		{"working user theme", "good", true},
		{"broken user theme", "bad", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cat, _ := userCatalog(t)
			m, out := themeStep(t, cat, tt.target)
			mustContain(t, m, "bad", "good")
			got := previews(out)
			if !tt.ok {
				if len(got) != 0 {
					t.Errorf("previews = %v, want none for a broken theme", got)
				}
				mustContain(t, m, "bad.toml")
				m2, out := press(t, m, "enter")
				if len(out) != 0 || m2.Stage() != StageTheme {
					t.Errorf("enter on a broken theme: stage %v, messages %v", m2.Stage(), out)
				}
				// Esc still goes back, on the current theme.
				_, out = press(t, m, "esc")
				if p := previewPalettes(out); len(p) != 1 || p[0].Key() != "catppuccin-mocha" {
					t.Errorf("esc previews %v, want catppuccin-mocha", p)
				}
				return
			}
			want, err := cat.Resolve("good")
			if err != nil {
				t.Fatal(err)
			}
			if p := previewPalettes(out); len(p) != 1 || p[0].Name != "good" || p[0].Key() != want.Key() {
				t.Errorf("previews = %v, want the good palette", got)
			}
			if strings.Contains(plain(m), "bad.toml") {
				t.Errorf("error shown on a working theme:\n%s", plain(m))
			}
			_, out = press(t, m, "enter")
			if d := doneMsg(t, out); d.Palette.Name != "good" || d.Palette.Key() != want.Key() {
				t.Errorf("DoneMsg palette = %q, want %q", d.Palette.Key(), want.Key())
			}
		})
	}
}

// TestThemeStepErrorClearsOnMove: moving from a broken theme to a working
// one clears the error and allows enter again.
func TestThemeStepErrorClearsOnMove(t *testing.T) {
	cat, _ := userCatalog(t)
	m, _ := themeStep(t, cat, "bad")
	mustContain(t, m, "bad.toml")
	m, out := press(t, m, "down") // good
	if p := previewPalettes(out); len(p) != 1 || p[0].Name != "good" {
		t.Errorf("previews = %v, want good", p)
	}
	if strings.Contains(plain(m), "bad.toml") {
		t.Errorf("error still shown:\n%s", plain(m))
	}
	_, out = press(t, m, "enter")
	if d := doneMsg(t, out); d.Palette.Name != "good" {
		t.Errorf("DoneMsg theme = %q, want good", d.Palette.Name)
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
		m := New(FirstRun, "~/Notes", builtin("rose-pine-dawn"), theme.Catalog{}, f.env(), testStyles()).SetSize(sz[0], sz[1])
		m, _ = press(t, m, "enter", "down", "enter")
		if m.Stage() != StageTheme {
			t.Fatalf("stage = %v", m.Stage())
		}
		checkFits(t, m, sz[0], sz[1], 0)
	}
}
