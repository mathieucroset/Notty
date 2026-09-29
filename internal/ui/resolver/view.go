package resolver

import (
	"fmt"
	"strings"
	"unicode"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/mathieucroset/notty/internal/ui/icons"
	"github.com/mathieucroset/notty/internal/ui/textutil"
	"github.com/mathieucroset/notty/internal/ui/theme"
)

// Layout constants.
const (
	listWidth     = 28  // file list width on wide terminals
	narrowBelow   = 100 // below this width only one text column is shown
	minSplitWidth = 40  // below this width the file list is hidden
	previewGap    = 2   // columns between the two binary previews
	tabWidth      = 4   // spaces a tab is shown as
)

var columnTitles = [...]string{"Yours", "Theirs", "Result"}

// Footer hints per screen.
const (
	footerText     = "]c/[c next/prev · o yours · t theirs · b both · u unset · e edit · enter resolve · j/k files · esc close"
	footerTextTab  = "]c/[c next/prev · o/t/b choose · u unset · e edit · tab column · enter resolve · j/k files · esc close"
	footerChoice   = "1-%d choose · enter confirm · j/k files · esc close"
	footerResolved = "j/k files · esc close"
	footerEdit     = "ctrl+s accept · esc done (keeps the edits)"
)

// split returns the widths of the file list, the separator and the right
// pane.
func (m Model) split() (listW, sepW, rightW int) {
	switch {
	case m.w < minSplitWidth:
		return 0, 0, m.w
	case m.w < narrowBelow:
		listW = min(listWidth, max(16, m.w/4))
	default:
		listW = listWidth
	}
	return listW, 1, m.w - listW - 1
}

// isNarrow reports whether text files show a single column.
func (m Model) isNarrow() bool { return m.w < narrowBelow }

// bodyHeight is the height below the title row and above the footer.
func (m Model) bodyHeight() int { return max(0, m.h-2) }

// contentHeight is the right pane's height below the file header and above
// the message line.
func (m Model) contentHeight() int { return max(0, m.bodyHeight()-2) }

// columnHeight is the height of a text column's lines (below the column
// titles).
func (m Model) columnHeight() int { return max(0, m.contentHeight()-1) }

// View renders the resolver at exactly the configured size.
func (m Model) View() string {
	if m.w <= 0 || m.h <= 0 {
		return ""
	}
	rows := []string{textutil.PadLine(m.title(), m.w)}
	if m.h >= 3 {
		rows = append(rows, m.body()...)
	}
	if m.h >= 2 {
		rows = append(rows, textutil.PadLine(m.styles.Muted.Render(m.footer()), m.w))
	}
	return strings.Join(rows, "\n")
}

func (m Model) title() string {
	done := 0
	for _, it := range m.items {
		if it.file.Resolved {
			done++
		}
	}
	return m.styles.DialogTitle.Render("Resolve conflicts") +
		m.styles.Muted.Render(fmt.Sprintf("  %d of %d files resolved", done, len(m.items)))
}

func (m Model) footer() string {
	it := m.current()
	switch {
	case m.editing:
		return footerEdit
	case it == nil || it.file.Resolved:
		return footerResolved
	case it.text != nil && m.isNarrow():
		return footerTextTab
	case it.text != nil:
		return footerText
	}
	return fmt.Sprintf(footerChoice, len(it.choice.options))
}

func (m Model) body() []string {
	h := m.bodyHeight()
	listW, sepW, rightW := m.split()
	right := textutil.FitBlock(m.rightPane(rightW, h), rightW, h)
	if listW == 0 {
		return right
	}
	list := textutil.FitBlock(m.fileList(listW, h), listW, h)
	sep := m.styles.Muted.Render(strings.Repeat("│", sepW))
	out := make([]string, h)
	for i := range h {
		out[i] = list[i] + sep + right[i]
	}
	return out
}

// kindIcon is the file list's icon for a file kind.
func kindIcon(set icons.Set, k FileKind) string {
	switch k {
	case Binary:
		return set.KindBinary
	case ModifyDelete:
		return set.KindModifyDelete
	case PathConflict:
		return set.KindPath
	}
	return set.KindText
}

