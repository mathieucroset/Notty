package editor

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/mathieucroset/notty/internal/buffer"
	"github.com/mathieucroset/notty/internal/ui/icons"
)

// TestRenderIconSets checks the display substitutions draw with the
// configured icon set: task checkboxes, the heading progress bar, and
// image chips.
func TestRenderIconSets(t *testing.T) {
	vault := t.TempDir()
	writePNG(t, filepath.Join(vault, "cat.png"), 64, 32)
	doc := "# Title\n\n- [ ] open\n- [x] done\n![](/cat.png)\n"
	for _, name := range icons.Names() {
		t.Run(name, func(t *testing.T) {
			set, _ := icons.Get(name)
			opts := testOptions(t)
			opts.VaultRoot = vault
			opts.Styles = opts.Styles.WithIcons(set)
			m := newModel(t, opts, doc, buffer.Pos{Line: 1}, 40, 8)
			rows := plainView(m)
			bar := strings.Repeat(set.ProgressFull, 3) + strings.Repeat(set.ProgressEmpty, 2)
			for row, want := range map[int]string{
				0: "# Title " + bar + " 1/2",
				2: set.TaskOpen + " open",
				3: set.TaskDone + " done",
				4: set.Image + " cat.png  64×32",
			} {
				if got := strings.TrimSpace(rows[row]); got != want {
					t.Errorf("row %d = %q, want %q", row, got, want)
				}
			}
			checkSize(t, m, 40, 8)

			ro := newModel(t, opts, "conflicted", buffer.Pos{}, 70, 5).SetReadOnly(true, "")
			if got, want := strings.TrimSpace(plainView(ro)[0]), set.Warn+" "+DefaultBanner; got != want {
				t.Errorf("banner = %q, want %q", got, want)
			}
		})
	}
}
