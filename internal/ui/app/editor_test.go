package app

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/mathieucroset/notty/internal/buffer"
	"github.com/mathieucroset/notty/internal/ui/editor"
	"github.com/mathieucroset/notty/internal/ui/msgs"
)

// fakeClipboard keeps tests away from the system clipboard.
type fakeClipboard struct{ text string }

func (c *fakeClipboard) ReadImage() ([]byte, error) { return nil, errors.New("no image") }
func (c *fakeClipboard) ReadText() (string, error)  { return c.text, nil }
func (c *fakeClipboard) WriteText(s string) error {
	c.text = s
	return nil
}

// openNote starts the app and opens rel in the editor.
func openNote(t *testing.T, opts Options, rel string) *Model {
	t.Helper()
	m := start(t, opts, 120, 30)
	run(t, m, msgs.OpenNoteMsg{Path: rel, Line: -1})
	if m.NotePath() != rel {
		t.Fatalf("note %q not open (open: %q)", rel, m.NotePath())
	}
	return m
}

// insertText enters insert mode and pastes text at the cursor, then leaves
// insert mode.
func insertText(t *testing.T, m *Model, text string) {
	t.Helper()
	run(t, m, keyMsg("i"))
	run(t, m, tea.PasteMsg{Content: text})
	run(t, m, keyMsg("esc"))
}

// pressKeys feeds each key in turn.
func pressKeys(t *testing.T, m *Model, ks ...string) {
	t.Helper()
	for _, k := range ks {
		run(t, m, keyMsg(k))
	}
}

func TestOpenTypeSaveWritesFile(t *testing.T) {
	opts := testOptions(t)
	m := openNote(t, opts, "ideas.md")
	if m.Focus() != FocusMain {
		t.Fatal("opening a note did not focus the editor")
	}
	if !strings.Contains(screen(m), "A note app in the terminal.") {
		t.Fatalf("note text not shown:\n%s", screen(m))
	}
	run(t, m, keyMsg("i"))
	if !strings.Contains(screen(m), "INSERT") {
		t.Errorf("mode pill does not say INSERT:\n%s", screen(m))
	}
	run(t, m, tea.PasteMsg{Content: "Hello "})
	if !strings.Contains(screen(m), "●") {
		t.Errorf("no dirty marker after typing:\n%s", screen(m))
	}
	if m.dirtyPath() != "ideas.md" {
		t.Errorf("dirty path = %q", m.dirtyPath())
	}
	run(t, m, keyMsg("esc"))
	pressKeys(t, m, ":", "w")
	if last := lastLine(screen(m)); !strings.HasPrefix(last, ":w") {
		t.Errorf("status row does not show the command line: %q", last)
	}
	if c := m.View().Cursor; c == nil || c.Y != 29 || c.X != 2 {
		t.Errorf("command line cursor = %+v, want (2, 29)", c)
	}
	run(t, m, keyMsg("enter"))
	if got := readFile(t, opts.Vault, "ideas.md"); got != "Hello # Ideas\n\nA note app in the terminal.\n" {
		t.Errorf("file after :w = %q", got)
	}
	if m.editor.Dirty() || strings.Contains(screen(m), "●") || m.dirtyPath() != "" {
		t.Error("buffer still dirty after the save")
	}
	if n, _ := m.ix.Get("ideas.md"); !strings.Contains(n.Content, "Hello") {
		t.Error("saved text not indexed")
	}
}

func lastLine(s string) string {
	lines := strings.Split(s, "\n")
	return lines[len(lines)-1]
}

func TestCtrlSSaves(t *testing.T) {
	opts := testOptions(t)
	m := openNote(t, opts, "ideas.md")
	insertText(t, m, "x")
	run(t, m, keyMsg("ctrl+s"))
	if got := readFile(t, opts.Vault, "ideas.md"); !strings.HasPrefix(got, "x# Ideas") {
		t.Errorf("file after ctrl+s = %q", got)
	}
}

