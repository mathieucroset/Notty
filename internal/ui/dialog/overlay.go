package dialog

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// Overlay composes box centered over base (dim-rendered first via dim) and
// returns a block exactly width columns by height rows, matching the size
// base is meant to fill (spec §4.2: overlays centered over a dimmed
// background).
func Overlay(base string, box string, width, height int, dim func(string) string) string {
	dimmed := dim(base)

	bw, bh := lipgloss.Width(box), lipgloss.Height(box)
	x := max(0, (width-bw)/2)
	y := max(0, (height-bh)/2)

	compositor := lipgloss.NewCompositor(
		lipgloss.NewLayer(dimmed).X(0).Y(0).Z(0),
		lipgloss.NewLayer(box).X(x).Y(y).Z(1),
	)

	canvas := lipgloss.NewCanvas(width, height)
	canvas.Compose(compositor)

	return padBlock(canvas.Render(), width, height)
}

// DimANSI strips s's existing ANSI styling and re-renders every line in
// muted. Used to dim the background behind an overlay.
func DimANSI(s string, muted color.Color) string {
	style := lipgloss.NewStyle().Foreground(muted)
	lines := strings.Split(ansi.Strip(s), "\n")
	for i, l := range lines {
		lines[i] = style.Render(l)
	}
	return strings.Join(lines, "\n")
}

// padBlock pads or truncates s to exactly width columns by height rows.
func padBlock(s string, width, height int) string {
	lines := strings.Split(s, "\n")
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
