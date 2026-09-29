package theme

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// Catalog lists and resolves themes: the built-ins, then the user theme
// files in Dir (<ConfigDir>/themes). An empty Dir means built-ins only.
type Catalog struct{ Dir string }

// Names returns the built-in names in their fixed order, then the user theme
// names sorted. User names come from file names alone (files are not
// parsed): a regular *.toml file, after following symlinks, whose name is
// a plain file name (validName) and not a built-in's, so every listed name
// resolves to a file in Dir.
func (c Catalog) Names() []string {
	out := Names()
	if c.Dir == "" {
		return out
	}
	entries, err := os.ReadDir(c.Dir)
	if err != nil {
		return out
	}
	var user []string
	for _, e := range entries {
		n, ok := strings.CutSuffix(e.Name(), ThemeExt)
		if !ok || !validName(n) {
			continue
		}
		if _, builtin := palettes[n]; builtin {
			continue
		}
		// Follow symlinks (dotfile managers link theme files in); skip
		// directories and dangling links.
		if info, err := os.Stat(filepath.Join(c.Dir, e.Name())); err != nil || !info.Mode().IsRegular() {
			continue
		}
		user = append(user, n)
	}
	slices.Sort(user)
	return append(out, user...)
}

// Resolve returns the named palette: a built-in from memory (no I/O, its
// chroma style already registered), else the user theme file, loaded and
// registered by LoadUser.
func (c Catalog) Resolve(name string) (Palette, error) {
	if p, ok := palettes[name]; ok {
		return p, nil
	}
	if c.Dir == "" {
		return Palette{}, fmt.Errorf("unknown theme %q", name)
	}
	return LoadUser(c.Dir, name)
}

// IsUser reports whether name would resolve to a user theme file.
func (c Catalog) IsUser(name string) bool {
	_, builtin := palettes[name]
	return !builtin && c.Dir != ""
}
