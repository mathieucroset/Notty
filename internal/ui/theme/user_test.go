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
		{name: "missing required", body: strings.Replace(fullTheme, "accent2 = \"#f2b7c2\"\n", "", 1), wantErr: `"accent2": missing or empty`},
		{name: "missing base", body: strings.Replace(fullTheme, "base = \"#141318\"\n", "", 1), wantErr: `"base": missing or empty`},
		{name: "empty base", body: strings.Replace(fullTheme, `base = "#141318"`, `base = ""`, 1), wantErr: `"base": missing or empty`},
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
				if !strings.HasPrefix(err.Error(), "theme t.toml: ") {
					t.Errorf("err = %v, want prefix %q", err, "theme t.toml: ")
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
	if !strings.HasPrefix(err.Error(), "theme nope.toml: ") {
		t.Errorf("err = %v, want prefix %q", err, "theme nope.toml: ")
	}
}

// TestLoadUserInvalidName checks that a name that is not a plain file name
// is rejected before any I/O, even when the file it points at exists.
func TestLoadUserInvalidName(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "themes")
	if err := os.MkdirAll(filepath.Join(dir, "a"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeTheme(t, root, "x", fullTheme)                    // reachable as "../x"
	writeTheme(t, filepath.Join(dir, "a"), "b", fullTheme) // reachable as "a/b"
	writeTheme(t, dir, ".hidden", fullTheme)
	tests := []string{"../x", "a/b", `a\b`, ".hidden", "", "..", ".", "/abs", `..\x`}
	for _, name := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := LoadUser(dir, name)
			if !errors.Is(err, ErrInvalidName) {
				t.Fatalf("LoadUser(%q) err = %v, want ErrInvalidName", name, err)
			}
		})
	}
}

func TestLoadUserFileKind(t *testing.T) {
	const limit = 64 << 10
	padded := func(n int) string { // a valid theme of exactly n bytes
		body := fullTheme + "#"
		return body + strings.Repeat("x", n-len(body))
	}
	tests := []struct {
		name    string
		setup   func(t *testing.T, dir string)
		wantErr string // "" = success
	}{
		{"directory", func(t *testing.T, dir string) {
			if err := os.Mkdir(filepath.Join(dir, "x.toml"), 0o700); err != nil {
				t.Fatal(err)
			}
		}, "not a regular file"},
		{"oversized", func(t *testing.T, dir string) { writeTheme(t, dir, "x", padded(limit+1)) }, "larger than"},
		{"at the limit", func(t *testing.T, dir string) { writeTheme(t, dir, "x", padded(limit)) }, ""},
		{"symlink to a regular file", func(t *testing.T, dir string) {
			target := filepath.Join(t.TempDir(), "elsewhere.toml")
			if err := os.WriteFile(target, []byte(fullTheme), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, filepath.Join(dir, "x.toml")); err != nil {
				t.Skip("no symlinks:", err)
			}
		}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			tt.setup(t, dir)
			_, err := LoadUser(dir, "x")
			if tt.wantErr == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) || !strings.HasPrefix(err.Error(), "theme x.toml: ") {
				t.Fatalf("err = %v, want \"theme x.toml: ...%s...\"", err, tt.wantErr)
			}
		})
	}
}
