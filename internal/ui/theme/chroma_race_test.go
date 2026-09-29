package theme

import (
	"fmt"
	"sync"
	"testing"

	"charm.land/glamour/v2"

	"github.com/mathieucroset/notty/internal/ui/icons"
)

// TestRegistryReadersAndWriterRace renders code blocks (which look chroma
// styles up by name) under RLockChroma while user palettes are registered
// concurrently. Run with -race.
func TestRegistryReadersAndWriterRace(t *testing.T) {
	p, _ := Get("nord")
	var wg sync.WaitGroup
	for i := range 4 {
		wg.Go(func() {
			for range 20 {
				unlock := RLockChroma()
				r, err := glamour.NewTermRenderer(glamour.WithStyles(GlamourStyle(p, icons.Default())), glamour.WithWordWrap(40))
				if err == nil {
					_, _ = r.Render("```go\nfunc main() {}\n```\n")
				}
				unlock()
			}
		})
		wg.Go(func() {
			q := p
			q.Name, q.ID = "race", fmt.Sprintf("race@%d", i)
			registerChromaStyle(ChromaStyleName(q), q)
		})
	}
	wg.Wait()
}
