package palette

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/sahilm/fuzzy"

	"github.com/mathieucroset/notty/internal/ui/theme"
)

// boxWidth is the palette's default (unclamped) width, in columns.
const boxWidth = 60

// maxHeightPct caps the palette's height at this percentage of the
// terminal's height.
const maxHeightPct = 60

// CloseMsg asks the app to close the command palette overlay.
type CloseMsg struct{}

// ThemePreviewMsg is emitted as the highlight moves in the theme picker, so
// the app can re-render live with the previewed theme.
type ThemePreviewMsg struct{ Name string }

// ThemeChosenMsg is emitted when enter confirms a theme in the picker.
type ThemeChosenMsg struct{ Name string }

// ThemeCancelMsg is emitted when esc cancels the theme picker. The app
// should restore Original.
type ThemeCancelMsg struct{ Original string }

// mode is the palette's internal display mode.
type mode int

const (
	modeCommands mode = iota
	modeTheme
)

// match is one command that survived the current fuzzy filter, together
// with the indexes of the runes in its Name that matched.
type match struct {
	cmd     Command
	matched []int
}

// Model is the command palette overlay (spec §8).
type Model struct {
	commands []Command
	input    string
	matches  []match
	cursor   int

	mode mode

	// Theme picker state.
	themeNames    []string
	themeCursor   int
	originalTheme string
	currentTheme  string

	styles theme.Styles
	width  int
	height int
}

// New creates a command palette listing cmds.
func New(cmds []Command, styles theme.Styles) Model {
	m := Model{commands: cmds, styles: styles}
	m.refilter()
	return m
}

// WithCurrentTheme sets the theme marked ✓ in the theme picker, and the
// theme esc restores when the picker is cancelled.
func (m Model) WithCurrentTheme(name string) Model {
	m.currentTheme = name
	return m
}

// SetStyles re-themes the palette, so the theme picker previews the
// highlighted theme on itself too.
func (m Model) SetStyles(styles theme.Styles) Model {
	m.styles = styles
	return m
}

// SetSize sizes the overlay box for a termW x termH terminal: about 60
// columns wide, up to 60% of the terminal's height, but never wider or
// taller than the terminal itself.
func (m Model) SetSize(termW, termH int) Model {
	m.width = boxWidth
	if margin := termW - 4; margin < m.width {
		m.width = margin
	}
	m.width = clampInt(m.width, 1, max(termW, 1))

	m.height = termH * maxHeightPct / 100
	m.height = clampInt(m.height, 1, max(termH, 1))
	return m
}

// refilter recomputes m.matches from m.input. An empty query lists every
// command in DefaultCommands order; otherwise matches are ranked by fuzzy
// score against Name.
func (m *Model) refilter() {
	m.matches = nil
	if m.input == "" {
		for _, c := range m.commands {
			m.matches = append(m.matches, match{cmd: c})
		}
	} else {
		names := make([]string, len(m.commands))
		for i, c := range m.commands {
			names[i] = c.Name
		}
		for _, fm := range fuzzy.Find(m.input, names) {
			m.matches = append(m.matches, match{cmd: m.commands[fm.Index], matched: fm.MatchedIndexes})
		}
	}
	m.cursor = clampInt(m.cursor, 0, max(len(m.matches)-1, 0))
}

// Update handles a key press. Any other message is ignored.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	if m.mode == modeTheme {
		return m.updateTheme(k)
	}
	return m.updateCommands(k)
}

func (m Model) updateCommands(k tea.KeyPressMsg) (Model, tea.Cmd) {
	switch k.String() {
	case "esc":
		return m, closeCmd()
	case "enter":
		return m.runSelected()
	case "down", "ctrl+j":
		m.cursor = clampInt(m.cursor+1, 0, max(len(m.matches)-1, 0))
	case "up", "ctrl+k":
		m.cursor = clampInt(m.cursor-1, 0, max(len(m.matches)-1, 0))
	case "backspace":
		r := []rune(m.input)
		if len(r) == 0 {
			return m, nil
		}
		m.input = string(r[:len(r)-1])
		m.refilter()
	default:
		if text := k.Text; text != "" {
			m.input += text
			m.refilter()
		}
	}
	return m, nil
}

