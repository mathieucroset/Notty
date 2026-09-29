package app

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/mathieucroset/notty/internal/gitsync/gittest"
	"github.com/mathieucroset/notty/internal/ui/msgs"
	"github.com/mathieucroset/notty/internal/ui/palette"
	"github.com/mathieucroset/notty/internal/ui/theme"
)

func TestStartupWarningsShownAsToasts(t *testing.T) {
	tests := []struct {
		name string
		opts func(t *testing.T) Options
	}{
		{"no vault", func(*testing.T) Options { return Options{} }},
		{"vault", testOptions},
		{"wizard", func(t *testing.T) Options {
			gittest.Isolate(t)
			return wizardOptions(t)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := tt.opts(t)
			opts.StartupWarnings = []string{"boom", "bang"}
			m := start(t, opts, 100, 30)
			t.Cleanup(m.Shutdown)
			for _, w := range opts.StartupWarnings {
				if !hasToast(m, msgs.ToastWarn, w) {
					t.Errorf("no warning toast %q; toasts %q", w, toastTexts(m))
				}
			}
			if !strings.Contains(screen(m), "boom") {
				t.Errorf("warning not on screen:\n%s", screen(m))
			}
			// Init again (it must not repeat them): each shown once.
			drive(t, m, execOne(t, m, m.Init()))
			for _, w := range opts.StartupWarnings {
				n := 0
				for _, s := range toastTexts(m) {
					if s == w {
						n++
					}
				}
				if n != 1 {
					t.Errorf("warning %q shown %d times, want 1", w, n)
				}
			}
		})
	}
}

func TestThemeNameAtStart(t *testing.T) {
	tests := []struct {
		name, config, want string
	}{
		{"configured theme", "mine", "mine"},
		{"no configured theme", "", "catppuccin-mocha"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := testOptions(t)
			opts.Config.Theme = tt.config
			if m := New(opts); m.themeName != tt.want {
				t.Errorf("themeName = %q, want %q", m.themeName, tt.want)
			}
		})
	}
}

// userThemeFile is a valid user theme file.
const userThemeFile = `base = "#141318"
surface = "#201f24"
overlay = "#36343a"
text = "#e6e1e9"
subtext = "#cac4cf"
muted = "#948f99"
accent = "#cfbcff"
accent2 = "#f2b7c2"
error = "#ffb4ab"
`

var upKey = tea.KeyPressMsg{Code: tea.KeyUp}

// brokenThemeFile does not parse as a theme.
const brokenThemeFile = "x"

// withUserThemes gives opts a catalog on a temporary themes directory
// holding files (theme name → content), and returns the directory.
func withUserThemes(t *testing.T, opts *Options, files map[string]string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "themes")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range files {
		writeTheme(t, dir, name, content)
	}
	opts.Catalog = theme.Catalog{Dir: dir}
	return dir
}

// writeTheme writes dir/name.toml; content "" removes it.
func writeTheme(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name+theme.ThemeExt)
	if content == "" {
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		return
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// startWithTheme makes name, resolved through opts.Catalog, the theme the
// app starts with.
func startWithTheme(t *testing.T, opts *Options, name string) {
	t.Helper()
	p, err := opts.Catalog.Resolve(name)
	if err != nil {
		t.Fatal(err)
	}
	opts.Palette, opts.Styles, opts.Config.Theme = p, theme.NewStyles(p), name
}

// highlightTheme moves the open theme picker's cursor from the theme from
// to the theme to, previewing each theme on the way.
func highlightTheme(t *testing.T, m *Model, from, to string) {
	t.Helper()
	names := m.opts.Catalog.Names()
	i, j := slices.Index(names, from), slices.Index(names, to)
	if i < 0 || j < 0 {
		t.Fatalf("theme %q or %q not in %v", from, to, names)
	}
	for ; i < j; i++ {
		run(t, m, downKey)
	}
	for ; i > j; i-- {
		run(t, m, upKey)
	}
}

// themeBefore is the theme listed just before name in the catalog.
func themeBefore(t *testing.T, cat theme.Catalog, name string) string {
	t.Helper()
	names := cat.Names()
	i := slices.Index(names, name)
	if i < 1 {
		t.Fatalf("no theme before %q in %v", name, names)
	}
	return names[i-1]
}

func TestThemePickerChoice(t *testing.T) {
	tests := []struct {
		name      string
		file      string // themes/mine.toml
		wantOK    bool
		wantTheme string // displayed palette after the choice
	}{
		{"working user theme", userThemeFile, true, "mine"},
		{"broken user theme", brokenThemeFile, false, "catppuccin-mocha"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := testOptions(t)
			withUserThemes(t, &opts, map[string]string{"mine": tt.file})
			m := start(t, opts, 120, 30)
			orig := m.opts.Palette
			openThemePicker(t, m)
			highlightTheme(t, m, "catppuccin-mocha", "mine")
			if !strings.Contains(screen(m), "mine") {
				t.Fatalf("picker does not list the user theme:\n%s", screen(m))
			}
			run(t, m, keyMsg("enter"))

			if m.overlayOpen() {
				t.Error("palette still open after the choice")
			}
			if got := m.opts.Palette.Name; got != tt.wantTheme {
				t.Errorf("palette = %q, want %q", got, tt.wantTheme)
			}
			b, err := os.ReadFile(opts.ConfigPath)
			if tt.wantOK {
				if err != nil || !strings.Contains(string(b), `theme = "mine"`) {
					t.Errorf("config = %q (%v), want theme = \"mine\"", b, err)
				}
				if m.themeName != "mine" || m.opts.Config.Theme != "mine" {
					t.Errorf("themeName / Config.Theme = %q / %q, want mine", m.themeName, m.opts.Config.Theme)
				}
				return
			}
			if err == nil {
				t.Errorf("a broken choice wrote the config: %q", b)
			}
			if m.opts.Palette.Key() != orig.Key() {
				t.Errorf("palette = %q, want the original %q", m.opts.Palette.Key(), orig.Key())
			}
			if m.themeName != "catppuccin-mocha" || m.opts.Config.Theme == "mine" {
				t.Errorf("themeName / Config.Theme = %q / %q after a broken choice", m.themeName, m.opts.Config.Theme)
			}
			if !hasToast(m, msgs.ToastWarn, "theme not changed") {
				t.Errorf("no warning toast for the broken choice; toasts %q", toastTexts(m))
			}
		})
	}
}

