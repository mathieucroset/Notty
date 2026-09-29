package app

import (
	"os"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/mathieucroset/notty/internal/ui/keys"
	"github.com/mathieucroset/notty/internal/ui/msgs"
	"github.com/mathieucroset/notty/internal/ui/palette"
	"github.com/mathieucroset/notty/internal/ui/theme"
)

// typeText sends each rune of s as a key press.
func typeText(t *testing.T, m *Model, s string) {
	t.Helper()
	for _, r := range s {
		run(t, m, keyMsg(string(r)))
	}
}

var downKey = tea.KeyPressMsg{Code: tea.KeyDown}

func TestPaletteRunsCommand(t *testing.T) {
	m := start(t, testOptions(t), 120, 30)
	run(t, m, keyMsg("ctrl+k"))
	if m.topOverlay() == nil || m.topOverlay().kind != overlayPalette {
		t.Fatal("ctrl+k did not open the palette")
	}
	if got := m.keyContext(); got != keys.Overlay {
		t.Errorf("keyContext = %v, want Overlay", got)
	}
	s := screen(m)
	for _, want := range []string{"New note", "Switch theme", "Open Trash"} {
		if !strings.Contains(s, want) {
			t.Errorf("palette missing %q:\n%s", want, s)
		}
	}
	assertSize(t, m, 120, 30)

	typeText(t, m, "open trash")
	run(t, m, keyMsg("enter"))
	if m.overlayOpen() {
		t.Error("palette still open after running a command")
	}
	if m.MainView() != ViewTrash {
		t.Errorf("MainView = %v, want ViewTrash", m.MainView())
	}
}

func TestPaletteNewNoteOpensDialog(t *testing.T) {
	m := start(t, testOptions(t), 120, 30)
	run(t, m, keyMsg("ctrl+k"))
	typeText(t, m, "new note")
	run(t, m, keyMsg("enter"))
	// The palette's close request must not close the dialog that replaced
	// it.
	if m.topOverlay() == nil || m.topOverlay().kind != overlayDialog || m.topOverlay().pending.kind != opNewNote {
		t.Fatalf("new note dialog not open: %+v", m.topOverlay())
	}
	// The sidebar cursor is on the Work folder (the first row).
	if m.topOverlay().pending.path != "Work" {
		t.Errorf("palette new note targets %q, want the sidebar's folder Work", m.topOverlay().pending.path)
	}
}

func openThemePicker(t *testing.T, m *Model) {
	t.Helper()
	run(t, m, keyMsg("ctrl+k"))
	typeText(t, m, "switch theme")
	run(t, m, keyMsg("enter"))
}

// nextTheme is the theme after name in the picker.
func nextTheme(t *testing.T, name string) string {
	t.Helper()
	names := theme.Names()
	i := slices.Index(names, name)
	if i < 0 || i+1 >= len(names) {
		t.Fatalf("no theme after %q in %v", name, names)
	}
	return names[i+1]
}

func TestThemePreviewAndCancel(t *testing.T) {
	opts := testOptions(t)
	m := start(t, opts, 120, 30)
	before := m.View().Content
	openThemePicker(t, m)
	run(t, m, downKey)
	want := nextTheme(t, "catppuccin-mocha")
	if m.opts.Palette.Name != want {
		t.Errorf("previewed palette = %q, want %q", m.opts.Palette.Name, want)
	}
	run(t, m, keyMsg("esc")) // back to the command list, theme restored
	if m.opts.Palette.Name != "catppuccin-mocha" {
		t.Errorf("palette after cancel = %q", m.opts.Palette.Name)
	}
	run(t, m, keyMsg("esc")) // close the palette
	if m.overlayOpen() {
		t.Fatal("palette still open")
	}
	if after := m.View().Content; after != before {
		t.Error("the screen differs after cancelling the theme preview")
	}
	if _, err := os.Stat(opts.ConfigPath); err == nil {
		t.Error("cancelled preview wrote the config")
	}
}

