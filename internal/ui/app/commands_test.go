package app

import (
	"os"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

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
	if m.overlay == nil || m.overlay.kind != overlayPalette {
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
	if m.overlay == nil || m.overlay.kind != overlayDialog || m.overlay.pending.kind != opNewNote {
		t.Fatalf("new note dialog not open: %+v", m.overlay)
	}
	if m.overlay.pending.path != "" {
		t.Errorf("palette new note targets %q, want the vault root", m.overlay.pending.path)
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
	if m.overlay == nil || m.overlay.kind != overlayHelp {
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
	if m.overlay == nil || m.overlay.kind != overlayHelp {
		t.Error("? did not open help")
	}
}

func TestErrorLogOverlay(t *testing.T) {
	m := start(t, testOptions(t), 120, 30)
	run(t, m, msgs.ToastMsg{Level: msgs.ToastError, Text: "Disk on fire"})
	run(t, m, keyMsg("!"))
	if m.overlay == nil || m.overlay.kind != overlayLog {
		t.Fatal("! did not open the error log")
	}
	s := screen(m)
	if !strings.Contains(s, "Error log") || !strings.Contains(s, "Disk on fire") {
		t.Errorf("error log content missing:\n%s", s)
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
