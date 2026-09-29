package toast

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/mathieucroset/notty/internal/ui/icons"
	"github.com/mathieucroset/notty/internal/ui/msgs"
)

// TestToastsDrawTheConfiguredIcons checks the level glyphs of toasts and
// the error log come from the icon set, and that a wrapped toast's second
// line starts under the first line's text whatever the glyph's width.
func TestToastsDrawTheConfiguredIcons(t *testing.T) {
	levels := []msgs.ToastLevel{msgs.ToastInfo, msgs.ToastWarn, msgs.ToastError}
	for _, name := range icons.Names() {
		t.Run(name, func(t *testing.T) {
			set, _ := icons.Get(name)
			want := map[msgs.ToastLevel]string{msgs.ToastInfo: set.Info, msgs.ToastWarn: set.Warn, msgs.ToastError: set.Error}
			for _, level := range levels {
				m := New(testStyles(t).WithIcons(set))
				m, _ = m.Push(level, "a message long enough to wrap onto a second line of the toast")
				lines := strings.Split(strip(m.View(30)), "\n")
				if !strings.Contains(lines[1], want[level]+" a message") {
					t.Fatalf("level %v: toast lacks %q:\n%s", level, want[level], strings.Join(lines, "\n"))
				}
				col := func(l, s string) int { return ansi.StringWidth(l[:strings.Index(l, s)]) }
				if c1, c2 := col(lines[1], "a message"), col(lines[2], "wrap"); c1 != c2 {
					t.Errorf("level %v: continuation at column %d, text at %d:\n%s", level, c2, c1, strings.Join(lines, "\n"))
				}
			}
			entries := testEntries(3)
			log := strip(NewLogView(entries, testStyles(t).WithIcons(set)).View(60, 10))
			for _, level := range levels {
				if !strings.Contains(log, want[level]) {
					t.Errorf("error log lacks %q:\n%s", want[level], log)
				}
			}
		})
	}
}