func TestThemeChosenPersists(t *testing.T) {
	opts := testOptions(t)
	m := start(t, opts, 120, 30)
	openThemePicker(t, m)
	run(t, m, downKey)
	run(t, m, keyMsg("enter"))
	want := nextTheme(t, "catppuccin-mocha")
	if m.opts.Config.Theme != want || m.opts.Palette.Name != want {
		t.Errorf("theme = %q / %q, want %q", m.opts.Config.Theme, m.opts.Palette.Name, want)
	}
	if m.overlayOpen() {
		t.Error("palette still open")
	}
	b, err := os.ReadFile(opts.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), want) {
		t.Errorf("config = %q, want theme %q", b, want)
	}
}

func TestToggleVimAndLineNumbersPersist(t *testing.T) {
	opts := testOptions(t)
	m := start(t, opts, 120, 30)
	run(t, m, palette.ToggleVimMsg{})
	if m.opts.Config.Vim {
		t.Error("vim still on")
	}
	if !strings.Contains(screen(m), "PLAIN") {
		t.Errorf("mode label not updated:\n%s", screen(m))
	}
	run(t, m, palette.ToggleLineNumbersMsg{})
	b, err := os.ReadFile(opts.ConfigPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"vim = false", "line_numbers = true"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("config = %q, missing %q", b, want)
		}
	}
}

func TestHelpOpensAndCloses(t *testing.T) {
	m := start(t, testOptions(t), 120, 30)
	run(t, m, keyMsg("f1"))
	if m.topOverlay() == nil || m.topOverlay().kind != overlayHelp {
		t.Fatal("F1 did not open help")
	}
	if s := screen(m); !strings.Contains(s, "ctrl+p") || !strings.Contains(s, "fuzzy finder") {
		t.Errorf("help content missing:\n%s", s)
	}
	assertSize(t, m, 120, 30)
	run(t, m, keyMsg("esc"))
	if m.overlayOpen() {
		t.Error("esc did not close help")
	}
	run(t, m, keyMsg("f1"))
	run(t, m, keyMsg("f1"))
	if m.overlayOpen() {
		t.Error("second F1 did not close help")
	}
	// ? in the sidebar opens it too.
	run(t, m, keyMsg("?"))
	if m.topOverlay() == nil || m.topOverlay().kind != overlayHelp {
		t.Error("? did not open help")
	}
}

func TestHelpStacksOverDialogAndPalette(t *testing.T) {
	m := start(t, testOptions(t), 120, 30)
	run(t, m, msgs.RequestRename{Path: "ideas.md"})
	run(t, m, keyMsg("f1"))
	if len(m.overlays) != 2 || m.topOverlay().kind != overlayHelp {
		t.Fatalf("F1 over a dialog: overlays = %d, top = %+v", len(m.overlays), m.topOverlay())
	}
	if !strings.Contains(screen(m), "fuzzy finder") {
		t.Errorf("help not drawn on top:\n%s", screen(m))
	}
	assertSize(t, m, 120, 30)
	run(t, m, keyMsg("esc"))
	if o := m.topOverlay(); o == nil || o.kind != overlayDialog || o.dialog.ID() != dlgRename {
		t.Fatalf("closing help did not return to the dialog: %+v", o)
	}
	run(t, m, keyMsg("esc"))

	run(t, m, keyMsg("ctrl+k"))
	run(t, m, keyMsg("f1"))
	run(t, m, keyMsg("f1")) // F1 again closes help
	if o := m.topOverlay(); o == nil || o.kind != overlayPalette || len(m.overlays) != 1 {
		t.Fatalf("closing help did not return to the palette: %+v", o)
	}
}

func TestPaletteHelpCommandReplacesPalette(t *testing.T) {
	m := start(t, testOptions(t), 120, 30)
	run(t, m, keyMsg("ctrl+k"))
	typeText(t, m, "help")
	run(t, m, keyMsg("enter"))
	if len(m.overlays) != 1 || m.topOverlay().kind != overlayHelp {
		t.Fatalf("overlays = %d, top = %+v; want help alone", len(m.overlays), m.topOverlay())
	}
	run(t, m, keyMsg("esc"))
	if m.overlayOpen() {
		t.Error("the palette came back after closing help")
	}
}

