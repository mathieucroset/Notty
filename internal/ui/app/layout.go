package app

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/mathieucroset/notty/internal/ui/theme"
)

// Layout constants (spec §4.1).
const (
	// SidebarWidth is the sidebar pane width, border included.
	SidebarWidth = 28
	// ZenWidth is the width of the centered content in zen layout.
	ZenWidth = 80
	// narrowWidth is the width below which the sidebar starts hidden.
	narrowWidth = 80
	// zenMinWidth is the width the terminal must exceed for zen layout.
	zenMinWidth = 100
	// minMainWidth is the narrowest main pane the sidebar may leave.
	minMainWidth = 20
)

// Rect is a screen rectangle.
type Rect struct{ X, Y, W, H int }

// Layout is the placement of every region on screen.
type Layout struct {
	// SidebarVisible reports whether the sidebar is drawn.
	SidebarVisible bool
	// Sidebar is the sidebar pane, border included (zero when hidden).
	Sidebar Rect
	// Main is the main pane, border included.
	Main Rect
	// Content is the main pane's content area: inside the border, and
	// centered at ZenWidth in zen layout.
	Content Rect
	// Status is the one-row status bar.
	Status Rect
}

// SidebarStartsVisible reports whether the sidebar is shown at startup for
// a terminal of the given width (spec §4.1: hidden below 80 columns).
func SidebarStartsVisible(width int) bool { return width >= narrowWidth }

// ComputeLayout places the sidebar, the main pane, and the status bar. The
// sidebar is dropped when it would leave the main pane narrower than
// minMainWidth. Zen layout applies when the sidebar is hidden and the
// terminal is wider than 100 columns.
func ComputeLayout(width, height int, sidebarVisible bool) Layout {
	width, height = max(width, 0), max(height, 0)
	var l Layout
	bodyH := max(height-1, 0)
	if height > 0 {
		l.Status = Rect{0, height - 1, width, 1}
	}

	l.SidebarVisible = sidebarVisible && width-SidebarWidth >= minMainWidth
	x := 0
	if l.SidebarVisible {
		l.Sidebar = Rect{0, 0, SidebarWidth, bodyH}
		x = SidebarWidth
	}
	l.Main = Rect{x, 0, width - x, bodyH}

	inner := Rect{l.Main.X + 1, 1, max(l.Main.W-2, 0), max(bodyH-2, 0)}
	if !l.SidebarVisible && width > zenMinWidth && inner.W > ZenWidth {
		inner.X += (inner.W - ZenWidth) / 2
		inner.W = ZenWidth
	}
	l.Content = inner
	return l
}

// padLine truncates or pads s (which may contain ANSI codes) to width w.
func padLine(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if sw := ansi.StringWidth(s); sw > w {
		return ansi.Truncate(s, w, "")
	} else if sw < w {
		return s + strings.Repeat(" ", w-sw)
	}
	return s
}

// fitBlock returns exactly h lines of exactly w columns from body.
func fitBlock(body string, w, h int) []string {
	src := strings.Split(body, "\n")
	out := make([]string, h)
	for i := range out {
		line := ""
		if i < len(src) {
			line = src[i]
		}
		out[i] = padLine(line, w)
	}
	return out
}

// renderPane draws a rounded pane of size w×h with title embedded in the
// top border (╭─ title ────── right ─╮) and body inside. The focused pane
// uses the accent border.
func renderPane(st theme.Styles, title, right, body string, w, h int, focused bool) string {
	if w < 2 || h < 2 {
		return strings.Join(fitBlock("", max(w, 0), max(h, 0)), "\n")
	}
	borderStyle, titleStyle := st.PaneBorder, st.PaneTitle
	if focused {
		borderStyle, titleStyle = st.PaneBorderFocused, st.PaneTitleFocused
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
		top.WriteString(st.Accent.Render(rightSeg))
	}
	top.WriteString(bc.Render("─╮"))

	lines := make([]string, 0, h)
	lines = append(lines, top.String())
	side := bc.Render("│")
	for _, l := range fitBlock(body, innerW, h-2) {
		lines = append(lines, side+l+side)
	}
	lines = append(lines, bc.Render("╰"+strings.Repeat("─", innerW)+"╯"))
	return strings.Join(lines, "\n")
}