// runSelected runs the highlighted command: "Switch theme" opens the
// internal theme picker; everything else emits its message and closes.
func (m Model) runSelected() (Model, tea.Cmd) {
	if m.cursor < 0 || m.cursor >= len(m.matches) {
		return m, nil
	}
	cmd := m.matches[m.cursor].cmd
	if cmd.ID == idSwitchTheme {
		m.mode = modeTheme
		m.themeNames = theme.Names()
		m.originalTheme = m.currentTheme
		m.themeCursor = indexOf(m.themeNames, m.currentTheme)
		return m, nil
	}
	return m, tea.Batch(cmd.Msg, closeCmd())
}

func (m Model) updateTheme(k tea.KeyPressMsg) (Model, tea.Cmd) {
	switch k.String() {
	case "esc":
		m.mode = modeCommands
		orig := m.originalTheme
		return m, func() tea.Msg { return ThemeCancelMsg{Original: orig} }
	case "enter":
		name := m.currentTheme
		if m.themeCursor >= 0 && m.themeCursor < len(m.themeNames) {
			name = m.themeNames[m.themeCursor]
		}
		return m, tea.Batch(func() tea.Msg { return ThemeChosenMsg{Name: name} }, closeCmd())
	case "down", "ctrl+j":
		m.themeCursor = clampInt(m.themeCursor+1, 0, max(len(m.themeNames)-1, 0))
		return m, m.previewCmd()
	case "up", "ctrl+k":
		m.themeCursor = clampInt(m.themeCursor-1, 0, max(len(m.themeNames)-1, 0))
		return m, m.previewCmd()
	}
	return m, nil
}

func (m Model) previewCmd() tea.Cmd {
	if m.themeCursor < 0 || m.themeCursor >= len(m.themeNames) {
		return nil
	}
	name := m.themeNames[m.themeCursor]
	return func() tea.Msg { return ThemePreviewMsg{Name: name} }
}

func closeCmd() tea.Cmd {
	return func() tea.Msg { return CloseMsg{} }
}

// View renders the palette: the command list, or the theme picker.
func (m Model) View() string {
	if m.mode == modeTheme {
		return m.viewTheme()
	}
	return m.viewCommands()
}

func (m Model) viewCommands() string {
	inner := m.innerWidth()
	lines := []string{
		padLine(m.styles.Accent.Render("›")+" "+m.input+m.styles.Muted.Render("▏"), inner),
	}
	if len(m.matches) == 0 {
		lines = append(lines, padLine(m.styles.Muted.Render("no matching commands"), inner))
	} else {
		rows := m.listRows()
		start := 0
		if m.cursor >= rows {
			start = m.cursor - rows + 1
		}
		end := min(start+rows, len(m.matches))
		for i := start; i < end; i++ {
			lines = append(lines, m.renderRow(m.matches[i], i == m.cursor, inner))
		}
	}
	return m.styles.Dialog.Width(m.width).Render(strings.Join(lines, "\n"))
}

// renderRow renders one command row: the (possibly fuzzy-highlighted) name
// on the left, its key right-aligned in Muted. When the name and key
// together don't fit width, the name is truncated with an ellipsis so the
// key — the shorter, more load-bearing half of the row — is always shown in
// full.
func (m Model) renderRow(mt match, selected bool, width int) string {
	keyWidth := ansi.StringWidth(mt.cmd.Key)

	// Reserve room for the key and at least one separating space; whatever
	// remains is the name's budget.
	nameBudget := width - keyWidth - 1
	if nameBudget < 1 {
		nameBudget = 1
	}

	truncatedName, kept := truncateName(mt.cmd.Name, nameBudget)
	name := highlight(truncatedName, filterMatched(mt.matched, kept), m.styles.Match)

	gap := width - ansi.StringWidth(truncatedName) - keyWidth
	if gap < 1 {
		gap = 1
	}
	line := name + strings.Repeat(" ", gap) + m.styles.Muted.Render(mt.cmd.Key)
	line = padLine(line, width)
	if selected {
		return m.styles.Selection.Render(line)
	}
	return line
}

