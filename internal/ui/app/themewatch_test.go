package app

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mathieucroset/notty/internal/gitsync/gittest"
	"github.com/mathieucroset/notty/internal/setup"
	"github.com/mathieucroset/notty/internal/ui/msgs"
	"github.com/mathieucroset/notty/internal/ui/palette"
	"github.com/mathieucroset/notty/internal/ui/theme"
	"github.com/mathieucroset/notty/internal/ui/wizard"
)

// themeVariant is userThemeFile with its base color and H1 color set, so
// each variant has its own ID and a heading color to look for.
func themeVariant(base, h1 string) string {
	return strings.Replace(userThemeFile, "#141318", base, 1) + fmt.Sprintf("headings = [%q]\n", h1)
}

// Theme file versions used by the reload tests.
var (
	mineV1 = themeVariant("#111111", "#a1a1a1")
	mineV2 = themeVariant("#222222", "#b2b2b2")
)

// fgSeq is the truecolor foreground parameters of hex "#rrggbb", as they
// appear in rendered output.
func fgSeq(t *testing.T, hex string) string {
	t.Helper()
	v, err := strconv.ParseUint(strings.TrimPrefix(hex, "#"), 16, 32)
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("38;2;%d;%d;%d", v>>16, v>>8&0xff, v&0xff)
}

// loadKey is the palette key of dir/name.toml as it is on disk now.
func loadKey(t *testing.T, dir, name string) string {
	t.Helper()
	p, err := theme.LoadUser(dir, name)
	if err != nil {
		t.Fatal(err)
	}
	return p.Key()
}

// themeKey is the palette key of content loaded as the theme mine.
func themeKey(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	writeTheme(t, dir, "mine", content)
	return loadKey(t, dir, "mine")
}

// themeWarnings counts the warning toasts about name.toml.
func themeWarnings(m *Model, name string) int {
	n := 0
	for _, e := range m.toast.Log() {
		if e.Level == msgs.ToastWarn && strings.Contains(e.Text, name+theme.ThemeExt) {
			n++
		}
	}
	return n
}

// stopThemeWatcher closes m's theme watcher: the test delivers the
// watcher's batches itself, and files it writes cannot race with them.
func stopThemeWatcher(t *testing.T, m *Model) {
	t.Helper()
	if m.themes == nil {
		t.Fatal("no theme watcher running")
	}
	m.closeThemeWatcher()
}

// showPreview opens ideas.md in the preview view.
func showPreview(t *testing.T, m *Model) {
	t.Helper()
	run(t, m, msgs.OpenNoteMsg{Path: "ideas.md"})
	for m.NoteView() != ViewPreview {
		run(t, m, msgs.CycleNoteViewMsg{})
	}
}

// reloadStep writes a theme file ("" removes it), then delivers the
// watcher batch naming it.
type reloadStep struct {
	file, content string
}

