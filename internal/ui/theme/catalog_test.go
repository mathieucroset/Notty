package theme

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestCatalogNames(t *testing.T) {
	dir := t.TempDir()
	writeTheme(t, dir, "zeta", fullTheme)
	writeTheme(t, dir, "alpha", "broken") // listed: names are not parsed
	writeTheme(t, dir, "nord", fullTheme) // shadows a built-in: ignored
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{".toml", ".hidden.toml", `back\slash.toml`} { // not plain names: skipped
		if err := os.WriteFile(filepath.Join(dir, n), []byte(fullTheme), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "sub.toml"), 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "linked.toml")
	if err := os.WriteFile(target, []byte(fullTheme), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, "linked.toml")); err != nil {
		t.Skip("no symlinks:", err)
	}
	if err := os.Symlink(filepath.Join(dir, "gone"), filepath.Join(dir, "dangling.toml")); err != nil {
		t.Fatal(err)
	}
	got := Catalog{Dir: dir}.Names()
	want := append(Names(), "alpha", "linked", "zeta") // symlinks followed, dangling skipped
	if !slices.Equal(got, want) {
		t.Fatalf("got  %v\nwant %v", got, want)
	}
}

func TestCatalogNamesWithoutDir(t *testing.T) {
	for _, dir := range []string{"", filepath.Join(t.TempDir(), "missing")} {
		if got := (Catalog{Dir: dir}).Names(); !slices.Equal(got, Names()) {
			t.Errorf("dir %q: %v", dir, got)
		}
	}
}

func TestCatalogResolve(t *testing.T) {
	dir := t.TempDir()
	writeTheme(t, dir, "mine", fullTheme)
	writeTheme(t, dir, "nord", fullTheme)
	c := Catalog{Dir: dir}
	tests := []struct {
		name    string
		wantID  string // prefix
		wantErr string
	}{
		{"nord", "nord", ""}, // built-in wins over the file
		{"mine", "mine@", ""},
		{"absent", "", "absent"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := c.Resolve(tt.name)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil || !strings.HasPrefix(p.ID, tt.wantID) {
				t.Fatalf("Resolve = %q, %v; want ID prefix %q", p.ID, err, tt.wantID)
			}
		})
	}
	if nord, _ := c.Resolve("nord"); nord.ID != "nord" {
		t.Errorf("built-in nord shadowed: ID %q", nord.ID)
	}
	if _, err := (Catalog{}).Resolve("mine"); err == nil {
		t.Error("empty Dir resolved a user theme")
	}
}

func TestCatalogIsUser(t *testing.T) {
	tests := []struct {
		dir, name string
		want      bool
	}{
		{"/themes", "matugen", true},
		{"/themes", "nord", false},
		{"", "matugen", false},
	}
	for _, tt := range tests {
		if got := (Catalog{Dir: tt.dir}).IsUser(tt.name); got != tt.want {
			t.Errorf("Catalog{%q}.IsUser(%q) = %v, want %v", tt.dir, tt.name, got, tt.want)
		}
	}
}
