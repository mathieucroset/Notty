package app

import (
	"strings"
	"testing"

	"github.com/mathieucroset/notty/internal/ui/icons"
	"github.com/mathieucroset/notty/internal/ui/msgs"
)

// TestIconsSettingReachesTheScreen checks that the icons config key picks
// the glyph set the whole UI draws with, and that a theme change keeps it.
func TestIconsSettingReachesTheScreen(t *testing.T) {
	for _, name := range icons.Names() {
		t.Run(name, func(t *testing.T) {
			set, _ := icons.Get(name)
			opts := testOptions(t)
			opts.Config.Icons = name
			m := start(t, opts, 120, 30)
			if got := m.opts.Styles.Icons.Name; got != name {
				t.Fatalf("styles icons = %q, want %q", got, name)
			}
			if s := screen(m); !strings.Contains(s, set.Logo+" Notty") {
				t.Errorf("sidebar title lacks the %s logo:\n%s", name, s)
			}
			run(t, m, msgs.OpenNoteMsg{Path: "ideas.md", Line: 0})
			typeText(t, m, "ix")
			if top := strings.Split(screen(m), "\n")[0]; !strings.Contains(top, " "+set.Dirty+" ─╮") {
				t.Errorf("pane title lacks the %s dirty mark: %q", name, top)
			}
			m.applyTheme("nord")
			if got := m.opts.Styles.Icons.Name; got != name {
				t.Errorf("after a theme change icons = %q, want %q", got, name)
			}
		})
	}
}
