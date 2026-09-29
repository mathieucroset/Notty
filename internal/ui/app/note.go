package app

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/mathieucroset/notty/internal/buffer"
	"github.com/mathieucroset/notty/internal/clipboard"
	"github.com/mathieucroset/notty/internal/ui/editor"
	"github.com/mathieucroset/notty/internal/ui/keys"
	"github.com/mathieucroset/notty/internal/ui/msgs"
	"github.com/mathieucroset/notty/internal/ui/textutil"
	"github.com/mathieucroset/notty/internal/vault"
)

// newEditor builds the note editor from the configuration.
func newEditor(opts Options) editor.Model {
	root := ""
	if opts.Vault != nil {
		root = opts.Vault.Root
	}
	clip := opts.Clipboard
	if clip == nil {
		clip = clipboard.Default()
	}
	return editor.New(editor.Options{
		Vim:         opts.Config.Vim,
		LineNumbers: opts.Config.LineNumbers,
		AutosaveMS:  opts.Config.AutosaveMS,
		Styles:      opts.Styles,
		Palette:     opts.Palette,
		Clipboard:   clip,
		VaultRoot:   root,
	})
}

// openNote opens the note at msg.Path. The note already open only moves
// its cursor; any other note is read from disk, after the open buffer is
// saved (spec §4.1: autosave on note switch) and its cursor remembered.
func (m *Model) openNote(msg msgs.OpenNoteMsg) tea.Cmd {
	if m.opts.Vault == nil || msg.Path == "" {
		return nil
	}
	m.openSeq++ // drop any load still in flight
	if msg.Path == m.note.path {
		if msg.Line >= 0 {
			m.editor = m.editor.SetCursor(buffer.Pos{Line: msg.Line})
		}
		m.mainView = ViewNote
		m.sidebar.Select(msg.Path)
		m.setFocus(FocusMain)
		return nil
	}
	return tea.Batch(m.leaveNote(), loadNoteCmd(m.opts.Vault, msg.Path, msg.Line, m.openSeq))
}

// leaveNote remembers the open note's cursor and saves its buffer when it
// has unsaved changes.
func (m *Model) leaveNote() tea.Cmd {
	if m.note.path == "" {
		return nil
	}
	m.rememberCursor()
	if !m.editor.Dirty() {
		return nil
	}
	return m.saveEditorCmd()
}

// rememberCursor records the open note's cursor in the local state, so
// reopening the note puts it back.
func (m *Model) rememberCursor() {
	if m.note.path == "" {
		return
	}
	if m.opts.Local.Cursor == nil {
		m.opts.Local.Cursor = map[string][2]int{}
	}
	c := m.editor.Cursor()
	m.opts.Local.Cursor[m.note.path] = [2]int{c.Line, c.Col}
}

// saveEditorCmd saves a snapshot of the editor buffer.
func (m *Model) saveEditorCmd() tea.Cmd {
	p, content, version := m.editor.Snapshot()
	if p == "" || m.editor.ModeName() == "READ-ONLY" {
		return nil
	}
	return m.saveNoteCmd(p, content, version)
}

// saveRequest saves the buffer on ctrl+s, :w or autosave.
func (m *Model) saveRequest() tea.Cmd {
	if m.note.path == "" || !m.editor.Dirty() {
		return nil
	}
	return m.saveEditorCmd()
}

// closeNote empties the main pane, dropping the buffer.
func (m *Model) closeNote() {
	m.note = note{}
	m.openSeq++ // drop any load still in flight
	m.editor = m.editor.SetReadOnly(false, "").Load("", "", buffer.Pos{})
	m.editorStatus = ""
	m.sidebar.SetDirty("")
}

// dirtyPath is the path to mark with ● in the sidebar, or "".
func (m *Model) dirtyPath() string {
	if m.editor.Dirty() {
		return m.editor.Path()
	}
	return ""
}

// handleEditorChanged follows a buffer change: the index, the pane title,
// the word count and the dirty markers.
func (m *Model) handleEditorChanged(msg editor.ChangedMsg) tea.Cmd {
	if msg.Path == "" || msg.Path != m.editor.Path() {
		return nil
	}
	content := m.editor.Content()
	if m.ix != nil {
		m.ix.UpdateContent(msg.Path, content)
	}
	m.note.title = vault.Title(content, msg.Path)
	m.note.words = len(strings.Fields(content))
	m.sidebar.SetDirty(m.dirtyPath())
	return nil
}

