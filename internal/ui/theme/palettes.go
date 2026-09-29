// Package theme defines Notty's color palettes and the lipgloss / glamour
// styles derived from them. Every palette exposes the same set of semantic
// tokens (see Palette) so that UI components and the markdown preview style
// themselves consistently, without ever hard-coding a hex value outside this
// package.
package theme

import (
	"image/color"

	"charm.land/lipgloss/v2"
)

// Palette holds the semantic color tokens for one theme. Components must
// style themselves from these tokens only.
type Palette struct {
	Name string
	// ID is unique per palette content: a built-in's Name, or a user
	// theme's Name@<hash of its file> (see LoadUser).
	ID   string
	Dark bool

	Base    color.Color // main background
	Surface color.Color // slightly raised panel background
	Overlay color.Color // selection / highlight background
	Text    color.Color // primary text
	Subtext color.Color // secondary text
	Muted   color.Color // dim markup / borders of unfocused panes
	Accent  color.Color // primary accent
	Accent2 color.Color // secondary accent
	Success color.Color
	Warning color.Color
	Error   color.Color

	Headings [6]color.Color // H1..H6
}

// Key identifies p's colors: caches and the chroma style are keyed by it.
// It is ID, or Name for palettes built by hand without one.
func (p Palette) Key() string {
	if p.ID != "" {
		return p.ID
	}
	return p.Name
}

// hex is a small convenience wrapper around lipgloss.Color for readability
// below: it always receives a "#RRGGBB" literal.
func hex(s string) color.Color {
	return lipgloss.Color(s)
}

// names is the stable, deliberate order in which themes are presented (e.g.
// in the wizard's theme picker and in Names()).
var names = []string{
	"catppuccin-mocha",
	"catppuccin-latte",
	"tokyo-night",
	"tokyo-night-day",
	"rose-pine",
	"rose-pine-dawn",
	"nord",
}

