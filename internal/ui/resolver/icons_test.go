package resolver

import (
	"strings"
	"testing"

	"github.com/mathieucroset/notty/internal/imgrender"
	"github.com/mathieucroset/notty/internal/ui/icons"
)

// TestFileListDrawsTheConfiguredIcons checks the file list's marks, kind
// icons and error glyph come from the icon set.
func TestFileListDrawsTheConfiguredIcons(t *testing.T) {
	for _, name := range icons.Names() {
		t.Run(name, func(t *testing.T) {
			set, _ := icons.Get(name)
			s, p, opts := testOpts(t, false)
			s = s.WithIcons(set)
			opts.Styles = s
			caps := imgrender.Caps{Inline: imgrender.ProtoHalfBlocks, CellW: 8, CellH: 16}
			m := New(allKinds(), s, p, caps, opts).SetSize(160, 40)
			m = m.SetError("n.md", "disk full")
			v := plain(m)
			for _, want := range []string{
				set.Dirty + " " + set.KindText + " notes/meeting.md",
				set.Dirty + " " + set.KindBinary + " att/x.bin",
				set.Dirty + " " + set.KindModifyDelete + " n.md",
				set.Dirty + " " + set.KindPath + " Work/n.md",
				set.Check + " " + set.KindText + " done.md",
				set.Error + " disk full",
			} {
				if !strings.Contains(v, want) {
					t.Errorf("view lacks %q:\n%s", want, v)
				}
			}
			m, _ = press(t, m, "j", "j", "j", "j", "j")
			if v := plain(m); !strings.Contains(v, set.Check+" This file is resolved.") {
				t.Errorf("resolved pane lacks the check:\n%s", v)
			}
		})
	}
}