// TestHandleThemeFiles covers the live reload rules of user themes spec §3.
// Each step rewrites a theme file and delivers the batch the watcher would
// send.
func TestHandleThemeFiles(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string // initial theme files
		// start is the theme displayed at start ("" = catppuccin-mocha);
		// configured is Config.Theme when it differs (a startup fallback).
		start, configured string
		// picker opens the theme picker on the start theme and highlights
		// this theme before the steps ("" = no picker).
		picker string
		steps  []reloadStep
		// leave is sent after the steps (nil = nothing).
		leave any
		// wantShown is the palette displayed at the end: "v1" or "v2" (mineV1
		// or mineV2 loaded), or a built-in's name.
		wantShown    string
		wantWarnings int    // warning toasts about mine.toml
		wantH1       string // preview H1 color, when set
	}{
		{name: "active theme rewritten",
			files: map[string]string{"mine": mineV1}, start: "mine",
			steps:     []reloadStep{{"mine", mineV2}},
			wantShown: "v2", wantH1: "#b2b2b2"},
		{name: "active theme broken, twice the same way",
			files: map[string]string{"mine": mineV1}, start: "mine",
			steps:     []reloadStep{{"mine", brokenThemeFile}, {"mine", brokenThemeFile}},
			wantShown: "v1", wantWarnings: 1, wantH1: "#a1a1a1"},
		{name: "broken then fixed: the fix applies silently",
			files: map[string]string{"mine": mineV1}, start: "mine",
			steps:     []reloadStep{{"mine", brokenThemeFile}, {"mine", mineV2}},
			wantShown: "v2", wantWarnings: 1, wantH1: "#b2b2b2"},
		{name: "broken, fixed, broken again: warned again",
			files: map[string]string{"mine": mineV1}, start: "mine",
			steps:     []reloadStep{{"mine", brokenThemeFile}, {"mine", mineV2}, {"mine", brokenThemeFile}},
			wantShown: "v2", wantWarnings: 2, wantH1: "#b2b2b2"},
		{name: "active theme deleted",
			files: map[string]string{"mine": mineV1}, start: "mine",
			steps:     []reloadStep{{"mine", ""}},
			wantShown: "v1", wantH1: "#a1a1a1"},
		{name: "active theme deleted then recreated",
			files: map[string]string{"mine": mineV1}, start: "mine",
			steps:     []reloadStep{{"mine", ""}, {"mine", mineV2}},
			wantShown: "v2", wantH1: "#b2b2b2"},
		{name: "startup fallback, then the file appears",
			files: map[string]string{}, configured: "mine",
			steps:     []reloadStep{{"mine", mineV2}},
			wantShown: "v2", wantH1: "#b2b2b2"},
		{name: "picker previewing another theme, original rewritten, esc",
			files: map[string]string{"mine": mineV1}, start: "mine", picker: "nord",
			steps: []reloadStep{{"mine", mineV2}}, leave: keyMsg("esc"),
			wantShown: "v2", wantH1: "#b2b2b2"},
		{name: "picker previewing another theme, original rewritten, CloseMsg",
			files: map[string]string{"mine": mineV1}, start: "mine", picker: "nord",
			steps: []reloadStep{{"mine", mineV2}}, leave: palette.CloseMsg{},
			wantShown: "v2", wantH1: "#b2b2b2"},
		{name: "picker previewing the rewritten theme, esc",
			files: map[string]string{"mine": mineV1}, picker: "mine",
			steps: []reloadStep{{"mine", mineV2}}, leave: keyMsg("esc"),
			wantShown: "catppuccin-mocha"},
		{name: "picker opened on a startup fallback, then the file appears, esc",
			files: map[string]string{}, configured: "mine", picker: "nord",
			steps: []reloadStep{{"mine", mineV2}}, leave: keyMsg("esc"),
			wantShown: "v2", wantH1: "#b2b2b2"},
		{name: "unrelated theme files",
			files: map[string]string{"mine": mineV1}, start: "mine",
			steps:     []reloadStep{{"other", brokenThemeFile}, {"nord", brokenThemeFile}},
			wantShown: "v1", wantH1: "#a1a1a1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := testOptions(t)
			dir := withUserThemes(t, &opts, tt.files)
			if tt.start != "" {
				startWithTheme(t, &opts, tt.start)
			}
			if tt.configured != "" {
				opts.Config.Theme = tt.configured
			}
			m := start(t, opts, 120, 30)
			stopThemeWatcher(t, m)
			showPreview(t, m)
			if tt.picker != "" {
				openThemePicker(t, m)
				highlightTheme(t, m, m.opts.Palette.Name, tt.picker)
			}
			shown := m.opts.Palette.Key()

			for _, s := range tt.steps {
				writeTheme(t, dir, s.file, s.content)
				run(t, m, themeFilesMsg{names: []string{s.file}})
				if tt.picker != "" {
					// The picker keeps its preview; the original follows
					// the file.
					if m.opts.Palette.Name != tt.picker && m.opts.Palette.Name != "mine" {
						t.Errorf("palette %q while the picker previews %q", m.opts.Palette.Name, tt.picker)
					}
					if tt.picker != "mine" && m.opts.Palette.Key() != shown {
						t.Errorf("preview replaced: %q, want %q", m.opts.Palette.Key(), shown)
					}
					// A preview of the rewritten theme follows the file.
					if tt.picker == "mine" && m.opts.Palette.Key() != loadKey(t, dir, "mine") {
						t.Errorf("preview = %q, want the new mine", m.opts.Palette.Key())
					}
				}
			}
			if tt.picker != "" && tt.picker != "mine" {
				if got, want := m.paletteOverlay().paletteOrig.Key(), loadKey(t, dir, "mine"); got != want {
					t.Errorf("picker original = %q, want the new %q", got, want)
				}
			}
			if tt.leave != nil {
				run(t, m, tt.leave)
				if m.overlayOpen() {
					run(t, m, keyMsg("esc")) // esc left theme mode: close the palette too
				}
				if m.overlayOpen() {
					t.Fatal("palette still open")
				}
			}

			want := map[string]string{"v1": themeKey(t, mineV1), "v2": themeKey(t, mineV2)}[tt.wantShown]
			if want == "" {
				want = tt.wantShown
			}
			if got := m.opts.Palette.Key(); got != want {
				t.Errorf("palette = %q, want %q", got, want)
			}
			if n := themeWarnings(m, "mine"); n != tt.wantWarnings {
				t.Errorf("%d warnings about mine.toml, want %d; toasts %q", n, tt.wantWarnings, toastTexts(m))
			}
			if n := themeWarnings(m, "other"); n != 0 {
				t.Errorf("an unrelated theme file was loaded: toasts %q", toastTexts(m))
			}
			if tt.wantH1 != "" && !strings.Contains(m.View().Content, fgSeq(t, tt.wantH1)) {
				t.Errorf("preview heading not in %s", tt.wantH1)
			}
			if _, err := os.Stat(opts.ConfigPath); err == nil {
				t.Error("a reload wrote the config")
			}
		})
	}
}