// palettes holds each theme's canonical colours, as published by its
// authors. legiblePalettes then nudges the few tokens that are too faint on
// their backgrounds (see contrast.go), so every theme stays readable.
var palettes = legiblePalettes(map[string]Palette{
	// Catppuccin Mocha — https://catppuccin.com/palette (Mocha)
	"catppuccin-mocha": {
		Name:    "catppuccin-mocha",
		ID:      "catppuccin-mocha",
		Dark:    true,
		Base:    hex("#1e1e2e"), // base
		Surface: hex("#313244"), // surface0
		Overlay: hex("#45475a"), // surface1
		Text:    hex("#cdd6f4"), // text
		Subtext: hex("#a6adc8"), // subtext0
		Muted:   hex("#6c7086"), // overlay0
		Accent:  hex("#cba6f7"), // mauve
		Accent2: hex("#89b4fa"), // blue
		Success: hex("#a6e3a1"), // green
		Warning: hex("#f9e2af"), // yellow
		Error:   hex("#f38ba8"), // red
		Headings: [6]color.Color{
			hex("#f38ba8"), // red
			hex("#fab387"), // peach
			hex("#f9e2af"), // yellow
			hex("#a6e3a1"), // green
			hex("#74c7ec"), // sapphire
			hex("#cba6f7"), // mauve
		},
	},

	// Catppuccin Latte — https://catppuccin.com/palette (Latte)
	"catppuccin-latte": {
		Name:    "catppuccin-latte",
		ID:      "catppuccin-latte",
		Dark:    false,
		Base:    hex("#eff1f5"), // base
		Surface: hex("#ccd0da"), // surface0
		Overlay: hex("#bcc0cc"), // surface1
		Text:    hex("#4c4f69"), // text
		Subtext: hex("#6c6f85"), // subtext0
		Muted:   hex("#9ca0b0"), // overlay0
		Accent:  hex("#8839ef"), // mauve
		Accent2: hex("#1e66f5"), // blue
		Success: hex("#40a02b"), // green
		Warning: hex("#df8e1d"), // yellow
		Error:   hex("#d20f39"), // red
		Headings: [6]color.Color{
			hex("#d20f39"), // red
			hex("#fe640b"), // peach
			hex("#df8e1d"), // yellow
			hex("#40a02b"), // green
			hex("#209fb5"), // sapphire
			hex("#8839ef"), // mauve
		},
	},

	// Tokyo Night — https://github.com/folke/tokyonight.nvim (Night)
	"tokyo-night": {
		Name:    "tokyo-night",
		ID:      "tokyo-night",
		Dark:    true,
		Base:    hex("#1a1b26"), // bg (Night)
		Surface: hex("#24283b"), // bg (Storm), used here as a raised panel
		Overlay: hex("#292e42"), // bg_highlight
		Text:    hex("#c0caf5"), // fg
		Subtext: hex("#a9b1d6"), // fg_dark
		Muted:   hex("#565f89"), // comment
		Accent:  hex("#7aa2f7"), // blue
		Accent2: hex("#bb9af7"), // magenta/purple
		Success: hex("#9ece6a"), // green
		Warning: hex("#e0af68"), // yellow
		Error:   hex("#f7768e"), // red
		Headings: [6]color.Color{
			hex("#f7768e"), // red
			hex("#ff9e64"), // orange
			hex("#e0af68"), // yellow
			hex("#9ece6a"), // green
			hex("#7dcfff"), // cyan
			hex("#bb9af7"), // purple
		},
	},

	// Tokyo Night Day — https://github.com/folke/tokyonight.nvim (Day)
	"tokyo-night-day": {
		Name:    "tokyo-night-day",
		ID:      "tokyo-night-day",
		Dark:    false,
		Base:    hex("#e1e2e7"), // bg
		Surface: hex("#d0d5e3"), // bg_dark
		Overlay: hex("#c4c8da"), // bg_highlight
		Text:    hex("#3760bf"), // fg
		Subtext: hex("#6172b0"), // fg_dark
		Muted:   hex("#848cb5"), // comment
		Accent:  hex("#2e7de9"), // blue
		Accent2: hex("#7847bd"), // purple
		Success: hex("#587539"), // green
		Warning: hex("#8c6c3e"), // yellow
		Error:   hex("#f52a65"), // red
		Headings: [6]color.Color{
			hex("#f52a65"), // red
			hex("#b15c00"), // orange
			hex("#8c6c3e"), // yellow
			hex("#587539"), // green
			hex("#007197"), // cyan
			hex("#9854f1"), // magenta
		},
	},

	// Rosé Pine — https://rosepinetheme.com/palette (Main)
	"rose-pine": {
		Name:    "rose-pine",
		ID:      "rose-pine",
		Dark:    true,
		Base:    hex("#191724"), // base
		Surface: hex("#1f1d2e"), // surface
		Overlay: hex("#403d52"), // highlightMed, used as selection background
		Text:    hex("#e0def4"), // text
		Subtext: hex("#908caa"), // subtle
		Muted:   hex("#6e6a86"), // muted
		Accent:  hex("#c4a7e7"), // iris
		Accent2: hex("#9ccfd8"), // foam
		Success: hex("#31748f"), // pine
		Warning: hex("#f6c177"), // gold
		Error:   hex("#eb6f92"), // love
		Headings: [6]color.Color{
			hex("#eb6f92"), // love
			hex("#f6c177"), // gold
			hex("#ebbcba"), // rose
			hex("#31748f"), // pine
			hex("#9ccfd8"), // foam
			hex("#c4a7e7"), // iris
		},
	},

	// Rosé Pine Dawn — https://rosepinetheme.com/palette (Dawn)
	"rose-pine-dawn": {
		Name:    "rose-pine-dawn",
		ID:      "rose-pine-dawn",
		Dark:    false,
		Base:    hex("#faf4ed"), // base
		Surface: hex("#fffaf3"), // surface
		Overlay: hex("#dfdad9"), // highlightMed, used as selection background
		Text:    hex("#575279"), // text
		Subtext: hex("#797593"), // subtle
		Muted:   hex("#9893a5"), // muted
		Accent:  hex("#907aa9"), // iris
		Accent2: hex("#56949f"), // foam
		Success: hex("#286983"), // pine
		Warning: hex("#ea9d34"), // gold
		Error:   hex("#b4637a"), // love
		Headings: [6]color.Color{
			hex("#b4637a"), // love
			hex("#ea9d34"), // gold
			hex("#d7827e"), // rose
			hex("#286983"), // pine
			hex("#56949f"), // foam
			hex("#907aa9"), // iris
		},
	},

	// Nord — https://www.nordtheme.com/docs/colors-and-palettes
	"nord": {
		Name:    "nord",
		ID:      "nord",
		Dark:    true,
		Base:    hex("#2e3440"), // nord0
		Surface: hex("#3b4252"), // nord1
		Overlay: hex("#434c5e"), // nord2
		Text:    hex("#eceff4"), // nord6
		Subtext: hex("#d8dee9"), // nord4
		Muted:   hex("#4c566a"), // nord3
		Accent:  hex("#88c0d0"), // nord8, frost cyan
		Accent2: hex("#b48ead"), // nord15, purple
		Success: hex("#a3be8c"), // nord14, green
		Warning: hex("#ebcb8b"), // nord13, yellow
		Error:   hex("#bf616a"), // nord11, red
		Headings: [6]color.Color{
			hex("#bf616a"), // nord11, red
			hex("#d08770"), // nord12, orange
			hex("#ebcb8b"), // nord13, yellow
			hex("#a3be8c"), // nord14, green
			hex("#88c0d0"), // nord8, cyan
			hex("#b48ead"), // nord15, purple
		},
	},
})

// Names returns the available palette names in a stable, deliberate order.
func Names() []string {
	out := make([]string, len(names))
	copy(out, names)
	return out
}

// Get looks up a palette by name. ok is false when the name is unknown.
func Get(name string) (Palette, bool) {
	p, ok := palettes[name]
	return p, ok
}
