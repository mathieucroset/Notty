package toast

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/mathieucroset/notty/internal/ui/msgs"
	"github.com/mathieucroset/notty/internal/ui/textutil"
	"github.com/mathieucroset/notty/internal/ui/theme"
)

// CloseLogMsg asks the app to close the error log overlay.
type CloseLogMsg struct{}

// LogView is the scrollable error-log overlay content (spec §9: "! in the
// sidebar, or from the palette"). It scrolls by rendered (wrapped) line
// rather than by entry, so a long entry can be read in full a line at a
// time instead of jumping past it. That means Update needs to know the
// viewport size to scroll and clamp correctly: call SetSize once the
// overlay's size is known (and again on resize), the same way the app
// calls sidebar.SetSize, before routing key presses to Update.
type LogView struct {
	entries []Entry
	styles  theme.Styles

	offset int // index of the first visible rendered line

	width, height int
	lines         []string // entries rendered and wrapped at width
}

// NewLogView returns a LogView over entries (as returned by Model.Log:
// newest first).
func NewLogView(entries []Entry, styles theme.Styles) LogView {
	return LogView{entries: append([]Entry(nil), entries...), styles: styles}
}

func emit(msg tea.Msg) tea.Cmd {
	return func() tea.Msg { return msg }
}

// SetSize sets the viewport the log renders into, re-wrapping entries for
// width when it changes and clamping the scroll offset to height.
func (v *LogView) SetSize(width, height int) {
	if width != v.width || v.lines == nil {
		v.width = width
		v.lines = v.renderLines(width)
	}
	v.height = height
	if max := len(v.lines) - height; v.offset > max {
		v.offset = max
	}
	if v.offset < 0 {
		v.offset = 0
	}
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

// renderLines flattens every entry into its wrapped display lines at width,
// aligning continuation lines under the first line's text (not under the
// timestamp/icon header).
func (v LogView) renderLines(width int) []string {
	var lines []string
	for _, e := range v.entries {
		glyph, style := levelGlyph(e.Level, v.styles)
		header := style.Render(e.Time.Format("15:04:05")+" "+glyph) + " "
		headerWidth := lipgloss.Width(header)
		indent := strings.Repeat(" ", headerWidth)

		for i, l := range textutil.Wrap(e.Text, max(width-headerWidth, 1)) {
			if i == 0 {
				lines = append(lines, header+l)
			} else {
				lines = append(lines, indent+l)
			}
		}
	}
	if len(lines) == 0 {
		lines = []string{v.styles.Muted.Render("No errors or warnings logged.")}
	}
	return lines
}

// Update handles j/k scrolling (by rendered line, per SetSize) and esc/q to
// close.
func (v LogView) Update(msg tea.Msg) (LogView, tea.Cmd) {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return v, nil
	}
	switch k.String() {
	case "j", "down":
		if maxOffset := len(v.lines) - v.height; v.offset < maxOffset {
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

// View renders the log at exactly width by height. It calls SetSize itself
// (on its own local copy) so it always renders correctly for the given
// size even if the caller hasn't called SetSize explicitly; for scrolling
// to track the same viewport, the caller should still call SetSize on the
// persisted model before Update, as noted on the type.
func (v LogView) View(width, height int) string {
	v.SetSize(width, height)
	end := min(v.offset+height, len(v.lines))
	return strings.Join(textutil.FitBlock(v.lines[v.offset:end], width, height), "\n")
}
