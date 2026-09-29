package tasksview

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/mathieucroset/notty/internal/index"
	"github.com/mathieucroset/notty/internal/ui/icons"
)

// TestDrawsTheConfiguredIcons checks the checkboxes and the empty-state
// mark come from the icon set.
func TestDrawsTheConfiguredIcons(t *testing.T) {
	refs := []index.TaskRef{
		ref("a.md", "A", 0, false, "open task"),
		ref("a.md", "A", 1, true, "finished task"),
	}
	for _, name := range icons.Names() {
		t.Run(name, func(t *testing.T) {
			set, _ := icons.Get(name)
			m := New(testStyles(t).WithIcons(set), 7, true).SetSize(40, 10).SetTasks(refs, testToday())
			view := ansi.Strip(m.View())
			for _, want := range []string{set.TaskOpen + " open task", set.TaskDone + " finished task"} {
				if !strings.Contains(view, want) {
					t.Errorf("view lacks %q:\n%s", want, view)
				}
			}
			empty := ansi.Strip(New(testStyles(t).WithIcons(set), 7, false).SetSize(40, 5).View())
			if !strings.Contains(empty, set.Check+" No open tasks") {
				t.Errorf("empty view lacks the %s check:\n%s", name, empty)
			}
		})
	}
}

// TestRowsUseThePaneGutter checks rows start at the view's left edge: the
// app's pane already pads its content by one column, like the editor's.
func TestRowsUseThePaneGutter(t *testing.T) {
	refs := []index.TaskRef{ref("Work/a.md", "A", 0, false, "open task")}
	m := newTest(t).SetTasks(refs, testToday())
	lines := strings.Split(ansi.Strip(m.View()), "\n")
	for _, want := range []string{"Work / A", icons.Default().TaskOpen + " open task"} {
		found := false
		for _, l := range lines {
			if strings.HasPrefix(l, want) {
				found = true
			}
		}
		if !found {
			t.Errorf("no row starts with %q:\n%s", want, strings.Join(lines, "\n"))
		}
	}
}
