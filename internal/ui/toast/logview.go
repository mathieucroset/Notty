package toast

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/mathieucroset/notty/internal/ui/msgs"
	"github.com/mathieucroset/notty/internal/ui/theme"
)

// CloseLogMsg asks the app to close the error log overlay.
type CloseLogMsg struct{}

// LogView is the scrollable error-log overlay content (spec §9: "! in the
// sidebar, or from the palette"). It scrolls by entry, not by wrapped line,
// so it needs no knowledge of the width it will eventually be rendered at.
type LogView struct {
	entries []Entry
	styles  theme.Styles
	offset  int // index of the first entry shown
}

// NewLogView returns a LogView over entries (as returned by Model.Log:
// newest first).
func NewLogView(entries []Entry, styles theme.Styles) LogView {
	return LogView{entries: append([]Entry(nil), entries...), styles: styles}
}

func emit(msg tea.Msg) tea.Cmd {
	return func() tea.Msg { return msg }
}

// Update handles j/k scrolling and esc/q to close.
func (v LogView) Update(msg tea.Msg) (LogView, tea.Cmd) {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return v, nil
	}
	switch k.String() {
	case "j", "down":
		if v.offset < len(v.entries)-1 {
			v.offset++
		}
	case "k", "up":
		if v.offset > 0 {
			v.offset--
		}
	case "esc", "q":
		return v, emit(CloseLogMsg{})
	}
	return v, nil
}

func levelGlyph(level msgs.ToastLevel, styles theme.Styles) (string, lipgloss.Style) {
	switch level {
	case msgs.ToastWarn:
		return "⚠", styles.Warning
	case msgs.ToastError:
		return "✗", styles.Error
	default:
		return "ℹ", styles.Muted
	}
}

// View renders the log at exactly width by height.
func (v LogView) View(width, height int) string {
	var lines []string
	for _, e := range v.entries[v.offset:] {
		glyph, style := levelGlyph(e.Level, v.styles)
		header := style.Render(e.Time.Format("15:04:05") + " " + glyph)
		for i, l := range wrapLines(e.Text, max(width-2, 1)) {
			if i == 0 {
				lines = append(lines, header+" "+l)
			} else {
				lines = append(lines, "   "+l)
			}
		}
		if len(lines) >= height {
			break
		}
	}
	if len(v.entries) == 0 {
		lines = append(lines, v.styles.Muted.Render("No errors or warnings logged."))
	}
	out := make([]string, height)
	for i := range height {
		if i < len(lines) {
			out[i] = padLine(lines[i], width)
		} else {
			out[i] = strings.Repeat(" ", width)
		}
	}
	return strings.Join(out, "\n")
}
