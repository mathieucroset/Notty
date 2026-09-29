package icons

import (
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/mathieucroset/notty/internal/config"
)

// glyphs returns every glyph field of s by name.
func glyphs(s Set) map[string]string {
	out := map[string]string{}
	v := reflect.ValueOf(s)
	for i := range v.NumField() {
		f := v.Type().Field(i)
		if f.Name == "Name" {
			continue
		}
		out[f.Name] = v.Field(i).String()
	}
	return out
}

// TestEverySetIsComplete checks that no set leaves a glyph empty, so a
// component never draws a gap where an icon belongs.
func TestEverySetIsComplete(t *testing.T) {
	for _, name := range Names() {
		t.Run(name, func(t *testing.T) {
			s, ok := Get(name)
			if !ok {
				t.Fatalf("Get(%q) failed", name)
			}
			if s.Name != name {
				t.Errorf("Name = %q", s.Name)
			}
			for field, g := range glyphs(s) {
				if g == "" {
					t.Errorf("%s is empty", field)
				}
				if strings.ContainsAny(g, "\n\t") {
					t.Errorf("%s = %q holds a control character", field, g)
				}
			}
		})
	}
}

// TestGlyphWidths keeps icons narrow so rows stay aligned: every nerd and
// unicode glyph is one column wide except the image chip's emoji, and
// ASCII glyphs are short words at most.
func TestGlyphWidths(t *testing.T) {
	tests := []struct {
		set  string
		max  int
		wide map[string]int
	}{
		{"nerd", 1, nil},
		{"unicode", 1, map[string]int{"Image": 2}},
		{"ascii", 3, map[string]int{"Image": 5}},
	}
	for _, tt := range tests {
		t.Run(tt.set, func(t *testing.T) {
			s, _ := Get(tt.set)
			for field, g := range glyphs(s) {
				limit := tt.max
				if w, ok := tt.wide[field]; ok {
					limit = w
				}
				if w := ansi.StringWidth(g); w < 1 || w > limit {
					t.Errorf("%s = %q is %d columns wide, want 1..%d", field, g, w, limit)
				}
			}
		})
	}
}

func TestSpinner(t *testing.T) {
	for _, name := range Names() {
		s, _ := Get(name)
		frames := s.Spinner()
		if len(frames) < 2 {
			t.Errorf("%s: %d spinner frames", name, len(frames))
		}
		for _, f := range frames {
			if ansi.StringWidth(f) != 1 {
				t.Errorf("%s: spinner frame %q is not one column", name, f)
			}
			if name == "ascii" && (len(f) != 1 || f[0] > 0x7e) {
				t.Errorf("ascii spinner frame %q is not ASCII", f)
			}
		}
	}
}

func TestASCIISetIsASCII(t *testing.T) {
	s, _ := Get("ascii")
	for field, g := range glyphs(s) {
		for _, r := range g {
			if r > 0x7e || r < 0x20 {
				t.Errorf("%s = %q is not printable ASCII", field, g)
			}
		}
	}
}

func TestGet(t *testing.T) {
	tests := []struct {
		name   string
		want   string
		wantOK bool
	}{
		{"nerd", "nerd", true},
		{"unicode", "unicode", true},
		{"ascii", "ascii", true},
		{"", "unicode", false},
		{"emoji", "unicode", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, ok := Get(tt.name)
			if ok != tt.wantOK || s.Name != tt.want {
				t.Errorf("Get(%q) = %q, %v; want %q, %v", tt.name, s.Name, ok, tt.want, tt.wantOK)
			}
		})
	}
	if Default().Name != "unicode" {
		t.Errorf("Default() = %q, want unicode", Default().Name)
	}
}

// TestNamesMatchConfig keeps the sets and the config's accepted values in
// step.
func TestNamesMatchConfig(t *testing.T) {
	if got := Names(); !slices.Equal(got, config.IconSets) {
		t.Errorf("Names() = %v, config.IconSets = %v", got, config.IconSets)
	}
	if config.Default().Icons != Default().Name {
		t.Errorf("config default %q, icons default %q", config.Default().Icons, Default().Name)
	}
}

// TestSetsDiffer checks that the sets are not accidental copies.
func TestSetsDiffer(t *testing.T) {
	n, _ := Get("nerd")
	u, _ := Get("unicode")
	a, _ := Get("ascii")
	for _, field := range []string{"FolderClosed", "TaskOpen", "Synced", "Pin"} {
		gn, gu, ga := glyphs(n)[field], glyphs(u)[field], glyphs(a)[field]
		if gn == gu || gu == ga || gn == ga {
			t.Errorf("%s is shared between sets: %q %q %q", field, gn, gu, ga)
		}
	}
}