func TestAutosaveTickSaves(t *testing.T) {
	opts := testOptions(t)
	m := openNote(t, opts, "ideas.md")
	insertText(t, m, "auto ")
	// A tick for an older version does nothing.
	run(t, m, editor.AutosaveTickMsg{Path: "ideas.md", Version: m.editor.Version() - 1})
	if got := readFile(t, opts.Vault, "ideas.md"); strings.Contains(got, "auto") {
		t.Fatal("stale autosave tick saved")
	}
	run(t, m, editor.AutosaveTickMsg{Path: "ideas.md", Version: m.editor.Version()})
	if got := readFile(t, opts.Vault, "ideas.md"); !strings.HasPrefix(got, "auto # Ideas") {
		t.Errorf("file after autosave = %q", got)
	}
	if m.editor.Dirty() {
		t.Error("buffer still dirty after autosave")
	}
}

func TestSwitchingNotesSavesAndRemembersCursor(t *testing.T) {
	opts := testOptions(t)
	m := openNote(t, opts, "ideas.md")
	pressKeys(t, m, "j", "j", "w")
	insertText(t, m, "big ")
	cur := m.editor.Cursor()
	run(t, m, msgs.OpenNoteMsg{Path: "Work/Standup notes.md", Line: -1})
	if m.NotePath() != "Work/Standup notes.md" {
		t.Fatalf("open note = %q", m.NotePath())
	}
	if got := readFile(t, opts.Vault, "ideas.md"); got != "# Ideas\n\nA big note app in the terminal.\n" {
		t.Errorf("switching notes did not save the buffer: %q", got)
	}
	if got := opts.Local.Cursor["ideas.md"]; got != [2]int{cur.Line, cur.Col} {
		t.Errorf("remembered cursor = %v, want %v", got, cur)
	}
	if m.editor.Dirty() || strings.Contains(screen(m), "●") {
		t.Error("the new note is shown dirty")
	}
	run(t, m, msgs.OpenNoteMsg{Path: "ideas.md", Line: -1})
	if got := m.editor.Cursor(); got != cur {
		t.Errorf("reopened cursor = %+v, want %+v", got, cur)
	}
}

func TestOpenNoteAtLine(t *testing.T) {
	opts := testOptions(t)
	m := start(t, opts, 120, 30)
	run(t, m, msgs.OpenNoteMsg{Path: "Work/Standup notes.md", Line: 3})
	if got := m.editor.Cursor(); got != (buffer.Pos{Line: 3}) {
		t.Errorf("cursor = %+v, want line 3", got)
	}
	// The note already open only moves the cursor, keeping the buffer.
	insertText(t, m, "kept ")
	run(t, m, msgs.OpenNoteMsg{Path: "Work/Standup notes.md", Line: 0})
	if m.editor.CursorLine() != 0 || !strings.Contains(m.editor.Content(), "kept ") {
		t.Errorf("reopening the open note: line %d content %q", m.editor.CursorLine(), m.editor.Content())
	}
}

func TestWordCountAndTitleFollowTheBuffer(t *testing.T) {
	m := openNote(t, testOptions(t), "ideas.md")
	if !strings.Contains(screen(m), "8 words") {
		t.Fatalf("word count missing:\n%s", lastLine(screen(m)))
	}
	run(t, m, keyMsg("i"))
	run(t, m, tea.PasteMsg{Content: "one two "})
	if !strings.Contains(screen(m), "10 words") {
		t.Errorf("word count not updated: %q", lastLine(screen(m)))
	}
}

func TestEditorCursorOffset(t *testing.T) {
	m := openNote(t, testOptions(t), "ideas.md")
	// Sidebar (28) + main pane border (1) + editor gutter (1); row 1 is
	// the first row inside the pane border.
	c := m.View().Cursor
	if c == nil || c.X != 30 || c.Y != 1 {
		t.Fatalf("cursor = %+v, want (30, 1)", c)
	}
	pressKeys(t, m, "j", "j", "l")
	if c = m.View().Cursor; c == nil || c.X != 31 || c.Y != 3 {
		t.Errorf("cursor after j j l = %+v, want (31, 3)", c)
	}

	// Hidden sidebar: zen layout centers the 80-column content.
	run(t, m, keyMsg("ctrl+b"))
	l := ComputeLayout(120, 30, false)
	if c = m.View().Cursor; c == nil || c.X != l.Content.X+2 || c.Y != 3 {
		t.Errorf("zen cursor = %+v, want (%d, 3)", c, l.Content.X+2)
	}
	run(t, m, keyMsg("ctrl+b"))

	// No cursor while an overlay is open or the sidebar has focus.
	run(t, m, msgs.OpenHelpMsg{})
	if c = m.View().Cursor; c != nil {
		t.Errorf("cursor shown under an overlay: %+v", c)
	}
	run(t, m, keyMsg("esc"))
	run(t, m, msgs.FocusSidebarMsg{})
	if c = m.View().Cursor; c != nil {
		t.Errorf("cursor shown with the sidebar focused: %+v", c)
	}
}

