package theme

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// readmeMarker precedes the example theme file in the README.
const readmeMarker = "<!-- example theme file: loaded by internal/ui/theme/readme_test.go -->"

// TestReadmeThemeExample: the example theme file in the README loads.
func TestReadmeThemeExample(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "..", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	// A Windows checkout (core.autocrlf) may have CRLF line endings.
	readme := strings.ReplaceAll(string(b), "\r\n", "\n")
	_, rest, ok := strings.Cut(readme, readmeMarker)
	if !ok {
		t.Fatal("README has no example theme marker")
	}
	_, rest, ok = strings.Cut(rest, "```toml\n")
	if !ok {
		t.Fatal("no toml block after the marker")
	}
	example, _, ok := strings.Cut(rest, "```")
	if !ok {
		t.Fatal("unterminated toml block")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "dusk"+ThemeExt), []byte(example), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err := LoadUser(dir, "dusk")
	if err != nil {
		t.Fatalf("README example does not load: %v\n%s", err, example)
	}
	if p.Name != "dusk" || !p.Dark {
		t.Errorf("loaded %q, dark %v; want dusk, dark", p.Name, p.Dark)
	}
}
