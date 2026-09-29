package trash

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// TestPreviewColumnIsPadded checks the preview keeps one column of padding
// after the separator and before the pane's edge, like the other panes.
func TestPreviewColumnIsPadded(t *testing.T) {
	m := newTest(t)
	it, _ := m.Selected()
	m = m.SetPreview(it.ID, strings.Repeat("word ", 40))
	rows := 0
	for i, l := range strings.Split(ansi.Strip(m.View()), "\n") {
		_, preview, ok := strings.Cut(l, "│")
		if !ok || !strings.Contains(preview, "word") {
			continue
		}
		rows++
		if !strings.HasPrefix(preview, " word") {
			t.Errorf("row %d preview %q does not start one column after the separator", i, preview)
		}
		if !strings.HasSuffix(preview, " ") {
			t.Errorf("row %d preview %q touches the pane's edge", i, preview)
		}
	}
	if rows < 2 {
		t.Fatalf("expected the paragraph to wrap over several rows, got %d:\n%s", rows, ansi.Strip(m.View()))
	}
}
