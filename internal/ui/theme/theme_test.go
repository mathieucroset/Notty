package theme

import (
	"image/color"
	"strings"
	"testing"

	"charm.land/glamour/v2"
	chromastyles "github.com/alecthomas/chroma/v2/styles"
)

// wantModes is the fixed set of status-bar modes every palette's Styles must
// provide a pill style for.
var wantModes = []string{"NORMAL", "INSERT", "VISUAL", "COMMAND", "PLAIN", "READ-ONLY"}

func TestNamesStable(t *testing.T) {
	got := Names()

	// Set equality against the registered palettes themselves (the single
	// source of truth in palettes.go), so this test never duplicates the
	// literal name list and stays correct as palettes are added or removed.
	if len(got) != len(palettes) {
		t.Fatalf("Names() returned %d names, want %d (len(palettes)): %v", len(got), len(palettes), got)
	}
	seen := make(map[string]bool, len(got))
	for _, name := range got {
		if _, ok := palettes[name]; !ok {
			t.Errorf("Names() contains %q, which is not a registered palette", name)
		}
		if seen[name] {
			t.Errorf("Names() contains duplicate name %q", name)
		}
		seen[name] = true
	}
	for name := range palettes {
		if !seen[name] {
			t.Errorf("Names() is missing registered palette %q", name)
		}
	}

	// Stable ordering: repeated calls must return names in the same order.
	again := Names()
	for i := range got {
		if got[i] != again[i] {
			t.Fatalf("Names() order is not stable across calls: %v then %v", got, again)
		}
	}
}

func TestGetEveryName(t *testing.T) {
	for _, name := range Names() {
		p, ok := Get(name)
		if !ok {
			t.Fatalf("Get(%q) returned ok=false, want true", name)
		}
		if p.Name != name {
			t.Errorf("Get(%q).Name = %q, want %q", name, p.Name, name)
		}
	}
}

func TestGetUnknown(t *testing.T) {
	if _, ok := Get("nope"); ok {
		t.Fatalf("Get(\"nope\") returned ok=true, want false")
	}
}

func TestPaletteTokensNonNil(t *testing.T) {
	for _, name := range Names() {
		p, ok := Get(name)
		if !ok {
			t.Fatalf("Get(%q) failed", name)
		}
		tokens := map[string]color.Color{
			"Base":    p.Base,
			"Surface": p.Surface,
			"Overlay": p.Overlay,
			"Text":    p.Text,
			"Subtext": p.Subtext,
			"Muted":   p.Muted,
			"Accent":  p.Accent,
			"Accent2": p.Accent2,
			"Success": p.Success,
			"Warning": p.Warning,
			"Error":   p.Error,
		}
		for field, c := range tokens {
			if c == nil {
				t.Errorf("palette %q: token %s is nil", name, field)
			}
		}
		for i, h := range p.Headings {
			if h == nil {
				t.Errorf("palette %q: Headings[%d] is nil", name, i)
			}
		}
	}
}

func TestNewStylesDoesNotPanic(t *testing.T) {
	for _, name := range Names() {
		p, _ := Get(name)
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("NewStyles(%q) panicked: %v", name, r)
				}
			}()
			styles := NewStyles(p)
			for _, mode := range wantModes {
				if _, ok := styles.StatusMode[mode]; !ok {
					t.Errorf("palette %q: StatusMode missing mode %q", name, mode)
				}
			}
			if len(styles.StatusMode) != len(wantModes) {
				t.Errorf("palette %q: StatusMode has %d modes, want %d", name, len(styles.StatusMode), len(wantModes))
			}
		}()
	}
}

// TestChromaStyleDoesNotLeakAcrossPalettes is a regression test for a bug
// where glamour registers CodeBlock.Chroma under the single fixed chroma
// style name "charm" and only registers it once per process, so code-block
// syntax colors froze to whichever palette rendered first. GlamourStyle must
// instead register a uniquely named chroma style per palette (via
// ChromaStyleName) so rendering a code block under two different palettes,
// in the same process, produces different colored output.
func TestChromaStyleDoesNotLeakAcrossPalettes(t *testing.T) {
	const md = "```go\nfunc main() {}\n```\n"

	render := func(name string) string {
		p, ok := Get(name)
		if !ok {
			t.Fatalf("Get(%q) failed", name)
		}
		r, err := glamour.NewTermRenderer(
			glamour.WithStyles(GlamourStyle(p)),
			glamour.WithWordWrap(60),
		)
		if err != nil {
			t.Fatalf("palette %q: NewTermRenderer error: %v", name, err)
		}
		out, err := r.Render(md)
		if err != nil {
			t.Fatalf("palette %q: Render error: %v", name, err)
		}
		return out
	}

	// Render mocha first, then latte, in the same process: if chroma style
	// names collided, latte's render would still carry mocha's colors.
	mocha := render("catppuccin-mocha")
	latte := render("catppuccin-latte")

	if mocha == latte {
		t.Fatalf("rendered code block is identical between catppuccin-mocha and catppuccin-latte; chroma style is leaking across palettes")
	}
}

func TestChromaStylesRegisteredAtInit(t *testing.T) {
	// Package init must have registered every built-in palette's style,
	// so rendering never writes chroma's (unlocked) registry.
	for _, name := range Names() {
		if _, ok := chromastyles.Registry["notty-"+name]; !ok {
			t.Errorf("chroma style for %q not registered at init", name)
		}
	}
	RegisterChromaStyles() // idempotent
}

func TestChromaStyleNameStableAndUnique(t *testing.T) {
	for _, name := range Names() {
		p, _ := Get(name)
		got := ChromaStyleName(p)
		want := "notty-" + name
		if got != want {
			t.Errorf("ChromaStyleName(%q) = %q, want %q", name, got, want)
		}
		// Calling it again must return the same name and must not panic
		// (re-registration must be a safe no-op).
		if again := ChromaStyleName(p); again != got {
			t.Errorf("ChromaStyleName(%q) not stable across calls: %q then %q", name, got, again)
		}
	}
}

func TestGlamourStyleRenders(t *testing.T) {
	const md = "# Hi\n\n**b** and - [x] done"
	for _, name := range Names() {
		p, _ := Get(name)
		r, err := glamour.NewTermRenderer(
			glamour.WithStyles(GlamourStyle(p)),
			glamour.WithWordWrap(60),
		)
		if err != nil {
			t.Fatalf("palette %q: NewTermRenderer error: %v", name, err)
		}
		out, err := r.Render(md)
		if err != nil {
			t.Fatalf("palette %q: Render error: %v", name, err)
		}
		if strings.TrimSpace(out) == "" {
			t.Errorf("palette %q: Render produced empty output", name)
		}
	}
}
