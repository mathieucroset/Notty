package theme

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"testing"
)

var placeholderRE = regexp.MustCompile(`\{\{\s*colors\.([a-z_]+)\.default\.hex\s*\}\}`)

// TestMatugenTemplateRenders fills every placeholder with a deterministic
// fake color, checks the result loads as a user theme, and checks the
// template only references real Material 3 roles.
func TestMatugenTemplateRenders(t *testing.T) {
	tpl := MatugenTemplate()
	roles := placeholderRE.FindAllStringSubmatch(tpl, -1)
	if len(roles) == 0 {
		t.Fatal("no placeholders")
	}
	i := 0
	out := placeholderRE.ReplaceAllStringFunc(tpl, func(string) string {
		i++
		return fmt.Sprintf("#%06x", i*0x0a0b0c%0xffffff)
	})
	dir := t.TempDir()
	writeTheme(t, dir, "matugen", out)
	if _, err := LoadUser(dir, "matugen"); err != nil {
		t.Fatal(err)
	}
	for _, r := range roles {
		if !slices.Contains(MatugenRoles, r[1]) {
			t.Errorf("unknown Material role %q", r[1])
		}
	}
}

// TestMatugenRealBinary renders the template with the real matugen, when
// installed, in both modes, and loads the result. Every input and output
// path (and HOME, for matugen's own files) is under t.TempDir().
func TestMatugenRealBinary(t *testing.T) {
	bin, err := exec.LookPath("matugen")
	if err != nil {
		t.Skip("matugen not installed")
	}
	for _, mode := range []string{"dark", "light"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			in := filepath.Join(dir, "in.toml")
			if err := os.WriteFile(in, []byte(MatugenTemplate()), 0o600); err != nil {
				t.Fatal(err)
			}
			outDir := filepath.Join(dir, "themes")
			cfg := fmt.Sprintf("[config]\n\n[templates.notty]\ninput_path = %q\noutput_path = %q\n", in, filepath.Join(outDir, "matugen.toml"))
			cfgPath := filepath.Join(dir, "cfg.toml")
			if err := os.WriteFile(cfgPath, []byte(cfg), 0o600); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(bin, "-c", cfgPath, "-m", mode, "color", "hex", "#6750a4", "-q")
			cmd.Dir = dir
			// Keep matugen's caches and default config out of the real home.
			cmd.Env = append(os.Environ(), "HOME="+dir, "XDG_CONFIG_HOME="+filepath.Join(dir, ".config"), "XDG_CACHE_HOME="+filepath.Join(dir, ".cache"))
			if b, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("matugen: %v\n%s", err, b)
			}
			p, err := LoadUser(outDir, "matugen")
			if err != nil {
				t.Fatal(err)
			}
			if p.Dark != (mode == "dark") {
				t.Errorf("dark = %v in %s mode", p.Dark, mode)
			}
		})
	}
}