// fileList renders the file list, keeping the selection in view.
func (m Model) fileList(w, h int) []string {
	type entry struct {
		rows []string
	}
	selStyle := m.styles.SidebarSelectedFocused
	var entries []entry
	for i, it := range m.items {
		mark := m.styles.Warning.Render(m.styles.Icons.Dirty)
		if it.file.Resolved {
			mark = m.styles.Success.Render(m.styles.Icons.Check)
		}
		name := truncateLeft(it.file.Path, max(1, w-6))
		row := " " + mark + " " + kindIcon(m.styles.Icons, it.file.Kind) + " "
		if i == m.sel {
			row = selStyle.Render(" ") + mark + selStyle.Render(" "+kindIcon(m.styles.Icons, it.file.Kind)+" "+textutil.PadLine(name, max(0, w-5)))
		} else {
			row += name
		}
		e := entry{rows: []string{row}}
		if it.err != "" {
			e.rows = append(e.rows, m.styles.Error.Render("   "+m.styles.Icons.Error+" "+sanitize(it.err)))
		}
		entries = append(entries, e)
	}
	out := []string{m.styles.Section.Render(" " + theme.SectionTitle("Files"))}
	// Window the entries so the selected one is visible.
	avail := h - 1
	start, used := 0, 0
	for i := 0; i <= m.sel && i < len(entries); i++ {
		used += len(entries[i].rows)
		for used > avail && start < i {
			used -= len(entries[start].rows)
			start++
		}
	}
	for i := start; i < len(entries) && len(out) < h; i++ {
		out = append(out, entries[i].rows...)
	}
	return out
}

// truncateLeft shortens s to w cells, eliding its start.
func truncateLeft(s string, w int) string {
	s = sanitize(s)
	if ansi.StringWidth(s) <= w {
		return s
	}
	r := []rune(s)
	for len(r) > 0 && ansi.StringWidth("…"+string(r)) > w {
		r = r[1:]
	}
	return "…" + string(r)
}

