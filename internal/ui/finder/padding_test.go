package finder

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/mathieucroset/notty/internal/index"
	"github.com/mathieucroset/notty/internal/search"
)

// TestPreviewPaneIsPadded checks the preview pane keeps one column of
// padding on each side, like every other pane: its text never touches the
// separator or the border.
func TestPreviewPaneIsPadded(t *testing.T) {
	m, _ := fullTextModelWithNote(t, "irrelevant")
	m.hits = []search.Hit{{Path: "note.md", Line: 0, Text: "line0"}}
	full := make([]string, 40)
	for i := range full {
		full[i] = padLine(strings.Repeat("x", m.previewWidth()), m.previewWidth())
	}
	m = primeCache(m, previewKey{path: "note.md", width: m.previewWidth(), windowStart: 0, windowLen: 40}, full)
	for i, l := range strings.Split(ansi.Strip(m.View()), "\n") {
		parts := strings.Split(l, "│")
		if len(parts) != 4 || !strings.Contains(l, "x") {
			continue // not a body row with preview text
		}
		preview := parts[2]
		if !strings.HasPrefix(preview, " x") || !strings.HasSuffix(preview, "x ") {
			t.Errorf("row %d preview %q is not padded by one column", i, preview)
		}
		if w := ansi.StringWidth(l); w != m.width {
			t.Errorf("row %d is %d wide", i, w)
		}
	}
}

func TestResultCounts(t *testing.T) {
	notes := []*index.Note{note("alpha.md", "alpha"), note("beta.md", "alpha beta")}
	tests := []struct {
		name string
		n    int
		want string
	}{
		{"one", 1, "1 result"},
		{"two", 2, "2 results"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := New(FullText, notes, nil, testStyles(t), testPalette(t)).SetSize(100, 30)
			m.query = "alpha"
			m.hits = make([]search.Hit, tt.n)
			if got := m.fullTextStatus(); got != tt.want {
				t.Errorf("full-text status = %q, want %q", got, tt.want)
			}
			f := New(Fuzzy, notes[:tt.n], nil, testStyles(t), testPalette(t)).SetSize(100, 30)
			f.query = "a"
			f.recomputeFuzzy()
			if got := f.fuzzyStatus(); got != tt.want {
				t.Errorf("fuzzy status = %q, want %q", got, tt.want)
			}
		})
	}
}
