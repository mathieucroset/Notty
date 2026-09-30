package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mathieucroset/notty/internal/ui/theme"
)

func TestThemeCommand(t *testing.T) {
	tests := []struct {
		name string
		args []string
		// setup prepares the fixture and returns the config path to use.
		setup      func(t *testing.T, f *fixture) string
		wantCode   int
		wantTpl    string // expected template content, "" = not checked
		wantNoTpl  bool   // the template must not exist afterwards
		wantStdout []string
		wantStderr []string
	}{
		{
			name:     "writes the template",
			args:     []string{"theme", "matugen"},
			wantCode: 0,
			wantTpl:  theme.MatugenTemplate(),
			wantStdout: []string{
				"[templates.notty]", "Noctalia 5", "[theme.templates.user.notty]",
				"input_path", "output_path",
				"{dir}/matugen-template.toml", "{dir}/themes/matugen.toml",
				`theme = "matugen"`, "Wrote",
			},
		},
		{
			name: "leaves an existing template alone",
			args: []string{"theme", "matugen"},
			setup: func(t *testing.T, f *fixture) string {
				f.writeConfig(t, "")
				tpl := filepath.Join(filepath.Dir(f.configPath), "matugen-template.toml")
				if err := os.WriteFile(tpl, []byte("mine"), 0o600); err != nil {
					t.Fatal(err)
				}
				return f.configPath
			},
			wantCode:   0,
			wantTpl:    "mine",
			wantStdout: []string{"already exists", "[templates.notty]", "[theme.templates.user.notty]", `theme = "matugen"`},
		},
		{
			name:       "no argument",
			args:       []string{"theme"},
			wantCode:   2,
			wantStderr: []string{"usage: notty theme matugen"},
		},
		{
			name:       "unknown argument",
			args:       []string{"theme", "bogus"},
			wantCode:   2,
			wantStderr: []string{"usage: notty theme matugen"},
		},
		{
			name:       "extra argument",
			args:       []string{"theme", "matugen", "extra"},
			wantCode:   2,
			wantStderr: []string{"usage: notty theme matugen"},
		},
		{
			name: "unwritable config dir",
			args: []string{"theme", "matugen"},
			setup: func(t *testing.T, f *fixture) string {
				file := filepath.Join(f.dir, "file")
				if err := os.WriteFile(file, nil, 0o600); err != nil {
					t.Fatal(err)
				}
				return filepath.Join(file, "config.toml")
			},
			wantCode:   1,
			wantStderr: []string{"notty:"},
		},
		{
			name: "read-only config dir",
			args: []string{"theme", "matugen"},
			setup: func(t *testing.T, f *fixture) string {
				if os.Geteuid() == 0 {
					t.Skip("root ignores directory permissions")
				}
				dir := filepath.Dir(f.configPath)
				if err := os.MkdirAll(dir, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(dir, 0o500); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
				return f.configPath
			},
			wantCode:   1,
			wantNoTpl:  true,
			wantStderr: []string{"notty:"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// A home outside the temp dirs, so paths are printed in full.
			t.Setenv("HOME", t.TempDir())
			f := newFixture(t)
			if tt.setup != nil {
				f.configPath = tt.setup(t, f)
			}
			dir := filepath.Dir(f.configPath)
			if code := run(tt.args, f.env()); code != tt.wantCode {
				t.Fatalf("exit code %d, want %d; stderr %q", code, tt.wantCode, f.stderr.String())
			}
			if f.got != nil {
				t.Error("started the TUI")
			}
			if tt.wantTpl != "" {
				b, err := os.ReadFile(filepath.Join(dir, "matugen-template.toml"))
				if err != nil {
					t.Fatal(err)
				}
				if string(b) != tt.wantTpl {
					t.Errorf("template = %q, want %q", b, tt.wantTpl)
				}
			}
			for _, want := range tt.wantStdout {
				want = strings.ReplaceAll(want, "{dir}", filepath.ToSlash(dir))
				if !strings.Contains(f.stdout.String(), want) {
					t.Errorf("stdout %q lacks %q", f.stdout.String(), want)
				}
			}
			for _, want := range tt.wantStderr {
				if !strings.Contains(f.stderr.String(), want) {
					t.Errorf("stderr %q lacks %q", f.stderr.String(), want)
				}
			}
			if tt.wantCode == 2 || tt.wantNoTpl {
				if _, err := os.Stat(filepath.Join(dir, "matugen-template.toml")); !os.IsNotExist(err) {
					t.Errorf("template written (stat err %v)", err)
				}
			}
		})
	}
}

func TestTildePath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	tests := []struct {
		name, path, want string
	}{
		{"under home", filepath.Join(home, ".config", "notty", "x.toml"), "~/.config/notty/x.toml"},
		{"home itself", home, "~"},
		{"outside home", "/etc/notty.toml", "/etc/notty.toml"},
		{"sibling with home as prefix", home + "x/a.toml", home + "x/a.toml"},
		{"dot-dot dir under home", filepath.Join(home, "..foo", "a.toml"), "~/..foo/a.toml"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tildePath(tt.path); got != tt.want {
				t.Errorf("tildePath(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}
