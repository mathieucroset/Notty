package help

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/mathieucroset/notty/internal/ui/theme"
)

// widthPct and heightPct size the overlay relative to the terminal (spec:
// "large overlay ~90% x 85%").
const (
	widthPct  = 90
	heightPct = 85
	// twoColumnMinWidth is the terminal width at or above which the help
	// overlay renders in two columns instead of one.
	twoColumnMinWidth = 100
)

// CloseMsg asks the app to close the help overlay.
type CloseMsg struct{}

// Model is the full-screen help overlay (spec §4.4).
type Model struct {
	sections []Section
	styles   theme.Styles

	termWidth int
	width     int
	height    int
	offset    int // first visible content line, for scrolling

	// content is the full, unscrolled set of rendered lines for the current
	// size. It is rebuilt only in SetSize, not on every scroll key press.
	content []string
}

// New creates a help overlay from Sections().
func New(styles theme.Styles) Model {
	return Model{sections: Sections(), styles: styles}
}

// SetSize sizes the overlay for a termW x termH terminal: about 90% wide and
// 85% tall, but never wider or taller than the terminal itself.
func (m Model) SetSize(termW, termH int) Model {
	m.termWidth = termW
	m.width = clampInt(termW*widthPct/100, 1, max(termW, 1))
	m.height = clampInt(termH*heightPct/100, 1, max(termH, 1))
	m.content = m.renderContent()
	m.offset = clampInt(m.offset, 0, m.maxOffset())
	return m
}

// Update handles a key press: j/k/pgup/pgdn/g/G scroll, esc/q/F1 close.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	switch k.String() {
	case "esc", "q", "f1":
		return m, func() tea.Msg { return CloseMsg{} }
	case "j", "down":
		m.offset = clampInt(m.offset+1, 0, m.maxOffset())
	case "k", "up":
		m.offset = clampInt(m.offset-1, 0, m.maxOffset())
	case "pgdown", "ctrl+f":
		m.offset = clampInt(m.offset+m.innerHeight(), 0, m.maxOffset())
	case "pgup", "ctrl+b":
		m.offset = clampInt(m.offset-m.innerHeight(), 0, m.maxOffset())
	case "g":
		m.offset = 0
	case "G":
		m.offset = m.maxOffset()
	}
	return m, nil
}

// View renders the currently visible slice of the help content.
func (m Model) View() string {
	viewport := m.innerHeight()
	width := m.innerWidth()

	start := clampInt(m.offset, 0, max(len(m.content)-1, 0))
	end := min(start+viewport, len(m.content))
	visible := append([]string(nil), m.content[start:end]...)
	for len(visible) < viewport {
		visible = append(visible, padLine("", width))
	}
	return m.styles.Dialog.Width(m.width).Height(m.height).Render(strings.Join(visible, "\n"))
}

// renderContent builds the full (unscrolled) list of content lines, in one
// or two columns depending on the terminal width.
func (m Model) renderContent() []string {
	width := m.innerWidth()
	if m.termWidth >= twoColumnMinWidth {
		return m.twoColumn(width)
	}
	return m.oneColumn(width)
}

func (m Model) oneColumn(width int) []string {
	var lines []string
	for _, sec := range m.sections {
		lines = append(lines, formatSection(sec, m.styles, width)...)
	}
	return lines
}

func (m Model) twoColumn(width int) []string {
	colWidth := max((width-1)/2, 1)

	formatted := make([][]string, len(m.sections))
	total := 0
	for i, sec := range m.sections {
		formatted[i] = formatSection(sec, m.styles, colWidth)
		total += len(formatted[i])
	}

	half := total / 2
	var left, right []string
	acc := 0
	inLeft := true
	for _, f := range formatted {
		if inLeft && acc >= half {
			inLeft = false
		}
		if inLeft {
			left = append(left, f...)
		} else {
			right = append(right, f...)
		}
		acc += len(f)
	}

	h := max(len(left), len(right))
	for len(left) < h {
		left = append(left, padLine("", colWidth))
	}
	for len(right) < h {
		right = append(right, padLine("", colWidth))
	}

	lines := make([]string, h)
	for i := 0; i < h; i++ {
		lines[i] = padLine(left[i], colWidth) + " " + padLine(right[i], colWidth)
	}
	return lines
}

