package theme

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/mathieucroset/notty/internal/ui/textutil"
)

// Pane draws a rounded pane of size w×h with title embedded in the top
// border (╭─ title ────── right ─╮) and body inside, fitted to the inner
// (w-2)×(h-2) area. The focused pane uses the accent border (spec §4.1).
// Every pane on every screen is drawn with it, so they all look alike.
func (s Styles) Pane(title, right, body string, w, h int, focused bool) string {
	fit := func(body string, w, h int) []string {
		return textutil.FitBlock(strings.Split(body, "\n"), max(w, 0), max(h, 0))
	}
	if w < 2 || h < 2 {
		return strings.Join(fit("", w, h), "\n")
	}
	borderStyle, titleStyle := s.PaneBorder, s.PaneTitle
	if focused {
		borderStyle, titleStyle = s.PaneBorderFocused, s.PaneTitleFocused
	}
	bc := lipgloss.NewStyle().Foreground(borderStyle.GetBorderTopForeground())
	innerW := w - 2

	// Top border: ╭─ title ─…─ right ─╮. Fixed cost: the two corners and
	// the dashes after ╭ and before ╮.
	rightSeg := ""
	if right != "" {
		rightSeg = " " + right + " "
	}
	titleRoom := innerW - 3 - ansi.StringWidth(rightSeg) // keep one ─ after the title
	if titleRoom < 5 {
		rightSeg = ""
		titleRoom = innerW - 3
	}
	titleSeg := ""
	if title != "" && titleRoom >= 3 {
		t := ansi.Truncate(title, titleRoom-2, "…")
		titleSeg = " " + t + " "
	}
	fill := innerW - 2 - ansi.StringWidth(titleSeg) - ansi.StringWidth(rightSeg)
	var top strings.Builder
	top.WriteString(bc.Render("╭─"))
	if titleSeg != "" {
		top.WriteString(titleStyle.Render(titleSeg))
	}
	top.WriteString(bc.Render(strings.Repeat("─", max(fill, 0))))
	if rightSeg != "" {
		top.WriteString(s.Accent.Render(rightSeg))
	}
	top.WriteString(bc.Render("─╮"))

	lines := make([]string, 0, h)
	lines = append(lines, top.String())
	side := bc.Render("│")
	for _, l := range fit(body, innerW, h-2) {
		lines = append(lines, side+l+side)
	}
	lines = append(lines, bc.Render("╰"+strings.Repeat("─", innerW)+"╯"))
	return strings.Join(lines, "\n")
}
