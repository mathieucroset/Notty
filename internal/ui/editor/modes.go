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
	return m.emit(msgs.SaveRequestMsg{})
}

// Lock locks the editor for the merge window (spec §7): key presses,
// pastes, clipboard text, task toggles and text insertions are queued and
// autosave pauses. Unlock is the only way out.
func (m Model) Lock() Model {
	m.locked = true
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

// toggleMsg and insertTextMsg are ApplyToggle and InsertText calls queued
// during the merge lock and replayed by Unlock.
type toggleMsg struct {
	line int
	text string
}

type insertTextMsg struct{ text string }

// ApplyToggle toggles the task identified by (line, lineText) from outside
// the editor (spec §5 "Toggling a task from outside the editor"): if the
// line number drifted the task is found by its text. The toggle is its own
// undo step and the returned Cmd reports the change and schedules autosave.
// It reports false when the task is not found or the note is read-only; the
// app then shows a warning toast. While locked the toggle is queued (and
// reported as done); if it fails on replay, the editor emits the toast.
func (m Model) ApplyToggle(line int, lineText string) (Model, tea.Cmd, bool) {
	if m.readOnly {
		return m, nil, false
	}
	if m.locked {
		m.queue = append(m.queue, toggleMsg{line: line, text: lineText})
		return m, nil, true
	}
	lines := m.aux.linesOf(m.buf)
	i, ok := tasks.FindLine(lines, line, lineText)
	if !ok {
		return m, nil, false
	}
	before, cur := m.buf.Version(), m.buf.Cursor()
	m.ed.Resync(m.buf, func() {
		m.buf.BeginGroupAt(cur)
		m.buf.Replace(buffer.Range{Start: buffer.Pos{Line: i}, End: buffer.Pos{Line: i, Col: m.buf.LineLen(i)}}, tasks.ToggleLine(lines[i]))
		m.buf.EndGroup()
		m.buf.SetCursor(cur)
	})
	m, cmd := m.afterExternal(before)
	return m, cmd, true
}

// InsertText inserts text on its own line at the cursor (used by the app
// after an image import to insert "![](/attachments/...)"): it replaces the
// cursor line when that line is blank, otherwise it goes on a new line
// below. The insertion is its own undo step and the returned Cmd reports
// the change and schedules autosave. While locked it is queued.
func (m Model) InsertText(text string) (Model, tea.Cmd) {
	if m.readOnly || text == "" {
		return m, nil
	}
	if m.locked {
		m.queue = append(m.queue, insertTextMsg{text: text})
		return m, nil
	}
	before, cur := m.buf.Version(), m.buf.Cursor()
	typing := m.typing()
	m.ed.Resync(m.buf, func() {
		m.buf.BeginGroupAt(cur)
		var end buffer.Pos
		if isBlank(m.buf.Line(cur.Line)) {
			end = m.buf.Replace(buffer.Range{Start: buffer.Pos{Line: cur.Line}, End: buffer.Pos{Line: cur.Line, Col: m.buf.LineLen(cur.Line)}}, text)
		} else {
			end = m.buf.Insert(buffer.Pos{Line: cur.Line, Col: m.buf.LineLen(cur.Line)}, "\n"+text)
		}
		m.buf.EndGroup()
		if typing {
			m.buf.SetCursor(end)
		} else {
			m.buf.SetCursor(buffer.Pos{Line: end.Line})
		}
	})
	m.aux.forgetMissing()
	return m.afterExternal(before)
}

// ReplaceAll replaces the whole text as one undoable edit that leaves the
// buffer dirty, so autosave writes it (the app restores a history version
// this way when the note is open, spec §8). The cursor stays on its line
// when that line still exists. Identical text changes nothing; a read-only
// or locked note is left alone.
func (m Model) ReplaceAll(content string) (Model, tea.Cmd) {
	if m.readOnly || m.locked || content == m.buf.String() {
		return m, nil
	}
	before, cur := m.buf.Version(), m.buf.Cursor()
	m.ed.Resync(m.buf, func() {
		m.buf.BeginGroupAt(cur)
		m.buf.SetText(content)
		m.buf.EndGroup()
		m.buf.SetCursor(cur)
	})
	m.aux.forgetMissing()
	return m.afterExternal(before)
}

// SetCursor moves the cursor to p, clamped to the text (the app jumps to a
// search hit in the note already open this way), and scrolls it into view.
// The engine keeps its mode.
func (m Model) SetCursor(p buffer.Pos) Model {
	m.ed.Resync(m.buf, func() { m.buf.SetCursor(p) })
	return m.ensureVisible()
}

// SetPath renames the loaded note (after a rename or move made in the
// app), keeping the buffer, its undo history and its dirty state.
func (m Model) SetPath(path string) Model {
	m.path = path
	return m
}

// afterExternal restyles and reports a change made outside the engine.
func (m Model) afterExternal(before uint64) (Model, tea.Cmd) {
	var cmd tea.Cmd
	if m.buf.Version() != before {
		m.syncStyle()
		cmd = m.ChangeCmd()
	}
	return m.ensureVisible(), cmd
}

// replayToggle applies a toggle queued during the lock.
func (m Model) replayToggle(msg toggleMsg) (Model, tea.Cmd) {
	m, cmd, ok := m.ApplyToggle(msg.line, msg.text)
	if !ok {
		return m, m.emit(msgs.ToastMsg{Level: msgs.ToastWarn, Text: "Task not found: " + msg.text})
	}
	return m, cmd
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