// sanitize makes file content safe to draw: tabs become spaces, a CR
// before the line end is dropped, and other control characters (including
// ESC, so no escape sequence gets through) become "·".
func sanitize(s string) string {
	s = strings.TrimSuffix(s, "\r")
	if !strings.ContainsFunc(s, unicode.IsControl) {
		return s
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '\t':
			b.WriteString(strings.Repeat(" ", tabWidth))
		case unicode.IsControl(r):
			b.WriteRune('·')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// rightPane renders the selected file's resolution UI: a header, the
// content and a message line.
func (m Model) rightPane(w, h int) []string {
	it := m.current()
	if it == nil {
		return []string{"", m.styles.Muted.Render(" No conflicts.")}
	}
	ch := max(0, h-2)
	var content []string
	switch {
	case it.file.Resolved:
		content = []string{"", m.styles.Success.Render(" " + m.styles.Icons.Check + " This file is resolved.")}
	case m.editing:
		content = append([]string{m.styles.Muted.Render(" " + editNote)}, strings.Split(m.ed.View(), "\n")...)
	case it.text != nil:
		content = m.textContent(it.text, w, ch)
	default:
		content = m.choiceContent(it, w, ch)
	}
	rows := []string{m.fileHeader(it)}
	rows = append(rows, textutil.FitBlock(content, w, ch)...)
	if h >= 2 {
		rows = append(rows, m.messageLine(it))
	}
	return rows
}

// fileHeader is the right pane's first row.
func (m Model) fileHeader(it *item) string {
	s := m.styles.PaneTitleFocused.Render(" "+sanitize(it.file.Path)) + "  "
	switch {
	case it.file.Resolved:
		return s + m.styles.Success.Render("resolved")
	case it.text != nil:
		t := it.text
		detail := fmt.Sprintf("%d of %d conflicts resolved", t.resolvedCount(), len(t.conflicts))
		if t.edited {
			detail += " · edited"
		}
		if t.twoWay {
			detail += " · created on both sides"
		}
		if m.editing {
			detail += " · " + m.ed.ModeName()
		}
		return s + m.styles.Muted.Render(detail)
	}
	return s + m.styles.Muted.Render(kindLabel(it.file.Kind))
}

func kindLabel(k FileKind) string {
	switch k {
	case Binary:
		return "binary file"
	case ModifyDelete:
		return "edited / deleted"
	case PathConflict:
		return "path conflict"
	}
	return "text file"
}

// messageLine is the right pane's last row: the editor's command line or
// status, an error, a pending request, a hint, or the next step.
func (m Model) messageLine(it *item) string {
	switch {
	case m.editing:
		if cl := m.ed.CommandLine(); cl != "" {
			return cl
		}
		return m.styles.Muted.Render(" " + m.status)
	case it.err != "":
		return m.styles.Error.Render(" " + m.styles.Icons.Error + " " + sanitize(it.err))
	case it.pending:
		return m.styles.Muted.Render(" Saving…")
	case m.hint != "":
		return m.styles.Warning.Render(" " + m.hint)
	case it.file.Resolved:
		return ""
	case it.text != nil && it.text.allResolved():
		return m.styles.Success.Render(" All conflicts resolved · enter writes the result")
	case it.text != nil:
		return ""
	}
	return m.styles.Muted.Render(fmt.Sprintf(" Press 1-%d to choose, enter to confirm", len(it.choice.options)))
}

// textContent renders the column titles and the text columns.
func (m Model) textContent(t *textState, w, h int) []string {
	colH := max(0, h-1)
	if m.isNarrow() {
		c := m.narrow
		title := m.columnTitle(c, true) + m.styles.Muted.Render("  tab: "+nextTitles(c))
		return append([]string{title}, m.columnRows(t, c, w, colH)...)
	}
	avail := max(0, w-2)
	widths := [3]int{avail / 3, avail / 3, avail / 3}
	for i := range avail % 3 {
		widths[i]++
	}
	sep := m.styles.Muted.Render("│")
	var cols [3][]string
	title := ""
	for c := colOurs; c <= colResult; c++ {
		cols[c] = m.columnRows(t, c, widths[c], colH)
		title += textutil.PadLine(m.columnTitle(c, c == colResult), widths[c])
		if c < colResult {
			title += sep
		}
	}
	out := []string{title}
	for i := range colH {
		out = append(out, cols[0][i]+sep+cols[1][i]+sep+cols[2][i])
	}
	return out
}

func (m Model) columnTitle(c column, focused bool) string {
	st := m.styles.PaneTitle
	if focused {
		st = m.styles.PaneTitleFocused
	}
	return st.Render(" " + columnTitles[c])
}

// nextTitles lists the other columns in tab order.
func nextTitles(c column) string {
	return columnTitles[(c+1)%3] + " › " + columnTitles[(c+2)%3]
}

// columnRows renders h rows of column c, each exactly w cells wide.
func (m Model) columnRows(t *textState, c column, w, h int) []string {
	lines := t.columnLines(c)
	top := clampScroll(t.scroll[c], len(lines), h)
	cur := lipgloss.NewStyle().Background(m.palette.Warning).Foreground(m.palette.Base)
	out := make([]string, h)
	blank := strings.Repeat(" ", max(0, w))
	for i := range h {
		n := top + i
		if n >= len(lines) || w <= 0 {
			out[i] = blank
			continue
		}
		l := lines[n]
		text := textutil.PadLine(" "+sanitize(l.text), max(0, w-1))
		if l.conflict < 0 {
			out[i] = " " + text
			continue
		}
		current := l.conflict == t.cur
		gutter := m.styles.Muted
		switch {
		case current:
			gutter = m.styles.Warning
		case l.kind == lineResolved:
			gutter = m.styles.Success
		case l.kind == lineUnresolved:
			gutter = m.styles.Error
		}
		body := text
		switch {
		case l.kind == lineUnresolved && current:
			body = m.styles.Error.Bold(true).Render(text)
		case l.kind == lineUnresolved:
			body = m.styles.Error.Render(text)
		case current:
			body = cur.Render(text)
		case l.kind == lineEmpty:
			body = m.styles.Muted.Render(text)
		}
		out[i] = gutter.Render("▌") + body
	}
	return out
}
