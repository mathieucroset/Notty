package preview

import (
	"slices"

	tea "charm.land/bubbletea/v2"

	"github.com/mathieucroset/notty/internal/ui/msgs"
)

// handleKey implements the preview view keys (spec §4.4).
func (m Model) handleKey(k tea.KeyPressMsg) (Model, tea.Cmd) {
	s := k.String()
	prefix := m.pending
	m.pending = ""
	switch prefix {
	case "g":
		if s == "g" {
			m.offset = 0
			return m, nil
		}
	case "]", "[":
		dir := 1
		if prefix == "[" {
			dir = -1
		}
		switch s {
		case "t":
			m.moveTask(dir)
			return m, nil
		case "i":
			m.moveImage(dir)
			return m, nil
		}
	}

	switch s {
	case "j", "down":
		m.scroll(1)
	case "k", "up":
		m.scroll(-1)
	case "ctrl+d":
		m.scroll(max(1, m.height/2))
	case "ctrl+u":
		m.scroll(-max(1, m.height/2))
	case "G", "end":
		m.offset = m.totalLines()
		m.clampOffset()
	case "home":
		m.offset = 0
	case "g", "]", "[":
		m.pending = s
	case "space":
		return m, m.toggleTask()
	case "enter":
		return m, m.openImage()
	case "tab":
		return m, emit(msgs.FocusSidebarMsg{})
	case "?":
		return m, emit(msgs.OpenHelpMsg{})
	}
	return m, nil
}

func emit(msg tea.Msg) tea.Cmd {
	return func() tea.Msg { return msg }
}

func (m *Model) scroll(n int) {
	m.offset += n
	m.clampOffset()
}

// ensureVisible scrolls so rows [row, row+n) are on screen, favoring the
// first row when they do not fit.
func (m *Model) ensureVisible(row, n int) {
	if row < 0 {
		return
	}
	if row+n > m.offset+m.height {
		m.offset = row + n - m.height
	}
	if row < m.offset {
		m.offset = row
	}
	m.clampOffset()
}

// pick moves a highlight over items at the given rows. From no highlight,
// forward starts at the first item at or below the top of the view and
// backward at the last one above its bottom.
func (m Model) pick(cur, dir int, rows []int) int {
	n := len(rows)
	if n == 0 {
		return -1
	}
	if cur >= 0 {
		return max(0, min(n-1, cur+dir))
	}
	if dir > 0 {
		for i, r := range rows {
			if r >= m.offset {
				return i
			}
		}
		return n - 1
	}
	for i := n - 1; i >= 0; i-- {
		if rows[i] < m.offset+m.height {
			return i
		}
	}
	return 0
}

func (m *Model) moveTask(dir int) {
	if m.doc == nil {
		return
	}
	m.taskIdx = m.pick(m.taskIdx, dir, m.doc.taskRows)
	if m.taskIdx >= 0 {
		m.imgIdx = -1
		m.ensureVisible(m.doc.taskRows[m.taskIdx], 1)
	}
}

func (m *Model) moveImage(dir int) {
	if m.doc == nil {
		return
	}
	m.imgIdx = m.pick(m.imgIdx, dir, m.doc.imgTop)
	if m.imgIdx >= 0 {
		m.taskIdx = -1
		m.ensureVisible(m.doc.imgTop[m.imgIdx], m.doc.imgRows[m.imgIdx])
	}
}

// toggleTask asks the app to toggle the highlighted task, identified by its
// line in the rendered content and the raw line text.
func (m Model) toggleTask() tea.Cmd {
	if m.doc == nil || m.taskIdx < 0 || m.taskIdx >= len(m.doc.tasks) {
		return nil
	}
	t := m.doc.tasks[m.taskIdx]
	return emit(msgs.ToggleTaskMsg{Path: m.doc.notePath, Line: t.Line, Text: t.Raw})
}

// openImage opens the image viewer on every resolved image of the note,
// starting at the highlighted one.
func (m Model) openImage() tea.Cmd {
	if m.doc == nil || m.imgIdx < 0 || m.imgIdx >= len(m.doc.pathIdx) {
		return nil
	}
	idx := m.doc.pathIdx[m.imgIdx]
	if idx < 0 {
		return nil
	}
	return emit(msgs.OpenImageViewerMsg{Paths: slices.Clone(m.doc.paths), Index: idx})
}
