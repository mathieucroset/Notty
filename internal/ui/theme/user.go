package theme

import (
	"crypto/sha256"
	hexenc "encoding/hex" // palettes.go already declares func hex
	"fmt"
	"image/color"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"
)

// ThemeExt is a user theme file's extension.
const ThemeExt = ".toml"

// userFile is the TOML shape of a user theme file (<ConfigDir>/themes/<name>.toml).
type userFile struct {
	Base, Surface, Overlay, Text, Subtext, Muted string
	Accent, Accent2, Error                       string
	Success, Warning                             string
	Headings                                     []string
	Dark                                         *bool
}

var hexRE = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// Fallbacks for the optional success/warning tokens, by darkness (Material
// You has no green or yellow role, so the matugen template omits them).
var (
	darkSuccess, darkWarning   = hex("#a6d189"), hex("#e5c890")
	lightSuccess, lightWarning = hex("#40a02b"), hex("#df8e1d")
)

// LoadUser reads dir/<name>.toml, validates it, fills the optional tokens
// and registers its chroma style. The palette's ID is name@<first 8 hex
// chars of the file's sha256>, so it changes whenever the file's bytes do.
// A missing file's error wraps fs.ErrNotExist.
//
// It takes the chroma registry's write lock: never call it while holding
// RLockChroma.
func LoadUser(dir, name string) (Palette, error) {
	path := filepath.Join(dir, name+ThemeExt)
	file := filepath.Base(path)
	b, err := os.ReadFile(path)
	if err != nil {
		return Palette{}, fmt.Errorf("theme %s: %w", name, err)
	}
	p, err := parseUser(name, b)
	if err != nil {
		return Palette{}, fmt.Errorf("theme %s: %w", file, err)
	}
	registerChromaStyle(ChromaStyleName(p), p)
	return p, nil
}

// parseUser builds the palette named name from a theme file's bytes.
func parseUser(name string, b []byte) (Palette, error) {
	var f userFile
	md, err := toml.Decode(string(b), &f)
	if err != nil {
		return Palette{}, fmt.Errorf("parse: %w", err)
	}
	if und := md.Undecoded(); len(und) > 0 {
		return Palette{}, fmt.Errorf("unknown key %q", und[0].String())
	}
	sum := sha256.Sum256(b)
	p := Palette{Name: name, ID: name + "@" + hexenc.EncodeToString(sum[:])[:8]}

	required := []struct {
		key string
		val string
		dst *color.Color
	}{
		{"base", f.Base, &p.Base},
		{"surface", f.Surface, &p.Surface},
		{"overlay", f.Overlay, &p.Overlay},
		{"text", f.Text, &p.Text},
		{"subtext", f.Subtext, &p.Subtext},
		{"muted", f.Muted, &p.Muted},
		{"accent", f.Accent, &p.Accent},
		{"accent2", f.Accent2, &p.Accent2},
		{"error", f.Error, &p.Error},
	}
	for _, r := range required {
		if r.val == "" {
			return Palette{}, fmt.Errorf("missing %q", r.key)
		}
		c, err := parseHex(r.key, r.val)
		if err != nil {
			return Palette{}, err
		}
		*r.dst = c
	}

	if f.Dark != nil {
		p.Dark = *f.Dark
	} else {
		p.Dark = luminance(p.Base) < 0.5
	}

	p.Success, p.Warning = lightSuccess, lightWarning
	if p.Dark {
		p.Success, p.Warning = darkSuccess, darkWarning
	}
	optional := []struct {
		key string
		val string
		dst *color.Color
	}{
		{"success", f.Success, &p.Success},
		{"warning", f.Warning, &p.Warning},
	}
	for _, o := range optional {
		if o.val == "" {
			continue
		}
		c, err := parseHex(o.key, o.val)
		if err != nil {
			return Palette{}, err
		}
		*o.dst = c
	}

	if len(f.Headings) > len(p.Headings) {
		return Palette{}, fmt.Errorf("headings has %d entries, at most %d", len(f.Headings), len(p.Headings))
	}
	for i := range p.Headings {
		if i >= len(f.Headings) {
			p.Headings[i] = [2]color.Color{p.Accent, p.Accent2}[i%2]
			continue
		}
		c, err := parseHex(fmt.Sprintf("headings[%d]", i), f.Headings[i])
		if err != nil {
			return Palette{}, err
		}
		p.Headings[i] = c
	}
	return p, nil
}

// parseHex parses a "#RRGGBB" color (case-insensitive) for key.
func parseHex(key, v string) (color.Color, error) {
	if !hexRE.MatchString(v) {
		return nil, fmt.Errorf("%q: %q is not a #RRGGBB color", key, v)
	}
	return hex(strings.ToLower(v)), nil
}
