package app

import (
	"strings"

	"github.com/mathieucroset/notty/internal/ui/textutil"
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

// fitBlock returns exactly h lines of exactly w columns from body.
func fitBlock(body string, w, h int) []string {
	return textutil.FitBlock(strings.Split(body, "\n"), max(w, 0), max(h, 0))
}

// renderPane draws a rounded pane of size w×h with title embedded in the
// top border (╭─ title ────── right ─╮) and body inside. The focused pane
// uses the accent border.
func renderPane(st theme.Styles, title, right, body string, w, h int, focused bool) string {
	return st.Pane(title, right, body, w, h, focused)
}