// truncateName returns s unchanged if it already fits maxWidth. Otherwise
// it returns a prefix of s followed by an ellipsis, sized to fit maxWidth,
// along with the byte length of that prefix (excluding the ellipsis) so
// callers can drop any fuzzy-match highlight that fell past the cut.
func truncateName(s string, maxWidth int) (truncated string, keptBytes int) {
	if maxWidth < 1 {
		maxWidth = 1
	}
	if ansi.StringWidth(s) <= maxWidth {
		return s, len(s)
	}
	const ellipsis = "…"
	budget := maxWidth - ansi.StringWidth(ellipsis)
	if budget < 0 {
		budget = 0
	}
	w, cut := 0, 0
	for i, r := range s {
		rw := ansi.StringWidth(string(r))
		if w+rw > budget {
			break
		}
		w += rw
		cut = i + len(string(r))
	}
	return s[:cut] + ellipsis, cut
}

// filterMatched drops any matched byte offset at or past limit, so
// highlighting a truncated name never reaches past what's actually shown.
func filterMatched(matched []int, limit int) []int {
	if len(matched) == 0 {
		return nil
	}
	out := make([]int, 0, len(matched))
	for _, i := range matched {
		if i < limit {
			out = append(out, i)
		}
	}
	return out
}

func (m Model) viewTheme() string {
	inner := m.innerWidth()
	lines := []string{padLine(m.styles.DialogTitle.Render("Switch theme"), inner)}
	rows := m.listRows()
	start := 0
	if m.themeCursor >= rows {
		start = m.themeCursor - rows + 1
	}
	end := min(start+rows, len(m.themeNames))
	for i := start; i < end; i++ {
		name := m.themeNames[i]
		mark := "  "
		if name == m.currentTheme {
			mark = "✓ "
		}
		line := padLine(mark+name, inner)
		if i == m.themeCursor {
			line = m.styles.Selection.Render(line)
		}
		lines = append(lines, line)
	}
	return m.styles.Dialog.Width(m.width).Render(strings.Join(lines, "\n"))
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

// listRows is the number of command/theme rows that fit below the
// prompt/title line, given m.height.
func (m Model) listRows() int {
	avail := m.height - 2 - 2 - 1 // border (2) + vertical padding (2) + prompt/title line
	if avail < 1 {
		avail = 1
	}
	return avail
}

// highlight renders s with the runes at the matched byte offsets styled
// with style, and every other rune left unstyled. matched holds byte
// offsets (as sahilm/fuzzy's Match.MatchedIndexes does, not rune indexes),
// so this ranges over the string directly instead of over []rune(s): for
// any name with a multi-byte rune before a match, a rune index and its byte
// offset diverge, and indexing []rune(s) by a byte offset would highlight
// the wrong character.
func highlight(s string, matched []int, style lipgloss.Style) string {
	if len(matched) == 0 {
		return s
	}
	set := make(map[int]bool, len(matched))
	for _, i := range matched {
		set[i] = true
	}
	var b strings.Builder
	for i, r := range s {
		if set[i] {
			b.WriteString(style.Render(string(r)))
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// padLine truncates or pads s (which may contain ANSI codes) to width w.
func padLine(s string, w int) string {
	if sw := ansi.StringWidth(s); sw > w {
		return ansi.Truncate(s, w, "")
	} else if sw < w {
		return s + strings.Repeat(" ", w-sw)
	}
	return s
}

func indexOf(list []string, s string) int {
	for i, v := range list {
		if v == s {
			return i
		}
	}
	return 0
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
