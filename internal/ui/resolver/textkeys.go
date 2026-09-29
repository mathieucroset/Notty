package resolver

import (
	tea "charm.land/bubbletea/v2"
)

// Hints shown under a text file.
const (
	hintUnresolved = "Resolve every conflict first: o yours · t theirs · b both · e edit"
	hintEdited     = "The result was edited · u discards the edit · e edits again"
)

// mutateText applies f to a copy of the selected file's text state.
func (m Model) mutateText(f func(t *textState)) Model {
	return m.mutate(m.sel, func(it *item) {
		if it.text == nil {
			return
		}
		it.text = it.text.clone()
		f(it.text)
	})
}

// textKey handles a navigation key on an unresolved text file.
func (m Model) textKey(s string) (Model, tea.Cmd) {
	it := m.current()
	t := it.text
	m.hint = ""
	switch s {
	case "o", "t", "b":
		if t.edited {
			m.hint = hintEdited
			return m, nil
		}
		c := map[string]choice{"o": chooseOurs, "t": chooseTheirs, "b": chooseBoth}[s]
		m = m.mutateText(func(t *textState) {
			t.setChoice(c)
			t.cur = t.nextUnresolved()
		})
		return m.scrollToCurrent(), nil
	case "u":
		m = m.mutateText(func(t *textState) {
			if t.edited {
				t.edited, t.editedText = false, ""
				return
			}
			t.setChoice(unset)
		})
		return m.scrollToCurrent(), nil
	case "e":
		return m.openEditor()
	case "enter":
		if !t.allResolved() {
			m.hint = hintUnresolved
			return m, nil
		}
		content := t.content()
		if msg := validate(it.file.Path, content); msg != "" {
			return m.SetError(it.file.Path, msg), nil
		}
		path := it.file.Path
		m = m.mutate(m.sel, func(it *item) { it.pending, it.err = true, "" })
		return m, emit(ResolveTextMsg{Path: path, Content: []byte(content)})
	case "tab":
		if m.isNarrow() {
			m.narrow = (m.narrow + 1) % 3
		}
		return m, nil
	case "ctrl+d", "pgdown":
		return m.scrollText(max(1, m.columnHeight()/2)), nil
	case "ctrl+u", "pgup":
		return m.scrollText(-max(1, m.columnHeight()/2)), nil
	}
	return m, nil
}

// jump moves to the next (d = 1) or previous (d = -1) conflict block of
// the selected text file and scrolls it into view.
func (m Model) jump(d int) Model {
	it := m.current()
	if it == nil || it.text == nil || len(it.text.conflicts) == 0 {
		return m
	}
	m.hint = ""
	m = m.mutateText(func(t *textState) {
		t.cur = min(max(t.cur+d, 0), len(t.conflicts)-1)
	})
	return m.scrollToCurrent()
}

// scrollText scrolls the selected text file's columns by d lines.
func (m Model) scrollText(d int) Model {
	h := m.columnHeight()
	return m.mutateText(func(t *textState) { t.scrollBy(d, h) })
}

// scrollToCurrent scrolls the selected text file so its current conflict
// is visible.
func (m Model) scrollToCurrent() Model {
	it := m.current()
	if it == nil || it.text == nil {
		return m
	}
	h := m.columnHeight()
	return m.mutateText(func(t *textState) { t.scrollTo(h) })
}
