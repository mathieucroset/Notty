package finder

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// View renders the overlay box at exactly its configured size (SetSize): a
// rounded border, a header with the mode label, the query input, a status
// line, the results list (left) and note preview (right), and a footer with
// key hints. Every line is exactly m.width columns (ANSI-aware) and the
// whole box is exactly m.height lines.
func (m Model) View() string {
	w, h := m.width, m.height
	if w <= 0 || h <= 0 {
		return ""
	}

	border := lipgloss.NewStyle().Foreground(m.palette.Accent)
	cw := max(w-2, 0)
	wrap := func(s string) string { return border.Render("│") + padLine(s, cw) + border.Render("│") }
	blankRow := wrap(m.bgStyle(lipgloss.NewStyle(), false).Render(strings.Repeat(" ", cw)))

	lines := make([]string, 0, h)
	lines = append(lines, border.Render("╭"+strings.Repeat("─", cw)+"╮"))
	lines = append(lines, wrap(m.headerLine()))
	lines = append(lines, wrap(m.inputLine()))
	lines = append(lines, wrap(m.statusLine()))
	lines = append(lines, m.bodyRows(cw, wrap)...)
	lines = append(lines, wrap(m.footerLine()))
	lines = append(lines, border.Render("╰"+strings.Repeat("─", cw)+"╯"))

	for len(lines) < h {
		lines = append(lines, blankRow)
	}
	if len(lines) > h {
		lines = lines[:h]
	}
	return strings.Join(lines, "\n")
}

func (m Model) headerLine() string {
	label := "Find note"
	if m.mode == FullText {
		label = "Search"
	}
	return " " + m.bgStyle(m.styles.DialogTitle, false).Render(label)
}

func (m Model) inputLine() string {
	prompt := m.bgStyle(m.styles.Accent, false).Render("› ")
	text := m.bgStyle(lipgloss.NewStyle(), false).Render(m.query)
	cursor := m.bgStyle(m.styles.Accent, false).Render("▏")
	return " " + prompt + text + cursor
}

func (m Model) statusLine() string {
	text := m.fuzzyStatus()
	if m.mode == FullText {
		text = m.fullTextStatus()
	}
	return " " + m.bgStyle(m.styles.Muted, false).Render(text)
}

func (m Model) footerLine() string {
	return " " + m.bgStyle(m.styles.Muted, false).Render("enter open · esc close · ctrl+j/k move")
}

// paneWidths splits the body's content width cw between the list pane and
// the preview pane, with a one-column separator between them.
func (m Model) paneWidths(cw int) (listW, previewW int) {
	listW = cw * listWidthPct / 100
	if listW < 1 {
		listW = min(1, cw)
	}
	previewW = max(cw-listW-1, 0)
	if previewW == 0 {
		listW = cw
	}
	return listW, previewW
}

// bodyRows renders the body: the results list on the left (listWidthPct of
// cw), a one-column separator, and the note preview on the right.
func (m Model) bodyRows(cw int, wrap func(string) string) []string {
	bodyH := m.bodyHeight()
	if bodyH <= 0 {
		return nil
	}

	listW, previewW := m.paneWidths(cw)
	listLines := m.listPane(listW, bodyH)
	previewLines := m.previewPaneLines(previewW, bodyH)
	sep := lipgloss.NewStyle().Foreground(m.palette.Muted).Inherit(m.surface).Render("│")

	out := make([]string, bodyH)
	for i := range out {
		row := padLine(listLines[i], listW)
		if previewW > 0 {
			row += sep + padLine(previewLines[i], previewW)
		}
		out[i] = wrap(row)
	}
	return out
}

// listPane renders the results list body: itemLines lines per row, windowed
// by m.offset, with a hint row when there are no results.
func (m Model) listPane(width, height int) []string {
	blank := m.bgStyle(lipgloss.NewStyle(), false).Render(strings.Repeat(" ", max(width, 0)))
	out := make([]string, 0, height)

	n, rows := m.itemCount(), height/itemLines
	for r := 0; r < rows; r++ {
		i := m.offset + r
		if i >= n {
			break
		}
		l1, l2 := m.resultLines(i, width)
		out = append(out, l1, l2)
	}
	if n == 0 {
		out = append(out, padLine(" "+m.bgStyle(m.styles.Muted, false).Render(m.emptyHint()), width))
	}
	for len(out) < height {
		out = append(out, blank)
	}
	return out[:height]
}

func (m Model) emptyHint() string {
	if !m.isEmptyQuery() {
		return "no matches"
	}
	if m.mode == Fuzzy {
		return "no recent notes"
	}
	return "type to search"
}

// resultLines dispatches to the mode-specific row renderer for result i.
func (m Model) resultLines(i, width int) (string, string) {
	selected := i == m.cursor
	if m.mode == Fuzzy {
		return m.fuzzyItemLines(i, width, selected)
	}
	return m.hitItemLines(i, width, selected)
}