func TestReplacingPreviewingPaletteCancelsTheme(t *testing.T) {
	m := start(t, testOptions(t), 120, 30)
	openThemePicker(t, m)
	run(t, m, downKey)
	if m.opts.Palette.Name == "catppuccin-mocha" {
		t.Fatal("no preview")
	}
	// A dialog arriving from an earlier command replaces the palette.
	run(t, m, unusedAttachmentsMsg{files: []string{"attachments/a.png"}})
	if o := m.topOverlay(); o == nil || o.kind != overlayDialog || len(m.overlays) != 1 {
		t.Fatalf("dialog did not replace the palette: %+v", o)
	}
	if m.opts.Palette.Name != "catppuccin-mocha" {
		t.Errorf("palette = %q, want the preview cancelled", m.opts.Palette.Name)
	}
}

func TestErrorLogOverlay(t *testing.T) {
	m := start(t, testOptions(t), 120, 30)
	run(t, m, msgs.ToastMsg{Level: msgs.ToastError, Text: "Disk on fire"})
	run(t, m, msgs.ToastMsg{Level: msgs.ToastWarn, Text: "Running hot"})
	run(t, m, msgs.ToastMsg{Level: msgs.ToastInfo, Text: "Pinned things"})
	run(t, m, keyMsg("!"))
	if m.topOverlay() == nil || m.topOverlay().kind != overlayLog {
		t.Fatal("! did not open the error log")
	}
	s := ansi.Strip(m.logBox(m.topOverlay())) // the box alone, not the toasts
	for _, want := range []string{"Error log", "Disk on fire", "Running hot"} {
		if !strings.Contains(s, want) {
			t.Errorf("error log missing %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, "Pinned things") {
		t.Errorf("info toast listed in the error log:\n%s", s)
	}
	assertSize(t, m, 120, 30)
	run(t, m, keyMsg("esc"))
	if m.overlayOpen() {
		t.Error("esc did not close the error log")
	}
}

func TestCleanAttachments(t *testing.T) {
	opts := testOptions(t)
	writeFile(t, opts.Vault, "attachments/used.png", "png")
	writeFile(t, opts.Vault, "attachments/unused.png", "png")
	writeFile(t, opts.Vault, "pics.md", "# Pics\n\n![x](attachments/used.png)\n")
	m := start(t, opts, 120, 30)
	run(t, m, palette.CleanAttachmentsMsg{})
	if s := screen(m); !strings.Contains(s, "Delete 1 unused attachment?") {
		t.Fatalf("confirmation missing:\n%s", s)
	}
	run(t, m, keyMsg("y"))
	if exists(opts.Vault, "attachments/unused.png") {
		t.Error("unused attachment not deleted")
	}
	if !exists(opts.Vault, "attachments/used.png") {
		t.Error("used attachment deleted")
	}
	if !hasToast(m, msgs.ToastInfo, "Deleted 1 unused attachment") {
		t.Errorf("toasts = %v", toastTexts(m))
	}
}

func TestPlaceholderCommands(t *testing.T) {
	tests := []struct {
		msg  tea.Msg
		want string
	}{
		{palette.SyncNowMsg{}, "Sync is not set up yet"},
		{palette.SetupSyncMsg{}, "Sync is not set up yet"},
		{msgs.OpenResolverMsg{}, "No conflicts to resolve"},
		{palette.CleanAttachmentsMsg{}, "No unused attachments"},
		{keyMsg("ctrl+p"), "fuzzy finder is coming soon"},
		{keyMsg("ctrl+f"), "Full-text search is coming soon"},
		{keyMsg("ctrl+s"), "Saving from the editor is coming soon"},
	}
	for _, tt := range tests {
		m := start(t, testOptions(t), 120, 30)
		run(t, m, tt.msg)
		if !hasToast(m, msgs.ToastInfo, tt.want) {
			t.Errorf("%T: toasts = %v, want %q", tt.msg, toastTexts(m), tt.want)
		}
	}
}

func TestOpenConfigRunsEditor(t *testing.T) {
	m := start(t, testOptions(t), 120, 30)
	if _, cmd := m.Update(palette.OpenConfigMsg{}); cmd == nil {
		t.Error("OpenConfigMsg returned no command")
	}
}
