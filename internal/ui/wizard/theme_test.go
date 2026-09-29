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

// TestThemePreviewSeq: every preview, including esc's return to the
// current theme, carries a larger Seq than the one before.
func TestThemePreviewSeq(t *testing.T) {
	f := &fakeEnv{vaultState: setup.Empty}
	m := start(t, FirstRun, f)
	m, _ = press(t, m, "enter", "down", "enter")
	var seqs []uint64
	for _, k := range []string{"down", "down", "up", "esc"} {
		var out []tea.Msg
		m, out = press(t, m, k)
		var got []uint64
		for _, msg := range out {
			if p, ok := msg.(ThemePreviewMsg); ok {
				got = append(got, p.Seq)
			}
		}
		if len(got) != 1 {
			t.Fatalf("%s emitted %d previews, want 1", k, len(got))
		}
		seqs = append(seqs, got[0])
	}
	for i := 1; i < len(seqs); i++ {
		if seqs[i] <= seqs[i-1] {
			t.Errorf("preview seqs %v are not increasing", seqs)
		}
	}
}

// TestThemeStepUnlistedCurrent: when the current theme is not listed (its
// file is gone), the cursor starts on the first theme and the app is asked
// to show it, so the screen matches the cursor.
func TestThemeStepUnlistedCurrent(t *testing.T) {
	cat, dir := userCatalog(t)
	cur, err := cat.Resolve("good")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "good.toml")); err != nil {
		t.Fatal(err)
	}
	f := &fakeEnv{vaultState: setup.Empty}
	m := New(FirstRun, "~/Notes", cur, cat, f.env(), testStyles()).SetSize(100, 40)
	m, _ = drive(t, m, m.Init())
	m, out := press(t, m, "enter", "down", "enter")
	if m.Stage() != StageTheme {
		t.Fatalf("stage = %v", m.Stage())
	}
	first := cat.Names()[0]
	if p := previewPalettes(out); len(p) != 1 || p[0].Name != first || p[0].Key() == cur.Key() {
		t.Errorf("previews on entering the step = %v, want %s", p, first)
	}
	_, out = press(t, m, "enter")
	if d := doneMsg(t, out); d.Palette.Name != first {
		t.Errorf("DoneMsg theme = %q, want %q", d.Palette.Name, first)
	}
}

// TestSetThemeNames: the app refreshes the theme list when theme files
// change. The cursor stays on the highlighted name; when that theme is
// gone, the cursor is clamped and the theme it lands on is previewed, so
// enter confirms the theme on screen.
func TestSetThemeNames(t *testing.T) {
	without := func(names []string, drop string) []string {
		return slices.DeleteFunc(slices.Clone(names), func(n string) bool { return n == drop })
	}
	tests := []struct {
		name        string
		cursorOn    string
		after       func(names []string) []string
		wantCursor  string
		wantPreview string // "" = none
		wantDone    string // "" = enter refused
	}{
		{"highlighted name kept", "good",
			func(ns []string) []string { return append(ns, "zzz") }, "good", "", "good"},
		{"highlighted name moved", "catppuccin-mocha",
			func(ns []string) []string { return append([]string{"aaa"}, ns...) }, "catppuccin-mocha", "", "catppuccin-mocha"},
		{"highlighted theme removed, lands on a working one", "good",
			func(ns []string) []string { return without(without(ns, "good"), "bad") }, "", "last", "last"},
		{"highlighted theme removed, lands on a broken one", "good",
			func(ns []string) []string { return without(ns, "good") }, "bad", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cat, _ := userCatalog(t)
			m, _ := themeStep(t, cat, tt.cursorOn)
			names := tt.after(cat.Names())
			want := tt.wantCursor
			if tt.wantPreview == "last" {
				want = names[len(names)-1]
			}
			m, cmd := m.SetThemeNames(names, nil)
			m, out := drive(t, m, cmd)
			if got := m.theme.names[m.theme.idx]; got != want {
				t.Errorf("cursor on %q, want %q", got, want)
			}
			gotPreviews := previews(out)
			switch {
			case tt.wantPreview == "" && len(gotPreviews) != 0:
				t.Errorf("previews = %v, want none", gotPreviews)
			case tt.wantPreview != "" && !slices.Equal(gotPreviews, []string{want}):
				t.Errorf("previews = %v, want [%s]", gotPreviews, want)
			}
			mustContain(t, m, want)
			_, out = press(t, m, "enter")
			if tt.wantDone == "" {
				if len(out) != 0 {
					t.Errorf("enter on a broken theme sent %v", out)
				}
				return
			}
			wantDone := tt.wantDone
			if wantDone == "last" {
				wantDone = want
			}
			if d := doneMsg(t, out); d.Palette.Name != wantDone {
				t.Errorf("DoneMsg theme = %q, want %q", d.Palette.Name, wantDone)
			}
		})
	}
}

