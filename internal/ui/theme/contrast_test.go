package theme

import (
	"image/color"
	"math"
	"testing"
)

func TestContrast(t *testing.T) {
	black := color.RGBA{0, 0, 0, 255}
	white := color.RGBA{255, 255, 255, 255}
	grey := hex("#777777")
	tests := []struct {
		name string
		a, b color.Color
		want float64
	}{
		{"black on white", black, white, 21},
		{"order does not matter", white, black, 21},
		{"same colour", grey, grey, 1},
		{"mid grey on white", grey, white, 4.48},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Contrast(tt.a, tt.b); math.Abs(got-tt.want) > 0.01 {
				t.Errorf("Contrast = %.3f, want %.2f", got, tt.want)
			}
		})
	}
}

func TestLegible(t *testing.T) {
	white := color.RGBA{255, 255, 255, 255}
	dark := hex("#1e1e2e")
	tests := []struct {
		name     string
		fg       color.Color
		min      float64
		unchange bool
	}{
		{"already legible is unchanged", hex("#cdd6f4"), 4.5, true},
		{"too dim is lightened", hex("#3b4252"), 3, false},
		{"far too dim still reaches the minimum", hex("#1f1f2f"), 7, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := legible(tt.fg, white, tt.min, dark)
			if tt.unchange && got != tt.fg {
				t.Errorf("legible changed %v to %v", tt.fg, got)
			}
			if c := Contrast(got, dark); c < tt.min {
				t.Errorf("contrast after legible = %.2f, want >= %.1f", c, tt.min)
			}
		})
	}
}

// TestEveryThemeIsLegible checks every registered theme's tokens against
// the backgrounds they are drawn on (spec §4.5), so no theme renders text
// that cannot be read. Muted is dim by design (unfocused borders, hints)
// but must stay readable.
func TestEveryThemeIsLegible(t *testing.T) {
	for _, name := range Names() {
		p, _ := Get(name)
		checks := []struct {
			token string
			fg    color.Color
			bgs   []color.Color
			min   float64
		}{
			{"Text", p.Text, []color.Color{p.Base}, minText},
			{"Text", p.Text, []color.Color{p.Surface, p.Overlay}, minTextRaised},
			{"Subtext", p.Subtext, []color.Color{p.Base, p.Surface}, minSubtext},
			{"Muted", p.Muted, []color.Color{p.Base, p.Surface}, minMuted},
			{"Accent", p.Accent, []color.Color{p.Base, p.Surface, p.Overlay}, minColor},
			{"Accent2", p.Accent2, []color.Color{p.Base, p.Surface}, minColor},
			{"Success", p.Success, []color.Color{p.Base, p.Surface}, minColor},
			{"Warning", p.Warning, []color.Color{p.Base, p.Surface}, minColor},
			{"Error", p.Error, []color.Color{p.Base, p.Surface}, minColor},
		}
		for i, h := range p.Headings {
			checks = append(checks, struct {
				token string
				fg    color.Color
				bgs   []color.Color
				min   float64
			}{"Headings[" + string(rune('1'+i)) + "]", h, []color.Color{p.Base}, minColor})
		}
		for _, c := range checks {
			for _, bg := range c.bgs {
				if got := Contrast(c.fg, bg); got < c.min {
					t.Errorf("%s: %s on %s has contrast %.2f, want >= %.1f", name, c.token, hexOf(bg), got, c.min)
				}
			}
		}
	}
}