// TestThemeFilesRefreshOpenPicker: the open picker lists a theme file as
// soon as it appears, with its cursor kept on the same name.
func TestThemeFilesRefreshOpenPicker(t *testing.T) {
	opts := testOptions(t)
	dir := withUserThemes(t, &opts, map[string]string{"mine": mineV1})
	m := start(t, opts, 120, 30)
	stopThemeWatcher(t, m)
	openThemePicker(t, m)
	highlightTheme(t, m, "catppuccin-mocha", "mine")
	writeTheme(t, dir, "aaa", mineV2)
	run(t, m, themeFilesMsg{names: []string{"aaa"}})
	o := m.paletteOverlay()
	if o == nil || !slices.Equal(o.palette.ThemeNames(), opts.Catalog.Names()) || !slices.Contains(o.palette.ThemeNames(), "aaa") {
		t.Fatalf("picker lists %v, want %v", o.palette.ThemeNames(), opts.Catalog.Names())
	}
	run(t, m, keyMsg("enter"))
	if m.opts.Palette.Name != "mine" || m.themeName != "mine" {
		t.Errorf("chose %q, want mine (the highlighted theme)", m.opts.Palette.Name)
	}
}

// TestThemeFilesRefreshWizard: the wizard's theme step lists a theme file
// as soon as it appears; an open wizard is previewing, so the theme it
// wants is not forced on it.
func TestThemeFilesRefreshWizard(t *testing.T) {
	gittest.Isolate(t)
	setIdentity(t)
	opts := wizardOptions(t)
	dir := withUserThemes(t, &opts, nil)
	opts.Config.Theme = "mine" // a startup fallback
	m := start(t, opts, 100, 30)
	t.Cleanup(m.Shutdown)
	stopThemeWatcher(t, m)
	run(t, m, keyMsg("enter")) // vault folder
	run(t, m, keyMsg("j"))     // Existing URL → Local only
	run(t, m, keyMsg("enter"))
	waitFor(t, m, func() bool { return m.wizard != nil && m.wizard.Stage() == wizard.StageTheme })

	if strings.Contains(screen(m), "mine") {
		t.Fatalf("mine listed before its file exists:\n%s", screen(m))
	}
	writeTheme(t, dir, "mine", mineV1)
	run(t, m, themeFilesMsg{names: []string{"mine"}})
	if !strings.Contains(screen(m), "mine") {
		t.Errorf("the wizard does not list the new theme:\n%s", screen(m))
	}
	if m.opts.Palette.Name != "catppuccin-mocha" {
		t.Errorf("palette = %q under the wizard, want catppuccin-mocha kept", m.opts.Palette.Name)
	}
	run(t, m, wizard.DoneMsg{Vault: m.wizard.VaultInput(), Palette: builtin(t, "nord"), Choice: setup.LocalOnly})
	finishWizard(t, m)
	if m.opts.Palette.Name != "nord" || m.themeName != "nord" {
		t.Errorf("palette / themeName = %q / %q, want nord", m.opts.Palette.Name, m.themeName)
	}
}

// TestWizardConfirmsReloadedTheme: the first-run wizard highlights a user
// theme whose file is then rewritten; enter saves the theme with the new
// colors, not the ones read when the cursor landed on it.
func TestWizardConfirmsReloadedTheme(t *testing.T) {
	gittest.Isolate(t)
	setIdentity(t)
	opts := wizardOptions(t)
	dir := withUserThemes(t, &opts, map[string]string{"mine": mineV1})
	m := start(t, opts, 100, 30)
	t.Cleanup(m.Shutdown)
	stopThemeWatcher(t, m)
	run(t, m, keyMsg("enter")) // vault folder
	run(t, m, keyMsg("j"))     // Existing URL → Local only
	run(t, m, keyMsg("enter"))
	waitFor(t, m, func() bool { return m.wizard != nil && m.wizard.Stage() == wizard.StageTheme })
	names := opts.Catalog.Names()
	for range slices.Index(names, "mine") - slices.Index(names, "catppuccin-mocha") {
		run(t, m, downKey)
	}
	if m.opts.Palette.Key() != themeKey(t, mineV1) {
		t.Fatalf("previewed %q, want mine v1", m.opts.Palette.Key())
	}

	writeTheme(t, dir, "mine", mineV2)
	run(t, m, themeFilesMsg{names: []string{"mine"}})
	run(t, m, keyMsg("enter")) // theme
	finishWizard(t, m)

	if got, want := m.opts.Palette.Key(), themeKey(t, mineV2); got != want {
		t.Errorf("palette = %q, want the rewritten mine %q", got, want)
	}
	b, err := os.ReadFile(opts.ConfigPath)
	if err != nil || !strings.Contains(string(b), `theme = "mine"`) {
		t.Errorf("config = %q (%v), want theme = \"mine\"", b, err)
	}
}

