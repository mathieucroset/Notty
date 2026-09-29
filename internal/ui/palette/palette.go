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

// SetSize sizes the overlay box for a termW x termH terminal: about 60
// columns wide (clamped to the terminal), up to 60% of the terminal's
// height.
func (m Model) SetSize(termW, termH int) Model {
	m.width = boxWidth
	if maxW := termW - 4; maxW < m.width {
		m.width = max(maxW, 20)
	}
	m.height = max(termH*maxHeightPct/100, 6)
	if m.height > termH {
		m.height = termH
	}
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

// runSelected runs the highlighted command: it emits its message and
// closes the palette.
func (m Model) runSelected() (Model, tea.Cmd) {
	if m.cursor < 0 || m.cursor >= len(m.matches) {
		return m, nil
	}
	cmd := m.matches[m.cursor].cmd
	return m, tea.Batch(cmd.Msg, closeCmd())
}

func closeCmd() tea.Cmd {
	return func() tea.Msg { return CloseMsg{} }
}

// View renders the palette's command list.
func (m Model) View() string {
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

func (m Model) renderRow(mt match, selected bool, width int) string {
	name := highlight(mt.cmd.Name, mt.matched, m.styles.Match)
	nameWidth := ansi.StringWidth(mt.cmd.Name)
	keyWidth := ansi.StringWidth(mt.cmd.Key)
	gap := width - nameWidth - keyWidth
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

// innerWidth is the width available for content inside the Dialog style's
// border and padding.
func (m Model) innerWidth() int {
	w := m.width - 2 - 4 // border (2) + horizontal padding (2x2)
	if w < 10 {
		w = 10
	}
	return w
}

// listRows is the number of command rows that fit below the prompt line,
// given m.height.
func (m Model) listRows() int {
	avail := m.height - 2 - 2 - 1 // border (2) + vertical padding (2) + prompt line
	if avail < 1 {
		avail = 1
	}
	return avail
}

// highlight renders s with the runes at the matched indexes styled with
// style, and every other rune left unstyled.
func highlight(s string, matched []int, style lipgloss.Style) string {
	if len(matched) == 0 {
		return s
	}
	set := make(map[int]bool, len(matched))
	for _, i := range matched {
		set[i] = true
	}
	var b strings.Builder
	for i, r := range []rune(s) {
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