// editorFocused reports whether keys go to the editor: a note is open in
// the editor or split view and the main pane has focus.
func (m *Model) editorFocused() bool {
	return m.focus == FocusMain && m.mainView == ViewNote && m.note.path != "" && m.noteView != ViewPreview
}

// editorContext maps the editor's mode to its key context.
func (m *Model) editorContext() keys.Context {
	switch m.editor.ModeName() {
	case "INSERT":
		return keys.EditorInsert
	case "VISUAL", "V-LINE":
		return keys.EditorVisual
	case "COMMAND":
		return keys.EditorCommand
	case "PLAIN":
		return keys.EditorPlain
	case "READ-ONLY":
		return keys.EditorReadOnly
	}
	return keys.EditorNormal
}

// handleEditorKey sends a key to the editor. In vim normal mode with
// nothing pending, esc dismisses the newest sticky error first; it never
// does while typing.
func (m *Model) handleEditorKey(k tea.KeyPressMsg) tea.Cmd {
	if k.String() == "esc" && m.stickyErrors > 0 && m.editor.CanLeave() &&
		(m.editor.ModeName() == "NORMAL" || m.editor.ModeName() == "READ-ONLY") {
		m.dismissToast()
		return nil
	}
	m.editorStatus = ""
	var cmd tea.Cmd
	m.editor, cmd = m.editor.Update(k)
	return cmd
}

// handlePaste sends a bracketed paste to the editor when it has focus.
func (m *Model) handlePaste(msg tea.PasteMsg) tea.Cmd {
	if m.overlayOpen() || m.history != nil || m.opts.WizardNeeded || !m.editorFocused() {
		return nil
	}
	var cmd tea.Cmd
	m.editor, cmd = m.editor.Update(msg)
	return cmd
}

// modeLabel is the status bar's mode pill.
func (m *Model) modeLabel() string {
	if m.note.path != "" && m.mainView == ViewNote {
		return m.editor.ModeName()
	}
	if m.opts.Config.Vim {
		return "NORMAL"
	}
	return "PLAIN"
}

// statusRow renders the bottom row: the editor's command line or message
// when there is one, otherwise the status bar.
func (m *Model) statusRow() string {
	if line := m.commandLine(); line != "" {
		return m.opts.Styles.StatusBar.Render(textutil.PadLine(line, m.width))
	}
	if m.editorStatus != "" && m.note.path != "" && m.mainView == ViewNote {
		return m.opts.Styles.StatusBar.Render(textutil.PadLine(" "+m.editorStatus, m.width))
	}
	// Fill a copy of the status bar: rendering never changes the model.
	status := m.status
	status.Mode = m.modeLabel()
	status.Path = m.note.path
	if m.indexing {
		status.Busy = "indexing…"
	}
	status.Words = m.note.words
	status.Sync = m.sync
	return status.View()
}

// commandLine is the vim ":" or "/" line being typed, or "".
func (m *Model) commandLine() string {
	if !m.editorFocused() {
		return ""
	}
	return strings.NewReplacer("\n", " ", "\t", " ").Replace(m.editor.CommandLine())
}

// cursor places the terminal cursor: on the command line while one is
// typed, otherwise at the editor's cursor, offset to screen coordinates.
// There is none while an overlay or full-screen view is open, or when the
// editor does not have focus.
func (m *Model) cursor() *tea.Cursor {
	if m.width <= 0 || m.height <= 0 || m.opts.WizardNeeded || m.overlayOpen() || m.history != nil || !m.editorFocused() {
		return nil
	}
	if line := m.commandLine(); line != "" {
		return tea.NewCursor(min(ansi.StringWidth(line), m.width-1), m.height-1)
	}
	c := m.editor.CursorPosition()
	if c == nil {
		return nil
	}
	x, y := m.editorOrigin()
	c.X += x
	c.Y += y
	return c
}

// editorOrigin is the screen position of the editor's top-left cell.
func (m *Model) editorOrigin() (int, int) {
	l := ComputeLayout(m.width, m.height, m.sidebarVisible)
	return l.Content.X, l.Content.Y
}
