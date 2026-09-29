package preview

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/mathieucroset/notty/internal/imgrender"
	"github.com/mathieucroset/notty/internal/ui/icons"
	"github.com/mathieucroset/notty/internal/ui/theme"
)

func TestHasBox(t *testing.T) {
	ascii, _ := icons.Get("ascii")
	boxes := [2]string{ascii.TaskOpen, ascii.TaskDone}
	tests := []struct {
		row  string
		want bool
	}{
		{"[ ] open task", true},
		{"[x] done task", true},
		{"☐ open task", false},
		{"plain text", false},
	}
	for _, tt := range tests {
		if got := hasBox(tt.row, boxes); got != tt.want {
			t.Errorf("hasBox(%q) = %v, want %v", tt.row, got, tt.want)
		}
	}
	if hasBox("anything", [2]string{}) {
		t.Error("empty boxes matched")
	}
}

// TestPreviewIconSets checks the preview draws checkboxes and image chips
// with the configured icon set, and still finds each task's row by its
// checkbox for ]t.
func TestPreviewIconSets(t *testing.T) {
	const note = "# Plan\n\n- [ ] open task\n- [x] done task\n\n![](nope.png)\n"
	for _, name := range icons.Names() {
		t.Run(name, func(t *testing.T) {
			set, _ := icons.Get(name)
			p := testPalette(t)
			m := New(theme.NewStyles(p).WithIcons(set), p, imgrender.Caps{Inline: imgrender.ProtoOff, CellW: 8, CellH: 16}, t.TempDir())
			m.debounce = time.Millisecond
			m = m.SetSize(60, 20)
			m, _ = setContent(t, m, "n.md", note)
			view := ansi.Strip(m.View())
			for _, want := range []string{set.TaskOpen + " open task", set.TaskDone + " done task", set.Image + " nope.png"} {
				if !strings.Contains(view, want) {
					t.Errorf("preview lacks %q:\n%s", want, view)
				}
			}
			rows := strings.Split(view, "\n")
			for i, want := range []string{"open task", "done task"} {
				m, _ = press(m, "]", "t")
				if r := m.doc.taskRows[i]; r < 0 || !strings.Contains(rows[r], want) {
					t.Errorf("task %d mapped to row %d, want the %q row:\n%s", i, r, want, view)
				}
			}
		})
	}
}
