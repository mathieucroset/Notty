package editor

import (
	tea "charm.land/bubbletea/v2"

	"github.com/mathieucroset/notty/internal/buffer"
	"github.com/mathieucroset/notty/internal/vim"
)

// KeysIdle reports whether the engine is between commands, so a host may
// take a key for itself (such as alt+m, "move line to note"): always in
// the Plain editor; in vim, in normal, visual or visual-line mode with no
// pending count, operator or register. Unlike CanLeave it is true in
// visual mode.
func (m Model) KeysIdle() bool {
	mc, ok := m.ed.(*vim.Machine)
	if !ok {
		return true
	}
	switch mc.Mode() {
	case vim.Normal, vim.Visual, vim.VisualLine:
		return mc.Pending() == ""
	}
	return false
}

// SelectedLines returns the lines start..end (inclusive) that the
// selection touches, and their text: every line of a vim visual selection
// (charwise or linewise) or of a Plain shift-selection, or the cursor line
// when nothing is selected. A selection ending at column 0 of a line (an
// exclusive end) does not include that line.
func (m Model) SelectedLines() (start, end int, text []string) {
	start, end = m.buf.Cursor().Line, m.buf.Cursor().Line
	if sel, ok := m.ed.Selection(m.buf); ok {
		sel = sel.Normalized()
		start, end = sel.Start.Line, sel.End.Line
		if sel.End.Col == 0 && end > start {
			end--
		}
		end = min(end, m.buf.LineCount()-1)
		start = min(start, end)
	}
	text = make([]string, 0, end-start+1)
	for i := start; i <= end; i++ {
		text = append(text, m.buf.Line(i))
	}
	return start, end, text
}

// DeleteLines deletes lines start..end (inclusive) as one undoable edit,
// provided they still hold want (the text SelectedLines returned when the
// move began), and leaves visual mode or the Plain selection. The cursor
// goes to the start of the line that follows. The returned Cmd reports the
// change and schedules autosave. It reports false, changing nothing, when
// the lines differ from want, or the note is read-only or locked.
func (m Model) DeleteLines(start, end int, want []string) (Model, tea.Cmd, bool) {
	if m.readOnly || m.locked || start < 0 || end < start || end >= m.buf.LineCount() || end-start+1 != len(want) {
		return m, nil, false
	}
	for i, w := range want {
		if m.buf.Line(start+i) != w {
			return m, nil, false
		}
	}
	before, cur := m.buf.Version(), m.buf.Cursor()
	m.ed.Resync(m.buf, func() {
		m.buf.BeginGroupAt(cur)
		m.buf.Delete(wholeLines(m.buf, start, end))
		m.buf.EndGroup()
		m.buf.SetCursor(buffer.Pos{Line: start})
	})
	// Leave visual mode (or the Plain selection): the selected text is
	// gone. Insert mode, reached by keys replayed after a merge, is kept.
	if mc, ok := m.ed.(*vim.Machine); !ok || mc.Mode() == vim.Visual || mc.Mode() == vim.VisualLine {
		m.ed.Reset(m.buf)
	}
	m.aux.forgetMissing()
	m, cmd := m.afterExternal(before)
	return m, cmd, true
}

// wholeLines is the range covering lines l1..l2 and one line break, so
// deleting it removes the lines entirely (an emptied buffer keeps one empty
// line).
func wholeLines(b *buffer.Buffer, l1, l2 int) buffer.Range {
	last := b.LineCount() - 1
	switch {
	case l2 < last:
		return buffer.Range{Start: buffer.Pos{Line: l1}, End: buffer.Pos{Line: l2 + 1}}
	case l1 > 0:
		return buffer.Range{Start: buffer.Pos{Line: l1 - 1, Col: b.LineLen(l1 - 1)}, End: buffer.Pos{Line: l2, Col: b.LineLen(l2)}}
	default:
		return buffer.Range{End: buffer.Pos{Line: l2, Col: b.LineLen(l2)}}
	}
}
