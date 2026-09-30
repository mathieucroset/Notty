package app

import (
	"fmt"
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
func newEditor(opts Options, mapMsg func(tea.Msg) tea.Msg) editor.Model {
	clip := clipboardOf(opts)
	return editor.New(editor.Options{
		Vim:         opts.Config.Vim,
		LineNumbers: opts.Config.LineNumbers,
		AutosaveMS:  opts.Config.AutosaveMS,
		Styles:      opts.Styles,
		Palette:     opts.Palette,
		Clipboard:   clip,
		VaultRoot:   vaultRoot(opts),
		MapMsg:      mapMsg,
	})
}

// clipboardOf is the clipboard the editors paste from.
func clipboardOf(opts Options) editor.Clipboard {
	if opts.Clipboard != nil {
		return opts.Clipboard
	}
	return clipboard.Default()
}

// editorClipboard is the clipboard the editors paste from.
func (m *Model) editorClipboard() editor.Clipboard { return clipboardOf(m.opts) }

// mapEditorMsg intercepts the editor's focus requests, which it makes
// synchronously inside Update, so they apply before the next key is
// routed. Every other message passes through untouched (timer messages
// are mapped on other goroutines, so this must not touch the model for
// them).
func (m *Model) mapEditorMsg(msg tea.Msg) tea.Msg {
	var f Focus
	switch msg.(type) {
	case msgs.FocusSidebarMsg:
		f = FocusSidebar
	case msgs.FocusMainMsg:
		f = FocusMain
	default:
		return msg
	}
	m.focusReq = &f
	return nil
}

// updateEditor feeds msg to the editor, then applies any focus change it
// asked for.
func (m *Model) updateEditor(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	m.editor, cmd = m.editor.Update(msg)
	if f := m.focusReq; f != nil {
		m.focusReq = nil
		if *f == FocusSidebar {
			m.focusSidebar()
		} else {
			m.setFocus(FocusMain)
		}
	}
	return cmd
}

// openNote opens the note at msg.Path. The note already open only moves
// its cursor; any other note is read from disk, after the open buffer is
// saved (spec §4.1: autosave on note switch) and its cursor remembered.
func (m *Model) openNote(msg msgs.OpenNoteMsg) tea.Cmd {
	if m.opts.Vault == nil || msg.Path == "" {
		return nil
	}
	m.closeOverlayKind(overlayFinder) // the finder's choice
	m.openSeq++                       // drop any load still in flight
	if msg.Path == m.note.path {
		if msg.Line >= 0 {
			m.editor = m.editor.SetCursor(buffer.Pos{Line: msg.Line})
		}
		m.mainView = ViewNote
		m.sidebar.Select(msg.Path)
		m.setFocus(FocusMain)
		return nil
	}
	m.rememberCursor()
	load := loadNoteMsg{path: msg.Path, line: msg.Line, seq: m.openSeq}
	if cmd := m.saveThen(load, "it stays open"); cmd != nil {
		return cmd // the new note loads once the old one is saved
	}
	return m.loadNote(load)
}

// loadNoteMsg asks to read a note from disk for open request seq.
type loadNoteMsg struct {
	path string
	line int
	seq  int
}

// loadNote reads the note of a still current open request.
func (m *Model) loadNote(msg loadNoteMsg) tea.Cmd {
	if msg.seq != m.openSeq || m.opts.Vault == nil {
		return nil // superseded by a newer open request
	}
	return loadNoteCmd(m.opts.Vault, msg.path, msg.line, msg.seq)
}

// savedThenMsg reports the save of the open buffer that must succeed
// before next is handled; abort says what did not happen if it failed.
type savedThenMsg struct {
	saved savedMsg
	next  tea.Msg
	abort string
}

// saveThen saves the dirty buffer and then delivers next; it returns nil
// when there is nothing to save. A failed save drops next with an error
// toast ("Could not save X, so <abort>").
func (m *Model) saveThen(next tea.Msg, abort string) tea.Cmd {
	if !m.editor.Dirty() {
		return nil
	}
	save := m.saveEditorCmd()
	if save == nil {
		return nil
	}
	return func() tea.Msg {
		res, _ := save().(savedMsg)
		return savedThenMsg{saved: res, next: next, abort: abort}
	}
}

// handleSavedThen goes on with the action a save was guarding, or keeps
// the note open when the save failed.
func (m *Model) handleSavedThen(msg savedThenMsg) tea.Cmd {
	if msg.saved.err != nil {
		ask, _ := m.changedOnDisk(msg.saved)
		return tea.Batch(m.pushToast(msgs.ToastError, fmt.Sprintf("Could not save %s, so %s: %v",
			msg.saved.path, msg.abort, msg.saved.err)), ask)
	}
	return tea.Batch(m.handleSaved(msg.saved), emit(msg.next))
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
	if m.extConflict == m.editor.Path() {
		return nil // the "changed on disk" dialog decides first
	}
	return m.saveEditorCmd()
}

// closeNote empties the main pane, dropping the buffer.
func (m *Model) closeNote() {
	m.note = note{}
	m.openSeq++ // drop any load still in flight
	m.editor = m.editor.SetReadOnly(false, "").Load("", "", buffer.Pos{})
	m.editorStatus = ""
	m.extConflict = ""
	m.setBaseline("")
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
	// New edits after a failed quit: the next quit must try to save them.
	m.discardOnQuit = false
	content := m.editor.Content()
	if m.ix != nil {
		m.ix.UpdateContent(msg.Path, content)
	}
	m.note.title = vault.Title(content, msg.Path)
	m.note.words = len(strings.Fields(content))
	m.sidebar.SetDirty(m.dirtyPath())
	if m.mainView == ViewTasks {
		// A task toggled in the Tasks view shows its new state now.
		m.refreshTasks()
	}
	cmd := m.syncPreview()
	m.followCursor()
	return cmd
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
	cmd := m.updateEditor(k)
	m.followCursor()
	return cmd
}

// handlePaste sends a bracketed paste to the editor when it has focus.
func (m *Model) handlePaste(msg tea.PasteMsg) tea.Cmd {
	if m.wizard != nil {
		return m.updateWizard(msg)
	}
	if m.resolver != nil && !m.overlayOpen() {
		return m.updateResolver(msg)
	}
	if m.overlayOpen() || m.history != nil || m.opts.WizardNeeded || !m.editorFocused() {
		return nil
	}
	return m.updateEditor(msg)
}

// modeLabel is the status bar's mode pill.
func (m *Model) modeLabel() string {
	if m.note.path != "" && m.mainView == ViewNote {
		if m.noteView == ViewPreview {
			return "PREVIEW"
		}
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
	if m.quitStatus != "" {
		status.Busy = m.quitStatus
	}
	status.Words = m.note.words
	status.Sync = m.sync
	status.Update = m.updateVersion
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
	if m.resolver != nil && !m.overlayOpen() && m.width > 0 && m.height > 0 {
		return m.resolver.CursorPosition()
	}
	if m.width <= 0 || m.height <= 0 || m.opts.WizardNeeded || m.overlayOpen() || m.history != nil || m.resolver != nil ||
		!m.editorFocused() {
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
