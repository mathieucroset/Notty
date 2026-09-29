package sidebar

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/mathieucroset/notty/internal/ui/icons"
)

// TestDrawsTheConfiguredIcons checks every sidebar glyph comes from the
// configured set, and that the entries' labels line up even when the set's
// entry glyphs differ in width (ASCII "[ ]" next to "!").
func TestDrawsTheConfiguredIcons(t *testing.T) {
	for _, name := range icons.Names() {
		t.Run(name, func(t *testing.T) {
			set, _ := icons.Get(name)
			m := New(testStyles(t).WithIcons(set))
			m.SetSize(26, 30)
			m.SetTree(testTree())
			m.SetExpanded([]string{"Work"})
			m.SetPins([]string{"readme.md"})
			m.SetCounts(2, 1, 3)
			m.SetDirty("Work/ideas.md")
			view := ansi.Strip(m.View())

			for _, want := range []string{
				"  " + set.Pin + " readme",
				"  " + set.FolderClosed + " Personal",
				"  " + set.FolderOpen + " Work",
				"    " + set.Note + " ideas " + set.Dirty,
				"  " + set.File + " diagram.png",
			} {
				if !strings.Contains(view, want) {
					t.Errorf("sidebar lacks %q:\n%s", want, view)
				}
			}
			cols := map[string]int{}
			for _, l := range strings.Split(view, "\n") {
				for _, label := range []string{"Tasks (2)", "Conflicts (1)", "Trash (3)"} {
					if i := strings.Index(l, label); i >= 0 {
						cols[label] = ansi.StringWidth(l[:i])
						glyph := strings.TrimSpace(l[:i])
						if !strings.Contains(set.Tasks+set.Conflicts+set.Trash, glyph) || glyph == "" {
							t.Errorf("%q drawn with %q, not a %s glyph", label, glyph, name)
						}
					}
				}
			}
			if len(cols) != 3 {
				t.Fatalf("entries missing: %v\n%s", cols, view)
			}
			if cols["Tasks (2)"] != cols["Conflicts (1)"] || cols["Tasks (2)"] != cols["Trash (3)"] {
				t.Errorf("entry labels not aligned: %v\n%s", cols, view)
			}
		})
	}
}
