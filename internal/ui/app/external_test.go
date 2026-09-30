package app

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/mathieucroset/notty/internal/ui/history"
	"github.com/mathieucroset/notty/internal/ui/msgs"
	"github.com/mathieucroset/notty/internal/ui/palette"
)

// syncExec makes tea.ExecProcess and tea.Exec run their command right
// away in the test's command loop.
func syncExec(t *testing.T) {
	t.Helper()
	oldProc, oldCmd := execProcess, execCommand
	execProcess = func(c *exec.Cmd, fn tea.ExecCallback) tea.Cmd {
		return func() tea.Msg { return fn(c.Run()) }
	}
	execCommand = func(c tea.ExecCommand, fn tea.ExecCallback) tea.Cmd {
		return func() tea.Msg { return fn(c.Run()) }
	}
	t.Cleanup(func() { execProcess, execCommand = oldProc, oldCmd })
}

// fakeEditorEnv, when set, makes the test binary act as $EDITOR in the
// mode it names (see TestMain and runFakeEditor).
const fakeEditorEnv = "NOTTY_TEST_FAKE_EDITOR"

// testBinaryEditor returns an editor command that runs this test binary
// as a fake editor in mode. Unlike a shell script, it runs on Windows too.
func testBinaryEditor(t *testing.T, mode string) string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(fakeEditorEnv, mode)
	return `"` + exe + `"`
}

// runFakeEditor is the test binary's main when it runs as $EDITOR on the
// file named by its last argument:
//
//   - "append:<seen>" copies the file to <seen>, then appends a line;
//   - "truncate" replaces the file's content with one line.
func runFakeEditor(mode string, args []string) error {
	if len(args) == 0 {
		return errors.New("no file to edit")
	}
	file := args[len(args)-1]
	if seen, ok := strings.CutPrefix(mode, "append:"); ok {
		b, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		if err := os.WriteFile(seen, b, 0o644); err != nil {
			return err
		}
		return os.WriteFile(file, append(b, "appended by editor\n"...), 0o644)
	}
	if mode == "truncate" {
		return os.WriteFile(file, []byte("one line\n"), 0o644)
	}
	return fmt.Errorf("unknown mode %q", mode)
}

// fakeEditor returns an editor command that appends a line to the file it
// edits and records the content it was given at seenPath.
func fakeEditor(t *testing.T) (cmd, seenPath string) {
	t.Helper()
	seenPath = filepath.Join(t.TempDir(), "seen.txt")
	return testBinaryEditor(t, "append:"+seenPath), seenPath
}

func TestCtrlESavesRunsEditorAndReloads(t *testing.T) {
	syncExec(t)
	opts := testOptions(t)
	script, seen := fakeEditor(t)
	opts.Config.Editor = script
	m := openNote(t, opts, "ideas.md")
	pressKeys(t, m, "G")
	insertText(t, m, "typed ")
	gen := m.kittyGen
	out := run(t, m, keyMsg("ctrl+e"))

	b, err := os.ReadFile(seen)
	if err != nil {
		t.Fatalf("editor did not run: %v", err)
	}
	if !strings.Contains(string(b), "typed ") {
		t.Errorf("editor saw the file before the buffer was saved: %q", b)
	}
	want := "# Ideas\n\ntyped A note app in the terminal.\nappended by editor\n"
	if got := readFile(t, opts.Vault, "ideas.md"); got != want {
		t.Fatalf("file = %q, want %q", got, want)
	}
	if m.editor.Content() != want {
		t.Errorf("buffer not reloaded: %q", m.editor.Content())
	}
	if m.editor.Dirty() {
		t.Error("reloaded buffer is dirty")
	}
	if m.editor.CursorLine() != 2 {
		t.Errorf("cursor line = %d, want it kept on line 2", m.editor.CursorLine())
	}
	if m.kittyGen != gen+1 || !hasMsg[readyTickMsg](out) {
		t.Error("the ready tick was not re-armed after the exec")
	}
}

func TestCtrlEClampsCursorWhenTheFileShrinks(t *testing.T) {
	syncExec(t)
	opts := testOptions(t)
	opts.Config.Editor = testBinaryEditor(t, "truncate")
	m := openNote(t, opts, "ideas.md")
	pressKeys(t, m, "G", "$")
	run(t, m, keyMsg("ctrl+e"))
	if m.editor.Content() != "one line\n" {
		t.Fatalf("buffer = %q", m.editor.Content())
	}
	if c := m.editor.Cursor(); c.Line != 0 || c.Col > len("one line") {
		t.Errorf("cursor not clamped: %+v", c)
	}
}

