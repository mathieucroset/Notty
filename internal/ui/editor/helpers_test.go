package editor

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/mathieucroset/notty/internal/buffer"
	"github.com/mathieucroset/notty/internal/ui/theme"
)

func testPalette(t *testing.T) theme.Palette {
	t.Helper()
	p, ok := theme.Get("catppuccin-mocha")
	if !ok {
		t.Fatal("theme catppuccin-mocha missing")
	}
	return p
}

func testOptions(t *testing.T) Options {
	p := testPalette(t)
	return Options{Vim: true, Styles: theme.NewStyles(p), Palette: p, AutosaveMS: 1}
}

// newModel returns a focused editor of size w×h holding content with the
// cursor at cur.
func newModel(t *testing.T, opts Options, content string, cur buffer.Pos, w, h int) Model {
	t.Helper()
	m := New(opts).SetSize(w, h).SetFocused(true)
	return m.Load("notes/test.md", content, cur)
}

// plainView returns the view without ANSI codes, with trailing spaces
// trimmed from each row.
func plainView(m Model) []string {
	rows := strings.Split(ansi.Strip(m.View()), "\n")
	for i, r := range rows {
		rows[i] = strings.TrimRight(r, " ")
	}
	return rows
}

// checkSize fails unless the view has exactly h rows of exactly w cells.
func checkSize(t *testing.T, m Model, w, h int) {
	t.Helper()
	rows := strings.Split(m.View(), "\n")
	if len(rows) != h {
		t.Fatalf("view has %d rows, want %d", len(rows), h)
	}
	for i, r := range rows {
		if got := ansi.StringWidth(r); got != w {
			t.Errorf("row %d is %d cells wide, want %d: %q", i, got, w, ansi.Strip(r))
		}
	}
}