func TestThemePreviewOfBrokenThemeKeepsColors(t *testing.T) {
	opts := testOptions(t)
	withUserThemes(t, &opts, map[string]string{"mine": brokenThemeFile})
	m := start(t, opts, 120, 30)
	prev := themeBefore(t, opts.Catalog, "mine")
	openThemePicker(t, m)
	highlightTheme(t, m, "catppuccin-mocha", "mine")
	if m.opts.Palette.Name != prev {
		t.Errorf("palette = %q, want %q kept", m.opts.Palette.Name, prev)
	}
	// Moving off and back on shows the same error once.
	run(t, m, upKey)
	run(t, m, downKey)
	n := 0
	for _, e := range m.toast.Log() {
		if e.Level == msgs.ToastWarn && strings.Contains(e.Text, "mine.toml") {
			n++
		}
	}
	if n != 1 {
		t.Errorf("%d warning toasts for the broken theme, want 1; toasts %q", n, toastTexts(m))
	}
}

// TestThemePickerRestoresFromMemory: leaving the picker restores the
// palette it opened with, even when that theme's file broke meanwhile.
func TestThemePickerRestoresFromMemory(t *testing.T) {
	tests := []struct {
		name       string
		startTheme string // "" = catppuccin-mocha
		preview    func(t *testing.T, cat theme.Catalog) string
		breakMine  string // new mine.toml content ("" = deleted)
		leave      tea.Msg
		wantOpen   bool
	}{
		{"built-in original, previewed file deleted, esc", "",
			func(*testing.T, theme.Catalog) string { return "mine" }, "", keyMsg("esc"), true},
		{"user original broken, esc", "mine",
			func(t *testing.T, cat theme.Catalog) string { return themeBefore(t, cat, "mine") }, brokenThemeFile, keyMsg("esc"), true},
		{"user original deleted, CloseMsg", "mine",
			func(t *testing.T, cat theme.Catalog) string { return themeBefore(t, cat, "mine") }, "", palette.CloseMsg{}, false},
		{"built-in original, CloseMsg", "",
			func(*testing.T, theme.Catalog) string { return "nord" }, userThemeFile, palette.CloseMsg{}, false},
		{"user original broken, replaced by the finder", "mine",
			func(t *testing.T, cat theme.Catalog) string { return themeBefore(t, cat, "mine") }, brokenThemeFile, msgs.OpenFinderMsg{}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := testOptions(t)
			dir := withUserThemes(t, &opts, map[string]string{"mine": userThemeFile})
			if tt.startTheme != "" {
				startWithTheme(t, &opts, tt.startTheme)
			}
			m := start(t, opts, 120, 30)
			orig := m.opts.Palette
			before := m.View().Content
			openThemePicker(t, m)
			target := tt.preview(t, opts.Catalog)
			highlightTheme(t, m, orig.Name, target)
			if m.opts.Palette.Name != target {
				t.Fatalf("previewed palette = %q, want %q", m.opts.Palette.Name, target)
			}
			writeTheme(t, dir, "mine", tt.breakMine)

			run(t, m, tt.leave)
			if m.opts.Palette.Key() != orig.Key() {
				t.Errorf("palette = %q, want the original %q", m.opts.Palette.Key(), orig.Key())
			}
			if m.overlayOpen() != tt.wantOpen {
				t.Errorf("overlay open = %v, want %v", m.overlayOpen(), tt.wantOpen)
			}
			if !tt.wantOpen && m.View().Content != before {
				t.Error("the screen differs after leaving the picker")
			}
			if _, err := os.Stat(opts.ConfigPath); err == nil {
				t.Error("leaving the picker wrote the config")
			}
		})
	}
}

// TestLatePreviewAfterCloseIgnored: a preview command that lands after the
// picker closed (commands run concurrently) must not change the colors,
// since nothing would restore them.
func TestLatePreviewAfterCloseIgnored(t *testing.T) {
	m := start(t, testOptions(t), 120, 30)
	run(t, m, palette.ThemePreviewMsg{Name: "nord"})
	if got := m.opts.Palette.Name; got != "catppuccin-mocha" {
		t.Errorf("palette = %q after a preview with no picker open", got)
	}
}
