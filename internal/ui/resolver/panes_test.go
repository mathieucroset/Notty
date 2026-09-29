package resolver

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// TestViewDrawsRoundedPanes checks the resolver uses the main screen's
// pane look (spec §4): the file list and the selected file in rounded
// panes, the file pane focused and titled with its path, and a key hint
// below them that is shortened with an ellipsis rather than cut off.
func TestViewDrawsRoundedPanes(t *testing.T) {
	tests := []struct {
		w, h     int
		wantList bool
	}{
		{120, 40, true},
		{80, 24, true},
		{39, 12, false},
	}
	for _, tt := range tests {
		m := newModel(t, tt.w, tt.h, textFile("notes/n.md", base1, ours1, theirs1))
		rows := strings.Split(plain(m), "\n")
		top, bottom, footer := rows[0], rows[len(rows)-2], rows[len(rows)-1]
		if got := strings.Contains(top, "╭─ Conflicts ─"); got != tt.wantList {
			t.Errorf("%dx%d: list pane shown = %v, want %v: %q", tt.w, tt.h, got, tt.wantList, top)
		}
		if !strings.Contains(top, "╭─ notes/n.md ─") {
			t.Errorf("%dx%d: file pane title missing: %q", tt.w, tt.h, top)
		}
		if !strings.HasPrefix(bottom, "╰") || !strings.HasSuffix(bottom, "╯") {
			t.Errorf("%dx%d: bottom border %q", tt.w, tt.h, bottom)
		}
		if !strings.HasPrefix(footer, " ]c/[c") {
			t.Errorf("%dx%d: footer %q is not padded by one column", tt.w, tt.h, footer)
		}
		if len(footerTextTab) > tt.w-2 && !strings.HasSuffix(strings.TrimRight(footer, " "), "…") {
			t.Errorf("%dx%d: long footer cut without an ellipsis: %q", tt.w, tt.h, footer)
		}
		if ansi.StringWidth(footer) != tt.w {
			t.Errorf("%dx%d: footer is %d wide", tt.w, tt.h, ansi.StringWidth(footer))
		}
	}
}
