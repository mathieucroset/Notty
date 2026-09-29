package theme

import (
	"image/color"
	"math"
)

// Minimum contrast ratios (WCAG 2) between a token and the backgrounds it
// is drawn on. Every palette is nudged to meet them (see legibleTokens),
// so every theme renders legibly (Task 39).
const (
	// minText is body text on the main background (WCAG AA).
	minText = 4.5
	// minTextRaised is body text on raised panels and the selection.
	minTextRaised = 3.5
	// minSubtext is secondary text, such as section headers.
	minSubtext = 3.0
	// minMuted is dim text: hints, unfocused borders, markdown markup.
	minMuted = 2.5
	// minColor is coloured text and the text of coloured pills (the pill
	// text is the base colour, so the ratio is the same).
	minColor = 3.0
)

// Contrast returns the WCAG 2 contrast ratio of a and b, from 1 (no
// contrast) to 21 (black on white).
func Contrast(a, b color.Color) float64 {
	la, lb := luminance(a), luminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

// luminance is the WCAG relative luminance of c.
func luminance(c color.Color) float64 {
	r, g, b, _ := c.RGBA()
	lin := func(v uint32) float64 {
		x := float64(v) / 0xffff
		if x <= 0.03928 {
			return x / 12.92
		}
		return math.Pow((x+0.055)/1.055, 2.4)
	}
	return 0.2126*lin(r) + 0.7152*lin(g) + 0.0722*lin(b)
}

// legible returns fg unchanged when its contrast with every one of bgs is
// at least min. Otherwise it mixes fg toward ink, in 5% steps, until it is;
// ink is the colour of maximum contrast (the theme's text, or black or
// white), so the result keeps as much of fg's hue as the minimum allows.
func legible(fg, ink color.Color, min float64, bgs ...color.Color) color.Color {
	ok := func(c color.Color) bool {
		for _, bg := range bgs {
			if Contrast(c, bg) < min {
				return false
			}
		}
		return true
	}
	if ok(fg) {
		return fg
	}
	for step := 1; step < 20; step++ {
		if c := mix(fg, ink, float64(step)/20); ok(c) {
			return c
		}
	}
	return ink
}

// mix blends a toward b by t (0 is a, 1 is b).
func mix(a, b color.Color, t float64) color.Color {
	ar, ag, ab, _ := a.RGBA()
	br, bg, bb, _ := b.RGBA()
	ch := func(x, y uint32) uint8 {
		v := (float64(x)*(1-t) + float64(y)*t) / 0xffff * 255
		return uint8(math.Round(v))
	}
	return color.RGBA{R: ch(ar, br), G: ch(ag, bg), B: ch(ab, bb), A: 0xff}
}

// legibleTokens returns p with every text token nudged, if needed, to the
// minimum contrast against the backgrounds it is drawn on. The canonical
// colours of each theme are kept wherever they are already legible; only
// the few that are too faint (e.g. Nord's comment grey, the yellows of the
// light themes) are darkened or lightened toward the text colour.
func legibleTokens(p Palette) Palette {
	extreme := color.Color(color.RGBA{0, 0, 0, 0xff})
	if p.Dark {
		extreme = color.RGBA{0xff, 0xff, 0xff, 0xff}
	}
	p.Text = legible(p.Text, extreme, minText, p.Base)
	p.Text = legible(p.Text, extreme, minTextRaised, p.Surface, p.Overlay)
	p.Subtext = legible(p.Subtext, p.Text, minSubtext, p.Base, p.Surface)
	p.Muted = legible(p.Muted, p.Text, minMuted, p.Base, p.Surface)
	p.Accent = legible(p.Accent, p.Text, minColor, p.Base, p.Surface, p.Overlay)
	p.Accent2 = legible(p.Accent2, p.Text, minColor, p.Base, p.Surface)
	p.Success = legible(p.Success, p.Text, minColor, p.Base, p.Surface)
	p.Warning = legible(p.Warning, p.Text, minColor, p.Base, p.Surface)
	p.Error = legible(p.Error, p.Text, minColor, p.Base, p.Surface)
	for i, h := range p.Headings {
		p.Headings[i] = legible(h, p.Text, minColor, p.Base)
	}
	return p
}

// legiblePalettes applies legibleTokens to every palette in ps.
func legiblePalettes(ps map[string]Palette) map[string]Palette {
	for name, p := range ps {
		ps[name] = legibleTokens(p)
	}
	return ps
}
