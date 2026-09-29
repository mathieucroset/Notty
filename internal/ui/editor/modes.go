package editor

import (
	tea "charm.land/bubbletea/v2"

	"github.com/mathieucroset/notty/internal/buffer"
	"github.com/mathieucroset/notty/internal/tasks"
	"github.com/mathieucroset/notty/internal/ui/msgs"
	"github.com/mathieucroset/notty/internal/vim"
)

// autosave emits msgs.SaveRequestMsg when the tick still matches the buffer
// (no change since it was scheduled) and there is something to save. It
// never saves a read-only (conflicted) note or during the merge lock.
func (m Model) autosave(t AutosaveTickMsg) tea.Cmd {
	if t.Path != m.path || t.Version != m.buf.Version() || !m.buf.Dirty() || m.readOnly || m.locked {
		return nil
	}
	return emit(msgs.SaveRequestMsg{})
}

// SetLocked locks or unlocks the editor for the merge window (spec §7).
// While locked, key presses and pastes are queued and autosave pauses.
// SetLocked(false) is Unlock(true) with its Cmd discarded; the app should
// call Unlock to get the Cmd and the dropped count.
func (m Model) SetLocked(locked bool) Model {
	if locked {
		m.locked = true
		return m
	}
	m, _, _ = m.Unlock(true)
	return m
}

// Locked reports whether the editor is locked.
func (m Model) Locked() bool { return m.locked }

// Unlock ends the merge window. With replay the queued keys and pastes are
// applied in order and the resulting commands batched; otherwise they are
// dropped and their count returned (for the "N keys dropped" toast). A
// dirty buffer gets a fresh autosave tick, since ticks were ignored while
// locked.
func (m Model) Unlock(replay bool) (Model, tea.Cmd, int) {
	m.locked = false
	queue := m.queue
	m.queue = nil
	var cmds []tea.Cmd
	dropped := 0
	if replay {
		for _, msg := range queue {
			var cmd tea.Cmd
			m, cmd = m.Update(msg)
			cmds = append(cmds, cmd)
		}
	} else {
		dropped = len(queue)
	}
	if m.buf.Dirty() {
		cmds = append(cmds, m.ChangeCmd())
	}
	return m, tea.Batch(cmds...), dropped
}

// ApplyToggle toggles the task identified by (line, lineText) from outside
// the editor (spec §5 "Toggling a task from outside the editor"): if the
// line number drifted the task is found by its text. The toggle is one undo
// step. It reports false when the task is not found or the note is
// read-only; the app then shows a warning toast. The app runs ChangeCmd
// afterwards so the change is autosaved.
func (m Model) ApplyToggle(line int, lineText string) (Model, bool) {
	if m.readOnly {
		return m, false
	}
	lines := m.aux.linesOf(m.buf)
	i, ok := tasks.FindLine(lines, line, lineText)
	if !ok {
		return m, false
	}
	cur := m.buf.Cursor()
	m.buf.BeginGroupAt(cur)
	m.buf.Replace(buffer.Range{Start: buffer.Pos{Line: i}, End: buffer.Pos{Line: i, Col: m.buf.LineLen(i)}}, tasks.ToggleLine(lines[i]))
	m.buf.EndGroup()
	m.buf.SetCursor(cur)
	m.syncStyle()
	return m.ensureVisible(), true
}

// InsertText inserts text on its own line at the cursor (used by the app
// after an image import to insert "![](/attachments/...)"): it replaces the
// cursor line when that line is blank, otherwise it goes on a new line
// below. The insertion is one undo step. The app runs ChangeCmd afterwards.
func (m Model) InsertText(text string) Model {
	if m.readOnly || text == "" {
		return m
	}
	cur := m.buf.Cursor()
	m.buf.BeginGroupAt(cur)
	var end buffer.Pos
	if isBlank(m.buf.Line(cur.Line)) {
		end = m.buf.Replace(buffer.Range{Start: buffer.Pos{Line: cur.Line}, End: buffer.Pos{Line: cur.Line, Col: m.buf.LineLen(cur.Line)}}, text)
	} else {
		end = m.buf.Insert(buffer.Pos{Line: cur.Line, Col: m.buf.LineLen(cur.Line)}, "\n"+text)
	}
	m.buf.EndGroup()
	if m.typing() {
		m.buf.SetCursor(end)
	} else {
		m.buf.SetCursor(buffer.Pos{Line: end.Line})
	}
	m.aux.forgetMissing()
	m.syncStyle()
	return m.ensureVisible()
}

// typing reports whether the engine is in a text-entry mode (insert mode or
// the Plain editor).
func (m Model) typing() bool {
	if mc, ok := m.ed.(*vim.Machine); ok {
		return mc.Mode() == vim.Insert
	}
	return true
}

func isBlank(s string) bool {
	for _, r := range s {
		if r != ' ' && r != '\t' {
			return false
		}
	}
	return true
}
