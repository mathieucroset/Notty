package resolver

import (
	"sync/atomic"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/mathieucroset/notty/internal/buffer"
	"github.com/mathieucroset/notty/internal/ui/editor"
	"github.com/mathieucroset/notty/internal/ui/msgs"
)

// editorMsg is the envelope for the embedded editor's own messages (its
// clipboard reads, flash timer and status messages): the app hands it back
// to Update (Owns reports true) and the resolver unwraps it for the editor.
type editorMsg struct{ inner tea.Msg }

// editSignals records the editor's save (:w) and quit (:q) requests. The
// editor maps its requests synchronously inside Update (editor.Options.
// MapMsg), so the resolver acts on them before the next key arrives. It is
// shared by every copy of the Model.
type editSignals struct{ save, quit atomic.Bool }

// mapMsg is the embedded editor's MapMsg: requests become signals, messages
// meant for the main editor's surroundings (change notifications,
// autosave, :e, focus moves, image imports, help, resolver) are dropped,
// toasts go to the app as they are, and the rest is wrapped in editorMsg.
func (s *editSignals) mapMsg(msg tea.Msg) tea.Msg {
	switch msg.(type) {
	case msgs.SaveRequestMsg:
		s.save.Store(true)
		return nil
	case msgs.QuitMsg:
		s.quit.Store(true)
		return nil
	case editor.ChangedMsg, editor.AutosaveTickMsg, msgs.OpenNoteMsg,
		msgs.FocusSidebarMsg, msgs.FocusMainMsg, msgs.ImportImageMsg,
		msgs.OpenHelpMsg, msgs.OpenResolverMsg:
		return nil
	case msgs.ToastMsg:
		return msg
	}
	return editorMsg{inner: msg}
}

// editNote is shown above the embedded editor.
const editNote = "Editing the result · unresolved blocks were pre-filled with yours"

// openEditor opens the embedded editor on the selected text file's result.
func (m Model) openEditor() (Model, tea.Cmd) {
	it := m.current()
	t := it.text
	cursor := buffer.Pos{}
	if !t.edited && len(t.conflicts) > 0 {
		// Start on the current conflict: count the result lines before it.
		n := 0
		for i := range t.conflicts[t.cur] {
			b := t.blocks[i]
			if k := t.conflictIndex(i); k >= 0 {
				n += len(chosen(b, t.choices[k], b.Ours))
			} else {
				n += len(resolvedLines(b))
			}
		}
		cursor.Line = n
	}
	m.ed = m.ed.Load(it.file.Path, t.baseEditText(), cursor)
	m.ed = m.ed.SetSize(m.editorSize()).SetFocused(true)
	m.editing = true
	m.status, m.hint = "", ""
	return m, nil
}

// editKey handles a key while the embedded editor is open: ctrl+s accepts,
// esc leaves when the editor has nothing left to cancel (vim: normal mode
// with no pending command), everything else goes to the editor.
func (m Model) editKey(k tea.KeyPressMsg) (Model, tea.Cmd) {
	switch k.String() {
	case "ctrl+s":
		return m.acceptEdit()
	case "esc":
		if m.ed.CanLeave() {
			return m.leaveEdit(), nil
		}
	}
	return m.forwardToEditor(k)
}

// forwardToEditor passes msg to the embedded editor, then acts on the save
// or quit request it made, if any (":wq" accepts, which also leaves).
func (m Model) forwardToEditor(msg tea.Msg) (Model, tea.Cmd) {
	m.sig.save.Store(false)
	m.sig.quit.Store(false)
	var cmd tea.Cmd
	m.ed, cmd = m.ed.Update(msg)
	save, quit := m.sig.save.Swap(false), m.sig.quit.Swap(false)
	switch {
	case save:
		m, _ = m.acceptEdit()
	case quit:
		m = m.leaveEdit()
	}
	return m, cmd
}

// editorMessage handles an editorMsg while editing.
func (m Model) editorMessage(msg editorMsg) (Model, tea.Cmd) {
	if !m.editing {
		return m, nil
	}
	if st, ok := msg.inner.(editor.StatusMsg); ok {
		m.status = st.Text
		return m, nil
	}
	return m.forwardToEditor(msg.inner)
}

// acceptEdit makes the editor's text the result: every block counts as
// resolved.
func (m Model) acceptEdit() (Model, tea.Cmd) {
	if !m.editing {
		return m, nil
	}
	text := m.ed.Content()
	m = m.mutateText(func(t *textState) {
		t.edited, t.editedText = true, text
		t.scroll[colResult] = 0
	})
	return m.closeEditor().scrollToCurrent(), nil
}

// leaveEdit closes the editor (esc, :q) keeping the edits: text that
// differs from what the editor opened with becomes the result, as with
// ctrl+s; unchanged text leaves everything as it was (an unresolved file
// stays unresolved).
func (m Model) leaveEdit() Model {
	if !m.editing {
		return m
	}
	if it := m.current(); it != nil && it.text != nil && m.ed.Content() != it.text.baseEditText() {
		m, _ = m.acceptEdit()
		return m
	}
	return m.closeEditor()
}

// closeEditor returns to the navigation context.
func (m Model) closeEditor() Model {
	m.editing = false
	m.ed = m.ed.SetFocused(false)
	m.status = ""
	return m
}

// editorSize returns the embedded editor's size: the right pane below the
// file header and the edit note, above the message line.
func (m Model) editorSize() (int, int) {
	_, rw := m.split()
	return rw, max(0, m.bodyHeight()-3)
}

// CursorPosition returns the terminal cursor while editing (relative to the
// resolver's top-left corner), or nil.
func (m Model) CursorPosition() *tea.Cursor {
	if !m.editing || m.w <= 0 || m.h <= 0 {
		return nil
	}
	x := m.rightX()
	_, rw := m.split()
	if cl := m.ed.CommandLine(); cl != "" {
		// The message line is the right pane's last row.
		c := tea.NewCursor(x+min(ansi.StringWidth(cl), max(0, rw-1)), 1+m.bodyHeight()-1)
		c.Shape = tea.CursorBar
		return c
	}
	c := m.ed.CursorPosition()
	if c == nil {
		return nil
	}
	c.X += x
	c.Y += 1 + 2 // the pane's top border, then the file header and the edit note
	return c
}