// TestLiveReload drives the real watcher: rewriting the active theme file
// re-themes the app, every time.
func TestLiveReload(t *testing.T) {
	opts := testOptions(t)
	dir := withUserThemes(t, &opts, map[string]string{"mine": mineV1})
	startWithTheme(t, &opts, "mine")
	m := start(t, opts, 120, 30)
	showPreview(t, m)
	for _, v := range []struct{ content, h1 string }{
		{mineV2, "#b2b2b2"},
		{themeVariant("#333333", "#c3c3c3"), "#c3c3c3"},
	} {
		writeTheme(t, dir, "mine", v.content)
		want := loadKey(t, dir, "mine")
		waitFor(t, m, func() bool { return m.opts.Palette.Key() == want })
		// The preview re-renders in the new colors.
		drive(t, m, nil)
		if !strings.Contains(m.View().Content, fgSeq(t, v.h1)) {
			t.Errorf("preview heading not in %s", v.h1)
		}
	}
}

// TestThemeWatcherLifecycle: the watcher starts once, only with a themes
// directory it can watch, and Shutdown stops it.
func TestThemeWatcherLifecycle(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name    string
		dir     string
		watched bool
	}{
		{"no themes directory configured", "", false},
		{"themes directory created", filepath.Join(t.TempDir(), "config", "themes"), true},
		{"themes directory unusable", filepath.Join(blocker, "themes"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := testOptions(t)
			opts.Catalog = theme.Catalog{Dir: tt.dir}
			m := start(t, opts, 120, 30)
			if (m.themes != nil) != tt.watched {
				t.Fatalf("watching = %v, want %v", m.themes != nil, tt.watched)
			}
			if len(toastTexts(m)) != 0 {
				t.Errorf("toasts %q, want none", toastTexts(m))
			}
			if !tt.watched {
				return
			}
			if info, err := os.Stat(tt.dir); err != nil || !info.IsDir() {
				t.Errorf("themes directory not created: %v", err)
			}
			tw := m.themes
			if drive(t, m, execOne(t, m, m.Init())); m.themes != tw {
				t.Error("Init again started another watcher")
			}
			m.Shutdown()
			if m.themes != nil {
				t.Error("Shutdown kept the watcher")
			}
			m.Shutdown() // idempotent
			done := make(chan struct{})
			go func() {
				if msg := listenThemesCmd(tw)(); msg != nil {
					t.Errorf("listener after Close returned %T", msg)
				}
				close(done)
			}()
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("listener still waiting after Close")
			}
		})
	}
}

// TestThemeWatcher drives the watcher with real file events: writes are
// debounced into one sorted batch of theme names.
func TestThemeWatcher(t *testing.T) {
	tests := []struct {
		name   string
		writes []string // file names, written 30ms apart
		want   []string // nil = no batch
	}{
		{"rapid rewrites of one theme", []string{"mine.toml", "mine.toml", "mine.toml"}, []string{"mine"}},
		{"several themes", []string{"b.toml", "a.toml", "b.toml"}, []string{"a", "b"}},
		{"other files ignored", []string{"notes.txt", "mine.toml.swp", ".toml"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := filepath.Join(t.TempDir(), "themes")
			tw, err := newThemeWatcher(dir)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = tw.Close() })
			first := time.Now()
			for i, f := range tt.writes {
				if i > 0 {
					time.Sleep(30 * time.Millisecond)
				}
				if err := os.WriteFile(filepath.Join(dir, f), []byte(strconv.Itoa(i)), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if tt.want == nil {
				select {
				case got := <-tw.out:
					t.Fatalf("batch %v for non-theme files", got)
				case <-time.After(2 * themeDebounce):
				}
				return
			}
			select {
			case got := <-tw.out:
				if !slices.Equal(got, tt.want) {
					t.Errorf("batch = %v, want %v", got, tt.want)
				}
				if waited := time.Since(first); waited < themeDebounce {
					t.Errorf("batch after %v, before the %v debounce", waited, themeDebounce)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("no batch after 2s")
			}
			select {
			case got := <-tw.out:
				t.Errorf("second batch %v, want one", got)
			case <-time.After(2 * themeDebounce):
			}
		})
	}
}
