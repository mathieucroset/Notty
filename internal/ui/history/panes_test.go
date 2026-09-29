package history

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// TestViewDrawsRoundedPanes checks the History view uses the main screen's
// pane look (spec §4): the commit list and the selected version in rounded
// panes with their titles in the top border, text padded one column from
// the borders, and the key hint below.
func TestViewDrawsRoundedPanes(t *testing.T) {
	m := newTest(t)
	e, _ := m.selected()
	m = m.SetVersion(e.Rev, "Hello world")
	lines := strings.Split(ansi.Strip(m.View()), "\n")
	top, bottom, footer := lines[0], lines[len(lines)-2], lines[len(lines)-1]
	for _, want := range []string{"╭─ History · Work/a.md ─", "╭─ 2026-09-29 14:03 · laptop ─", " rendered ─╮"} {
		if !strings.Contains(top, want) {
			t.Errorf("top border %q lacks %q", top, want)
		}
	}
	if !strings.HasPrefix(bottom, "╰") || !strings.HasSuffix(bottom, "╯") {
		t.Errorf("bottom border = %q", bottom)
	}
	if !strings.HasPrefix(footer, " tab rendered/diff") {
		t.Errorf("footer %q is not padded by one column", footer)
	}
	if !strings.HasPrefix(lines[1], "│ 2026-09-29 14:03") {
		t.Errorf("list row %q is not padded by one column", lines[1])
	}
	found := false
	for _, l := range lines {
		if strings.Contains(l, "│ Hello world") {
			found = true
		}
	}
	if !found {
		t.Errorf("version text is not padded by one column:\n%s", strings.Join(lines, "\n"))
	}

	m, _ = m.Update(key("tab"))
	if top := strings.Split(ansi.Strip(m.View()), "\n")[0]; !strings.Contains(top, " diff ─╮") {
		t.Errorf("diff mode title %q", top)
	}
}