// formatSection renders sec's title and rows to lines of exactly width,
// with the key column styled Accent and the description left plain. The
// section title uses SidebarSection, and a blank line follows for spacing.
//
// The key column's width is the longest Keys string in the section, capped
// at a fraction of width so a single long multi-key label (e.g. vim text
// objects: "iw aw i\" a\" i( a( ip ap") can't blow out every row's
// alignment. A row whose Keys text is still wider than that cap is never
// truncated — see formatRow.
func formatSection(sec Section, styles theme.Styles, width int) []string {
	keyWidth := sectionKeyWidth(sec, width)

	lines := []string{padLine(styles.SidebarSection.Render(sec.Title), width)}
	for _, r := range sec.Rows {
		lines = append(lines, formatRow(r, styles, width, keyWidth)...)
	}
	lines = append(lines, padLine("", width))
	return lines
}

// sectionKeyWidth is sec's key column width: the longest Keys string in the
// section, capped at 40% of width (so it never crowds out the description
// column) and floored at 4.
func sectionKeyWidth(sec Section, width int) int {
	capWidth := width * 2 / 5
	if capWidth < 6 {
		capWidth = 6
	}

	longest := 0
	for _, r := range sec.Rows {
		if w := ansi.StringWidth(r.Keys); w > longest {
			longest = w
		}
	}

	switch {
	case longest > capWidth:
		return capWidth
	case longest < 4:
		return 4
	default:
		return longest
	}
}

// formatRow renders one Row to a single line of exactly width when its
// Keys text fits within keyWidth. When it doesn't, the key label is never
// silently truncated: instead the full Keys text gets its own line, padded
// to width, and the description follows on the next line, indented to
// where it would otherwise start.
func formatRow(r Row, styles theme.Styles, width, keyWidth int) []string {
	if ansi.StringWidth(r.Keys) <= keyWidth {
		line := styles.Accent.Render(padRight(r.Keys, keyWidth)) + " " + r.Desc
		return []string{padLine(line, width)}
	}
	keyLine := padLine(styles.Accent.Render(r.Keys), width)
	descLine := padLine(strings.Repeat(" ", keyWidth+1)+r.Desc, width)
	return []string{keyLine, descLine}
}

// innerWidth is the width available for content inside the Dialog style's
// border and padding.
func (m Model) innerWidth() int {
	w := m.width - 2 - 4 // border (2) + horizontal padding (2x2)
	if w < 1 {
		w = 1
	}
	return w
}

// innerHeight is the number of content lines that fit inside the Dialog
// style's border and padding.
func (m Model) innerHeight() int {
	h := m.height - 2 - 2 // border (2) + vertical padding (2)
	if h < 1 {
		h = 1
	}
	return h
}

// maxOffset is the largest scroll offset that still shows a full viewport
// of content (or 0, if the content is shorter than the viewport).
func (m Model) maxOffset() int {
	max := len(m.content) - m.innerHeight()
	if max < 0 {
		max = 0
	}
	return max
}

// padRight pads or truncates s (which may contain ANSI codes) to width w,
// without a trailing ellipsis.
func padRight(s string, w int) string {
	if sw := ansi.StringWidth(s); sw > w {
		return ansi.Truncate(s, w, "")
	} else if sw := ansi.StringWidth(s); sw < w {
		return s + strings.Repeat(" ", w-sw)
	}
	return s
}

// padLine truncates or pads s (which may contain ANSI codes) to width w.
func padLine(s string, w int) string {
	return padRight(s, w)
}

func clampInt(v, lo, hi int) int {
	if hi < lo {
		return lo
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
