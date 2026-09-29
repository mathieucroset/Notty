package theme

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	chromastyles "github.com/alecthomas/chroma/v2/styles"
)

const fullTheme = `
base = "#141318"
surface = "#201f24"
overlay = "#36343a"
text = "#E6E1E9"
subtext = "#cac4cf"
muted = "#948f99"
accent = "#cfbcff"
accent2 = "#f2b7c2"
error = "#ffb4ab"
`

func writeTheme(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name+".toml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func checkHeadings(t *testing.T, p Palette, want []string) {
	t.Helper()
	for i, w := range want {
		if got := hexOf(p.Headings[i]); got != w {
			t.Errorf("H%d = %s, want %s", i+1, got, w)
		}
	}
}

func TestLoadUser(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		wantErr string // substring; "" = success
		check   func(t *testing.T, p Palette)
	}{
		{name: "full, dark inferred", body: fullTheme, check: func(t *testing.T, p Palette) {
			if !p.Dark {
				t.Error("dark not inferred")
			}
			if hexOf(p.Text) != "#e6e1e9" {
				t.Errorf("text %s", hexOf(p.Text))
			}
			if hexOf(p.Success) != "#a6d189" || hexOf(p.Warning) != "#e5c890" {
				t.Errorf("dark fallbacks: %s %s", hexOf(p.Success), hexOf(p.Warning))
			}
			checkHeadings(t, p, []string{"#cfbcff", "#f2b7c2", "#cfbcff", "#f2b7c2", "#cfbcff", "#f2b7c2"})
			if p.Name != "t" || !strings.HasPrefix(p.ID, "t@") || len(p.ID) != len("t@")+8 {
				t.Errorf("name/id %q %q", p.Name, p.ID)
			}
		}},
		{name: "light inferred with light fallbacks", body: strings.Replace(fullTheme, `base = "#141318"`, `base = "#fdf8fd"`, 1), check: func(t *testing.T, p Palette) {
			if p.Dark {
				t.Error("dark on light base")
			}
			if hexOf(p.Success) != "#40a02b" || hexOf(p.Warning) != "#df8e1d" {
				t.Errorf("light fallbacks: %s %s", hexOf(p.Success), hexOf(p.Warning))
			}
		}},
		{name: "explicit dark overrides", body: fullTheme + "dark = false\n", check: func(t *testing.T, p Palette) {
			if p.Dark {
				t.Error("dark = false ignored")
			}
		}},
		{name: "explicit success, warning and headings", body: fullTheme + "success = \"#00ff00\"\nwarning = \"#FFFF00\"\nheadings = [\"#111111\", \"#222222\", \"#333333\"]\n", check: func(t *testing.T, p Palette) {
			if hexOf(p.Success) != "#00ff00" || hexOf(p.Warning) != "#ffff00" {
				t.Errorf("success/warning: %s %s", hexOf(p.Success), hexOf(p.Warning))
			}
			// level i >= 3 falls back to [accent, accent2][i%2]
			checkHeadings(t, p, []string{"#111111", "#222222", "#333333", "#f2b7c2", "#cfbcff", "#f2b7c2"})
		}},
		{name: "missing required", body: strings.Replace(fullTheme, "accent2 = \"#f2b7c2\"\n", "", 1), wantErr: `"accent2"`},
		{name: "missing base", body: strings.Replace(fullTheme, "base = \"#141318\"\n", "", 1), wantErr: `"base"`},
		{name: "bad hex", body: strings.Replace(fullTheme, `"#cfbcff"`, `"cfbcff"`, 1), wantErr: `"accent"`},
		{name: "short hex", body: strings.Replace(fullTheme, `"#cfbcff"`, `"#fff"`, 1), wantErr: `"accent"`},
		{name: "bad optional", body: fullTheme + "warning = \"yellow\"\n", wantErr: `"warning"`},
		{name: "bad heading", body: fullTheme + "headings = [\"#111111\", \"#12345g\"]\n", wantErr: `"headings[1]"`},
		{name: "unknown key", body: fullTheme + "acent = \"#000000\"\n", wantErr: `"acent"`},
		{name: "too many headings", body: fullTheme + `headings = ["#000000","#000000","#000000","#000000","#000000","#000000","#000000"]` + "\n", wantErr: "headings"},
		{name: "not toml", body: "base = ", wantErr: "t.toml"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			writeTheme(t, dir, "t", tt.body)
			p, err := LoadUser(dir, "t")
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			tt.check(t, p)
		})
	}
}

func TestLoadUserIDTracksContent(t *testing.T) {
	dir := t.TempDir()
	writeTheme(t, dir, "t", fullTheme)
	a, err := LoadUser(dir, "t")
	if err != nil {
		t.Fatal(err)
	}
	again, err := LoadUser(dir, "t")
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != a.ID {
		t.Fatalf("ID changed without a rewrite: %q then %q", a.ID, again.ID)
	}
	writeTheme(t, dir, "t", fullTheme+"\n# changed\n")
	b, err := LoadUser(dir, "t")
	if err != nil {
		t.Fatal(err)
	}
	if a.ID == b.ID {
		t.Fatal("ID unchanged after rewrite")
	}
}

func TestLoadUserRegistersChroma(t *testing.T) {
	dir := t.TempDir()
	writeTheme(t, dir, "t", fullTheme)
	p, err := LoadUser(dir, "t")
	if err != nil {
		t.Fatal(err)
	}
	unlock := RLockChroma()
	defer unlock()
	if _, ok := chromastyles.Registry[strings.ToLower(ChromaStyleName(p))]; !ok {
		t.Fatal("style not registered")
	}
}

func TestLoadUserMissingFile(t *testing.T) {
	_, err := LoadUser(t.TempDir(), "nope")
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("err = %v", err)
	}
}
