package help

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// TestViewHasATitleAndKeyHint checks the help overlay says what it is and
// how to scroll and close it, like the error log, and only offers
// scrolling when there is more to see.
func TestViewHasATitleAndKeyHint(t *testing.T) {
	tests := []struct {
		name       string
		w, h       int
		wantScroll bool
	}{
		{"cramped", 80, 24, true},
		{"roomy", 240, 160, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := New(testStyles(t)).SetSize(tt.w, tt.h)
			lines := splitLines(ansi.Strip(m.View()))
			header := lines[2] // top border, then the padding row
			if !strings.Contains(header, "Keyboard shortcuts") || !strings.Contains(header, "esc close") {
				t.Errorf("header = %q", header)
			}
			if got := strings.Contains(header, "j/k scroll"); got != tt.wantScroll {
				t.Errorf("scroll hint shown = %v, want %v: %q", got, tt.wantScroll, header)
			}
			if len(lines) != m.height {
				t.Errorf("view has %d lines, want %d", len(lines), m.height)
			}
			// Scrolling to the end still shows the last section's last row.
			m, _ = m.Update(key("G"))
			if v := ansi.Strip(m.View()); !strings.Contains(v, "Keyboard shortcuts") {
				t.Errorf("header scrolled away:\n%s", v)
			}
		})
	}
}