func TestEditorKeyContexts(t *testing.T) {
	m := openNote(t, testOptions(t), "ideas.md")
	if got := m.keyContext().String(); got != "Editor · normal" {
		t.Errorf("context = %s", got)
	}
	run(t, m, keyMsg("i"))
	if got := m.keyContext().String(); got != "Editor · insert" {
		t.Errorf("context in insert mode = %s", got)
	}
	run(t, m, keyMsg("esc"))
	run(t, m, keyMsg("v"))
	if got := m.keyContext().String(); got != "Editor · visual" {
		t.Errorf("context in visual mode = %s", got)
	}
	run(t, m, keyMsg("esc"))
	run(t, m, keyMsg(":"))
	if got := m.keyContext().String(); got != "Editor · command" {
		t.Errorf("context in command mode = %s", got)
	}
	run(t, m, keyMsg("esc"))
	// tab in normal mode focuses the sidebar (the editor asks for it).
	run(t, m, keyMsg("tab"))
	if m.Focus() != FocusSidebar {
		t.Error("tab did not focus the sidebar")
	}
}

func TestPlainEditorContext(t *testing.T) {
	opts := testOptions(t)
	opts.Config.Vim = false
	m := openNote(t, opts, "ideas.md")
	if got := m.keyContext().String(); got != "Editor · vim off" {
		t.Errorf("context = %s", got)
	}
	run(t, m, keyMsg("q"))
	if !strings.HasPrefix(m.editor.Content(), "q# Ideas") {
		t.Errorf("typing q did not insert it: %q", m.editor.Content())
	}
	// esc in the Plain editor focuses the sidebar.
	run(t, m, keyMsg("esc"))
	if m.Focus() != FocusSidebar {
		t.Error("esc did not focus the sidebar")
	}
}

func TestEscDismissesToastOnlyInNormalMode(t *testing.T) {
	m := openNote(t, testOptions(t), "ideas.md")
	run(t, m, msgs.ToastMsg{Level: msgs.ToastError, Text: "boom"})
	run(t, m, keyMsg("i"))
	run(t, m, keyMsg("esc")) // leaves insert mode, keeps the toast
	if m.stickyErrors != 1 || m.editor.ModeName() != "NORMAL" {
		t.Fatalf("esc in insert mode: sticky %d mode %s", m.stickyErrors, m.editor.ModeName())
	}
	run(t, m, keyMsg("d")) // pending operator: esc cancels it first
	run(t, m, keyMsg("esc"))
	if m.stickyErrors != 1 {
		t.Fatal("esc with a pending operator dismissed the toast")
	}
	run(t, m, keyMsg("esc"))
	if m.stickyErrors != 0 {
		t.Error("esc in normal mode did not dismiss the toast")
	}
}

func TestEditorViewExactSize(t *testing.T) {
	for _, size := range [][2]int{{120, 30}, {70, 20}, {160, 45}} {
		m := start(t, testOptions(t), size[0], size[1])
		run(t, m, msgs.OpenNoteMsg{Path: "Work/Standup notes.md", Line: -1})
		assertSize(t, m, size[0], size[1])
		run(t, m, keyMsg(":"))
		assertSize(t, m, size[0], size[1])
	}
}

func TestEditorStatusMessage(t *testing.T) {
	m := openNote(t, testOptions(t), "ideas.md")
	pressKeys(t, m, ":", "x", "y", "z", "enter")
	if last := lastLine(screen(m)); strings.Contains(last, "NORMAL") {
		t.Errorf("unknown command message not shown: %q", last)
	}
	run(t, m, keyMsg("j"))
	if last := lastLine(screen(m)); !strings.Contains(last, "NORMAL") {
		t.Errorf("message not cleared by the next key: %q", last)
	}
}
