package palette

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/mathieucroset/notty/internal/ui/icons"
)

// TestThemePickerMarksTheCurrentThemeWithTheIconSet checks the current
// theme's mark comes from the icon set.
func TestThemePickerMarksTheCurrentThemeWithTheIconSet(t *testing.T) {
	for _, name := range icons.Names() {
		t.Run(name, func(t *testing.T) {
			set, _ := icons.Get(name)
			m := New(DefaultCommands(), testStyles(t).WithIcons(set)).WithCurrentTheme("nord").SetSize(100, 40)
			m = findCommand(t, m, idSwitchTheme)
			m, _ = m.Update(key("enter"))
			if v := ansi.Strip(m.View()); !strings.Contains(v, set.Check+" nord") {
				t.Errorf("picker lacks %q:\n%s", set.Check+" nord", v)
			}
		})
	}
}