func TestCtrlEFailedSaveKeepsTheNote(t *testing.T) {
	syncExec(t)
	opts := testOptions(t)
	script, seen := fakeEditor(t)
	opts.Config.Editor = script
	m := openNote(t, opts, "ideas.md")
	insertText(t, m, "x")
	// Make the save fail: a folder where the note should go.
	abs := filepath.Join(opts.Vault.Root, "ideas.md")
	if err := os.Remove(abs); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(abs, 0o755); err != nil {
		t.Fatal(err)
	}
	run(t, m, keyMsg("ctrl+e"))
	if _, err := os.Stat(seen); err == nil {
		t.Error("the editor ran although the save failed")
	}
	if !hasToast(m, msgs.ToastError, "Could not save") || !m.editor.Dirty() {
		t.Errorf("toasts = %v, dirty = %v", toastTexts(m), m.editor.Dirty())
	}
}

func TestHistoryRestoreIntoOpenBuffer(t *testing.T) {
	opts := historyOptions(t)
	m := openNote(t, opts, "ideas.md")
	run(t, m, msgs.OpenHistoryMsg{Path: "ideas.md"})
	if m.history == nil {
		t.Fatalf("history not open: %v", toastTexts(m))
	}
	run(t, m, keyMsg("j"))
	run(t, m, keyMsg("enter"))
	if m.editor.Content() != "# Ideas\n\nsecond version\n" {
		t.Fatalf("buffer = %q", m.editor.Content())
	}
	if got := readFile(t, opts.Vault, "ideas.md"); got != "# Ideas\n\nsecond version\n" {
		t.Errorf("file = %q", got)
	}
	if m.editor.Dirty() || !hasToast(m, msgs.ToastInfo, "Restored ideas") {
		t.Errorf("dirty = %v, toasts = %v", m.editor.Dirty(), toastTexts(m))
	}
	// The restore is one undo step in the buffer.
	run(t, m, keyMsg("u"))
	if m.editor.Content() != "# Ideas\n\nthird version\n" {
		t.Errorf("undo gives %q", m.editor.Content())
	}

	// Restoring a note that is not open still writes the file.
	run(t, m, history.RestoreVersionMsg{Path: "other.md", Rev: "abcdef123", Content: "restored\n"})
	if got := readFile(t, opts.Vault, "other.md"); got != "restored\n" {
		t.Errorf("other.md = %q", got)
	}
}

func TestPaletteTogglesApplyToTheEditor(t *testing.T) {
	m := openNote(t, testOptions(t), "ideas.md")
	run(t, m, palette.ToggleVimMsg{})
	if m.editor.ModeName() != "PLAIN" || !strings.Contains(lastLine(screen(m)), "PLAIN") {
		t.Fatalf("vim off: mode %s", m.editor.ModeName())
	}
	run(t, m, keyMsg("q"))
	if !strings.HasPrefix(m.editor.Content(), "q") {
		t.Errorf("typing with vim off: %q", m.editor.Content())
	}
	run(t, m, palette.ToggleVimMsg{})
	if m.editor.ModeName() != "NORMAL" {
		t.Errorf("vim on: mode %s", m.editor.ModeName())
	}

	run(t, m, palette.ToggleLineNumbersMsg{})
	l := ComputeLayout(120, 30, true)
	row := strings.Split(screen(m), "\n")[1]
	if !strings.Contains(row, " 1 q# Ideas") {
		t.Errorf("no line numbers: %q (content at %d)", row, l.Content.X)
	}
	run(t, m, palette.ToggleLineNumbersMsg{})
	if row = strings.Split(screen(m), "\n")[1]; strings.Contains(row, " 1 q# Ideas") {
		t.Errorf("line numbers still shown: %q", row)
	}
}

func TestThemeChangeRestylesNoteViews(t *testing.T) {
	m := openNote(t, testOptions(t), "ideas.md")
	run(t, m, keyMsg("ctrl+g"))
	before := m.View().Content
	run(t, m, palette.ThemeChosenMsg{Name: "catppuccin-latte"})
	if m.View().Content == before {
		t.Error("theme change did not restyle the screen")
	}
	assertSize(t, m, 120, 30)
	if !strings.Contains(screen(m), "A note app in the terminal.") {
		t.Errorf("preview lost after the theme change:\n%s", screen(m))
	}
}
