package theme

import (
	"crypto/sha256"
	hexenc "encoding/hex" // palettes.go already declares func hex
	"errors"
	"fmt"
	"image/color"
	"io"
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

// ErrInvalidName is wrapped by LoadUser's error for a theme name that is not
// a plain file name (see validName).
var ErrInvalidName = errors.New("not a plain file name")

// maxThemeSize bounds a theme file, so an endless file cannot exhaust the UI
// goroutine that loads it.
const maxThemeSize = 64 << 10

// validName reports whether name is a plain file name usable as a theme
// name: not empty, no leading ".", no "/" or "\" (on every OS), and local.
// Names can come from the vault's synced settings, so anything that could
// reach outside the themes directory is refused.
func validName(name string) bool {
	return name != "" &&
		!strings.HasPrefix(name, ".") &&
		!strings.ContainsAny(name, `/\`) &&
		filepath.IsLocal(name)
}

// LoadUser reads dir/<name>.toml, validates it, fills the optional tokens
// and registers its chroma style. The palette's ID is name@<first 8 hex
// chars of the file's sha256>, so it changes whenever the file's bytes do.
//
// name must be a plain file name (validName), else the error wraps
// ErrInvalidName and nothing is read. The file must be a regular file
// (symlinks followed) of at most 64 KiB. A missing file's error wraps
// fs.ErrNotExist. Every other error is prefixed "theme <name>.toml: ".
//
// It takes the chroma registry's write lock: never call it while holding
// RLockChroma.
func LoadUser(dir, name string) (Palette, error) {
	if !validName(name) {
		return Palette{}, fmt.Errorf("theme %q: %w", name, ErrInvalidName)
	}
	file := name + ThemeExt
	b, err := readTheme(filepath.Join(dir, file))
	if err != nil {
		return Palette{}, fmt.Errorf("theme %s: %w", file, err)
	}
	p, err := parseUser(name, b)
	if err != nil {
		return Palette{}, fmt.Errorf("theme %s: %w", file, err)
	}
	registerChromaStyle(ChromaStyleName(p), p)
	return p, nil
}

// readTheme reads the regular file at path, of at most maxThemeSize bytes.
func readTheme(path string) ([]byte, error) {
	// Stat before opening: opening a FIFO for reading blocks until a writer
	// shows up.
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("not a regular file (%s)", info.Mode().Type())
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	// Stat the opened file too, in case the path was swapped meanwhile.
	if info, err = f.Stat(); err != nil {
		return nil, fmt.Errorf("stat: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("not a regular file (%s)", info.Mode().Type())
	}
	b, err := io.ReadAll(io.LimitReader(f, maxThemeSize+1))
	if err != nil {
		return nil, fmt.Errorf("read: %w", err)
	}
	if len(b) > maxThemeSize {
		return nil, fmt.Errorf("larger than %d KiB", maxThemeSize>>10)
	}
	return b, nil
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
			return Palette{}, fmt.Errorf("%q: missing or empty", r.key)
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
