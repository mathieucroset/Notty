package resolver

import (
	"reflect"

	tea "charm.land/bubbletea/v2"

	"github.com/mathieucroset/notty/internal/buffer"
	"github.com/mathieucroset/notty/internal/ui/editor"
	"github.com/mathieucroset/notty/internal/ui/msgs"
)

// acceptEditMsg accepts the embedded editor's text as the result (ctrl+s,
// or the editor's :w).
type acceptEditMsg struct{}

// leaveEditMsg closes the embedded editor keeping its text as a draft (the
// editor's :q).
type leaveEditMsg struct{}

// editStatusMsg carries a vim status message for the edit footer.
type editStatusMsg struct{ text string }

// editNote is shown above the embedded editor.
const editNote = "Editing the result · unresolved blocks were pre-filled with yours"

// openEditor opens the embedded editor on the selected text file's result.
func (m Model) openEditor() (Model, tea.Cmd) {
	it := m.current()
	t := it.text
	cursor := buffer.Pos{}
	if !t.edited && !t.hasDraft && len(t.conflicts) > 0 {
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
	m.ed = m.ed.Load(it.file.Path, t.editText(), cursor)
	m.ed = m.ed.SetSize(m.editorSize()).SetFocused(true)
	m.editing = true
	m.status, m.hint = "", ""
	return m, nil
}

// editKey handles a key while the embedded editor is open: ctrl+s accepts,
// esc leaves (in vim mode only from normal mode), everything else goes to
// the editor.
func (m Model) editKey(k tea.KeyPressMsg) (Model, tea.Cmd) {
	switch k.String() {
	case "ctrl+s":
		return m.acceptEdit()
	case "esc":
		if !m.vim || m.ed.ModeName() == "NORMAL" {
			return m.leaveEdit(), nil
		}
	}
	return m.forwardToEditor(k)
}

// forwardToEditor passes msg to the embedded editor and translates the
// messages its commands produce.
func (m Model) forwardToEditor(msg tea.Msg) (Model, tea.Cmd) {
	var cmd tea.Cmd
	m.ed, cmd = m.ed.Update(msg)
	return m, translate(cmd)
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
		t.hasDraft, t.draft = false, ""
		t.scroll[colResult] = 0
	})
	m.editing = false
	m.ed = m.ed.SetFocused(false)
	m.status = ""
	return m.scrollToCurrent(), nil
}

// leaveEdit closes the editor keeping its text as a draft that the next
// `e` resumes; the result is unchanged.
func (m Model) leaveEdit() Model {
	if !m.editing {
		return m
	}
	text := m.ed.Content()
	m = m.mutateText(func(t *textState) {
		t.hasDraft, t.draft = text != t.baseEditText(), text
		if !t.hasDraft {
			t.draft = ""
		}
	})
	m.editing = false
	m.ed = m.ed.SetFocused(false)
	m.status = ""
	return m
}

// translate wraps the embedded editor's command so the messages it
// produces make sense inside the resolver: a save request (:w, or :wq's
// first half) accepts the edit, :q leaves it, a status message goes to the
// edit footer, and messages addressed to the main editor's surroundings
// (autosave, change notifications, :e, focus moves, image imports, help)
// are dropped. Batches and sequences are translated recursively.
func translate(cmd tea.Cmd) tea.Cmd {
	if cmd == nil {
		return nil
	}
	return func() tea.Msg { return translateMsg(cmd()) }
}

var cmdType = reflect.TypeFor[tea.Cmd]()

func translateMsg(msg tea.Msg) tea.Msg {
	switch msg := msg.(type) {
	case nil:
		return nil
	case msgs.SaveRequestMsg:
		return acceptEditMsg{}
	case msgs.QuitMsg:
		return leaveEditMsg{}
	case editor.StatusMsg:
		return editStatusMsg{text: msg.Text}
	case editor.ChangedMsg, editor.AutosaveTickMsg, msgs.OpenNoteMsg,
		msgs.FocusSidebarMsg, msgs.FocusMainMsg, msgs.ImportImageMsg,
		msgs.OpenHelpMsg, msgs.OpenResolverMsg:
		return nil
	case tea.BatchMsg:
		out := make(tea.BatchMsg, len(msg))
		for i, c := range msg {
			out[i] = translate(c)
		}
		return out
	}
	// tea.Sequence produces an unexported []tea.Cmd type: rebuild it with
	// the same type so Bubble Tea still runs it in order.
	v := reflect.ValueOf(msg)
	if v.Kind() == reflect.Slice && v.Type().Elem() == cmdType {
		out := reflect.MakeSlice(v.Type(), v.Len(), v.Len())
		for i := range v.Len() {
			c, _ := v.Index(i).Interface().(tea.Cmd)
			out.Index(i).Set(reflect.ValueOf(translate(c)))
		}
		return out.Interface()
	}
	return msg
}

// editorSize returns the embedded editor's size: the right pane below the
// file header and the edit note, above the message line.
func (m Model) editorSize() (int, int) {
	_, _, rw := m.split()
	return rw, max(0, m.bodyHeight()-3)
}

// CursorPosition returns the terminal cursor while editing (relative to the
// resolver's top-left corner), or nil.
func (m Model) CursorPosition() *tea.Cursor {
	if !m.editing || m.w <= 0 || m.h <= 0 {
		return nil
	}
	lw, sw, _ := m.split()
	if cl := m.ed.CommandLine(); cl != "" {
		c := tea.NewCursor(lw+sw+min(len([]rune(cl)), m.w-lw-sw-1), 1+m.bodyHeight()-1)
		c.Shape = tea.CursorBar
		return c
	}
	c := m.ed.CursorPosition()
	if c == nil {
		return nil
	}
	c.X += lw + sw
	c.Y += 1 + 2 // title row, then the file header and the edit note
	return c
}