// TestSetThemeNamesBeforeTheThemeStep: names set before the theme step do
// nothing visible; the step lists the catalog when it opens.
func TestSetThemeNamesBeforeTheThemeStep(t *testing.T) {
	cat, _ := userCatalog(t)
	f := &fakeEnv{vaultState: setup.Empty}
	m := New(FirstRun, "~/Notes", builtin("catppuccin-mocha"), cat, f.env(), testStyles()).SetSize(120, 40)
	m, _ = drive(t, m, m.Init())
	m, cmd := m.SetThemeNames([]string{"nord"}, nil)
	if cmd != nil {
		t.Error("SetThemeNames before the theme step returned a command")
	}
	m, _ = press(t, m, "enter", "down", "enter")
	if m.Stage() != StageTheme || !slices.Equal(m.theme.names, cat.Names()) {
		t.Errorf("stage %v, names %v; want the theme step on %v", m.Stage(), m.theme.names, cat.Names())
	}
}

// rewriteTheme writes dir/name.toml.
func rewriteTheme(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name+theme.ThemeExt), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// goodThemeV2 is goodTheme with another base color: another palette ID.
var goodThemeV2 = strings.Replace(goodTheme, "#141318", "#000000", 1)

// TestSetThemeNamesChangedHighlight: a changed file of the highlighted
// theme is read again and previewed, so enter confirms the new colors,
// and a fixed file clears the inline error.
func TestSetThemeNamesChangedHighlight(t *testing.T) {
	tests := []struct {
		name      string
		highlight string
		content   string   // new content of the highlighted file
		changed   []string // names reported changed
		wantErr   bool     // inline error after the refresh
		wantNew   bool     // the preview and DoneMsg carry the new file
	}{
		{"rewritten", "good", goodThemeV2, []string{"good"}, false, true},
		{"fixed", "bad", goodThemeV2, []string{"bad"}, false, true},
		{"broken", "good", "x", []string{"good"}, true, false},
		{"rewritten but not reported", "good", goodThemeV2, []string{"other"}, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cat, dir := userCatalog(t)
			m, _ := themeStep(t, cat, tt.highlight)
			old := m.theme.cur
			rewriteTheme(t, dir, tt.highlight, tt.content)
			m, cmd := m.SetThemeNames(cat.Names(), tt.changed)
			m, out := drive(t, m, cmd)

			if got := strings.Contains(plain(m), tt.highlight+".toml"); got != tt.wantErr {
				t.Errorf("inline error shown = %v, want %v:\n%s", got, tt.wantErr, plain(m))
			}
			_, done := press(t, m, "enter")
			if tt.wantErr {
				if len(done) != 0 {
					t.Errorf("enter on a broken theme sent %v", done)
				}
				return
			}
			want := old.Key()
			if tt.wantNew {
				p, err := cat.Resolve(tt.highlight)
				if err != nil {
					t.Fatal(err)
				}
				want = p.Key()
				if ps := previewPalettes(out); len(ps) != 1 || ps[0].Key() != want {
					t.Errorf("previews = %v, want the new %s", previews(out), want)
				}
			} else if len(previews(out)) != 0 {
				t.Errorf("previews = %v, want none", previews(out))
			}
			if d := doneMsg(t, done); d.Palette.Key() != want {
				t.Errorf("DoneMsg palette = %q, want %q", d.Palette.Key(), want)
			}
		})
	}
}

// TestSetThemeNamesChangedCurrent: a changed file of the current theme is
// read again, before and during the theme step; a file that no longer
// loads keeps the palette from memory.
func TestSetThemeNamesChangedCurrent(t *testing.T) {
	tests := []struct {
		name    string
		content string
		wantNew bool
	}{
		{"rewritten", goodThemeV2, true},
		{"broken", "x", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cat, dir := userCatalog(t)
			cur, err := cat.Resolve("good")
			if err != nil {
				t.Fatal(err)
			}
			f := &fakeEnv{vaultState: setup.Empty}
			m := New(FirstRun, "~/Notes", cur, cat, f.env(), testStyles()).SetSize(120, 40)
			m, _ = drive(t, m, m.Init())
			rewriteTheme(t, dir, "good", tt.content)
			m, _ = m.SetThemeNames(cat.Names(), []string{"good"})
			want := cur.Key()
			if tt.wantNew {
				p, err := cat.Resolve("good")
				if err != nil {
					t.Fatal(err)
				}
				want = p.Key()
			}
			if m.current.Key() != want {
				t.Errorf("current = %q, want %q", m.current.Key(), want)
			}
			// The theme step opens on it; enter confirms it.
			m, _ = press(t, m, "enter", "down", "enter")
			if m.Stage() != StageTheme {
				t.Fatalf("stage = %v", m.Stage())
			}
			if _, out := press(t, m, "enter"); doneMsg(t, out).Palette.Key() != want {
				t.Errorf("DoneMsg palette = %q, want %q", doneMsg(t, out).Palette.Key(), want)
			}
			// Esc goes back to it too.
			if _, out := press(t, m, "esc"); len(previewPalettes(out)) != 1 || previewPalettes(out)[0].Key() != want {
				t.Errorf("esc previews %v, want %q", previews(out), want)
			}
		})
	}
}
