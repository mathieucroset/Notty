package dialog

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestOverlayDimensions(t *testing.T) {
	const width, height = 60, 20
	// Build a base of exactly width x height, filled with a repeating
	// character so it's easy to tell it apart from the box.
	lines := make([]string, height)
	for i := range lines {
		lines[i] = strings.Repeat(".", width)
	}
	base := strings.Join(lines, "\n")

	box := "single"
	out := Overlay(base, box, width, height, func(s string) string { return s })

	outLines := strings.Split(out, "\n")
	if len(outLines) != height {
		t.Fatalf("expected %d lines, got %d", height, len(outLines))
	}
	for i, l := range outLines {
		if w := ansi.StringWidth(l); w != width {
			t.Fatalf("line %d: expected width %d, got %d (%q)", i, width, w, l)
		}
	}
}

func TestOverlayCentersBox(t *testing.T) {
	const width, height = 40, 10
	base := strings.Repeat(" ", width)
	lines := make([]string, height)
	for i := range lines {
		lines[i] = base
	}
	full := strings.Join(lines, "\n")

	box := "HELLO"
	out := Overlay(full, box, width, height, func(s string) string { return s })
	outLines := strings.Split(out, "\n")

	// The box should land on the vertical middle row...
	midRow := (height - lipgloss.Height(box)) / 2
	if !strings.Contains(outLines[midRow], "HELLO") {
		t.Fatalf("expected HELLO on row %d, got %q", midRow, outLines[midRow])
	}
	// ...horizontally centered.
	col := strings.Index(outLines[midRow], "HELLO")
	wantCol := (width - len("HELLO")) / 2
	if col != wantCol {
		t.Fatalf("expected HELLO at column %d, got %d (line %q)", wantCol, col, outLines[midRow])
	}
}

func TestDimANSIStripsColors(t *testing.T) {
	styled := lipgloss.NewStyle().Foreground(lipgloss.Color("#ff0000")).Render("hello") + "\n" +
		lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#00ff00")).Render("world")

	if !strings.Contains(styled, "\x1b[") {
		t.Fatal("test setup: expected the input to actually contain ANSI codes")
	}

	dimmed := DimANSI(styled, lipgloss.Color("#888888"))

	if ansi.Strip(dimmed) != "hello\nworld" {
		t.Fatalf("expected text to survive unchanged, got %q", ansi.Strip(dimmed))
	}
	if !strings.Contains(dimmed, "\x1b[") {
		t.Fatal("expected the muted color to be applied")
	}
	// The original red/green codes should be gone.
	if strings.Contains(dimmed, "255;0;0") || strings.Contains(dimmed, "0;255;0") {
		t.Fatalf("expected original colors to be stripped, got %q", dimmed)
	}
}
