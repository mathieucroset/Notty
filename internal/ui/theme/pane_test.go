package theme

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestPane(t *testing.T) {
	p, _ := Get("catppuccin-mocha")
	st := NewStyles(p)
	tests := []struct {
		name, title, right string
		w, h               int
		wantTop            string
	}{
		{"title", "◆ Notty", "", 20, 4, "╭─ ◆ Notty ────────╮"},
		{"title and marker", "Work / Standup", "●", 24, 3, "╭─ Work / Standup ─ ● ─╮"},
		{"title truncated", "A very long pane title indeed", "●", 20, 3, "╭─ A very lo… ─ ● ─╮"},
		{"no title", "", "", 8, 3, "╭──────╮"},
		{"too small", "x", "", 1, 1, " "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lines := strings.Split(ansi.Strip(st.Pane(tt.title, tt.right, "hello\nworld", tt.w, tt.h, false)), "\n")
			if len(lines) != tt.h {
				t.Fatalf("got %d lines, want %d", len(lines), tt.h)
			}
			for i, l := range lines {
				if w := ansi.StringWidth(l); w != tt.w {
					t.Errorf("line %d width %d, want %d: %q", i, w, tt.w, l)
				}
			}
			if lines[0] != tt.wantTop {
				t.Errorf("top = %q, want %q", lines[0], tt.wantTop)
			}
			if tt.h >= 3 && !strings.HasPrefix(lines[1], "│hello") {
				t.Errorf("body = %q", lines[1])
			}
		})
	}
	if st.Pane("x", "", "", 10, 3, true) == st.Pane("x", "", "", 10, 3, false) {
		t.Error("focused and unfocused panes render identically")
	}
}
