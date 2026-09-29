package app

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/mathieucroset/notty/internal/ui/preview"
	"github.com/mathieucroset/notty/internal/ui/textutil"
)

// readyDelay is how long after start (and after every tea.Exec) Kitty
// transmissions wait when the terminal does not answer the keyboard
// enhancements query (kitty spike, condition 1).
const readyDelay = 100 * time.Millisecond

// readyTickMsg releases the preview's Kitty transmissions. gen drops a tick
// armed before the latest tea.Exec.
type readyTickMsg struct{ gen int }

// readyTickCmd arms the ready tick for the current generation.
func (m *Model) readyTickCmd() tea.Cmd {
	gen := m.kittyGen
	return tea.Tick(readyDelay, func(time.Time) tea.Msg { return readyTickMsg{gen: gen} })
}

// terminalReady tells the preview the terminal is on the alternate screen.
func (m *Model) terminalReady() tea.Cmd {
	var cmd tea.Cmd
	m.preview, cmd = m.preview.SetTerminalReady()
	return cmd
}

// afterExec holds Kitty transmissions after a tea.Exec, which leaves and
// re-enters the alternate screen and so wipes the terminal's image store,
// and re-arms the ready tick that re-transmits them.
func (m *Model) afterExec() tea.Cmd {
	m.preview, _ = m.preview.ResetKittyState()
	m.kittyGen++
	return m.readyTickCmd()
}

// splitWidths divides the content width w between the editor and the
// preview of split view, leaving one column for the divider.
func splitWidths(w int) (editorW, previewW int) {
	if w < 3 {
		return max(w, 0), 0
	}
	editorW = (w - 1) / 2
	return editorW, w - 1 - editorW
}

// sizeNoteViews sizes the editor and the preview for the note view.
func (m *Model) sizeNoteViews(w, h int) {
	switch m.noteView {
	case ViewSplit:
		ew, pw := splitWidths(w)
		m.editor = m.editor.SetSize(ew, h)
		m.preview = m.preview.SetMode(preview.ModeSplit).SetSize(pw, h)
	case ViewPreview:
		m.editor = m.editor.SetSize(w, h)
		m.preview = m.preview.SetMode(preview.ModeFull).SetSize(w, h)
	default:
		m.editor = m.editor.SetSize(w, h)
	}
	var cmd tea.Cmd
	m.preview, cmd = m.preview.Refresh()
	m.later(cmd)
	m.followCursor()
}

// previewShown reports whether the preview is on screen.
func (m *Model) previewShown() bool {
	return m.mainView == ViewNote && m.note.path != "" && m.noteView != ViewEditor
}

// syncPreview hands the unsaved buffer to the preview when it is on
// screen (spec §4.1: the preview never reads the file on disk).
func (m *Model) syncPreview() tea.Cmd {
	if !m.previewShown() {
		return nil
	}
	var cmd tea.Cmd
	m.preview, cmd = m.preview.SetContent(m.editor.Path(), m.editor.Content())
	return cmd
}

// followCursor keeps the split preview on the editor's cursor line.
func (m *Model) followCursor() {
	if m.noteView == ViewSplit && m.previewShown() {
		m.preview = m.preview.FollowLine(m.editor.CursorLine())
	}
}

// cycleNoteView switches editor → split → preview → editor (ctrl+g).
func (m *Model) cycleNoteView() tea.Cmd {
	m.noteView = (m.noteView + 1) % 3
	m.relayout()
	return m.syncPreview()
}

// updatePreview forwards a message to the preview's render pipeline.
func (m *Model) updatePreview(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	m.preview, cmd = m.preview.Update(msg)
	m.followCursor()
	return cmd
}

// syncOverlayFlag tells the preview whether an overlay covers it, so it
// shows chips instead of images the dimming would spoil (spec §6.4).
func (m *Model) syncOverlayFlag() {
	m.preview = m.preview.SetOverlayOpen(m.overlayOpen())
}

// later queues a command for Update to return.
func (m *Model) later(cmd tea.Cmd) {
	if cmd != nil {
		m.deferred = append(m.deferred, cmd)
	}
}

// noteContent renders the open note at w×h in the current note view.
func (m *Model) noteContent(w, h int) string {
	switch m.noteView {
	case ViewSplit:
		ew, pw := splitWidths(w)
		left := textutil.FitBlock(strings.Split(m.editor.View(), "\n"), ew, h)
		right := textutil.FitBlock(strings.Split(m.preview.View(), "\n"), pw, h)
		div := m.opts.Styles.Muted.Render("│")
		rows := make([]string, h)
		for i := range rows {
			rows[i] = left[i] + div + right[i]
		}
		return strings.Join(rows, "\n")
	case ViewPreview:
		return m.preview.View()
	}
	return m.editor.View()
}
