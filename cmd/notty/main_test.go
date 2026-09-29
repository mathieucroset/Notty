package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/mathieucroset/notty/internal/ui/app"
)

type fixture struct {
	dir        string
	configPath string
	vaultDir   string
	stdout     bytes.Buffer
	stderr     bytes.Buffer
	gitFound   bool
	tuiErr     error
	got        *app.Options
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	dir := t.TempDir()
	return &fixture{
		dir:        dir,
		configPath: filepath.Join(dir, "config", "config.toml"),
		vaultDir:   filepath.Join(dir, "Notes"),
		gitFound:   true,
	}
}

func (f *fixture) writeConfig(t *testing.T, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(f.configPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(f.configPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// makeRepo creates the vault with a .git directory and a pin.
func (f *fixture) makeRepo(t *testing.T) {
	t.Helper()
	for _, d := range []string{".git", ".notty"} {
		if err := os.MkdirAll(filepath.Join(f.vaultDir, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(f.vaultDir, ".notty", "state.json"), []byte(`{"pins":["a.md"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
}

func (f *fixture) env() env {
	return env{
		stdout:     &f.stdout,
		stderr:     &f.stderr,
		configPath: f.configPath,
		stateDir:   filepath.Join(f.dir, "state"),
		lookPath: func(name string) (string, error) {
			if f.gitFound {
				return "/usr/bin/" + name, nil
			}
			return "", errors.New("not found")
		},
		runTUI: func(opts app.Options) error {
			f.got = &opts
			return f.tuiErr
		},
	}
}

func TestVersion(t *testing.T) {
	f := newFixture(t)
	if code := run([]string{"--version"}, f.env()); code != 0 {
		t.Fatalf("exit code %d", code)
	}
	if got := f.stdout.String(); got != "notty "+version+"\n" {
		t.Errorf("stdout = %q", got)
	}
	if f.got != nil {
		t.Error("--version started the TUI")
	}
}

func TestBadArgs(t *testing.T) {
	for _, args := range [][]string{{"--nope"}, {"stray"}} {
		f := newFixture(t)
		if code := run(args, f.env()); code != 2 {
			t.Errorf("run(%v) = %d, want 2", args, code)
		}
		if f.got != nil {
			t.Errorf("run(%v) started the TUI", args)
		}
	}
}

func TestNormalStartup(t *testing.T) {
	f := newFixture(t)
	f.makeRepo(t)
	f.writeConfig(t, "vault = \""+filepath.ToSlash(f.vaultDir)+"\"\ntheme = \"nord\"\n")
	if code := run(nil, f.env()); code != 0 {
		t.Fatalf("exit code %d, stderr %q", code, f.stderr.String())
	}
	o := f.got
	if o == nil {
		t.Fatal("TUI not started")
	}
	if o.WizardNeeded {
		t.Error("WizardNeeded = true for a configured git vault")
	}
	if o.Vault == nil || o.Vault.Root != f.vaultDir {
		t.Errorf("Vault = %+v, want root %s", o.Vault, f.vaultDir)
	}
	if o.Config.Theme != "nord" || o.Palette.Name != "nord" {
		t.Errorf("theme = %q, palette = %q, want nord", o.Config.Theme, o.Palette.Name)
	}
	if o.Local == nil || !strings.HasPrefix(o.LocalPath, filepath.Join(f.dir, "state")) {
		t.Errorf("Local = %v, LocalPath = %q", o.Local, o.LocalPath)
	}
	if o.Pins == nil || !reflect.DeepEqual(o.Pins.Pins, []string{"a.md"}) {
		t.Errorf("Pins = %+v", o.Pins)
	}
}

func TestVaultFlagOverridesConfig(t *testing.T) {
	f := newFixture(t)
	f.makeRepo(t)
	f.writeConfig(t, "vault = \"/does/not/matter\"\n")
	if code := run([]string{"--vault", f.vaultDir}, f.env()); code != 0 {
		t.Fatalf("exit code %d, stderr %q", code, f.stderr.String())
	}
	if f.got.Vault == nil || f.got.Vault.Root != f.vaultDir {
		t.Errorf("Vault = %+v, want %s", f.got.Vault, f.vaultDir)
	}
	if f.got.Config.Vault != f.vaultDir {
		t.Errorf("Config.Vault = %q", f.got.Config.Vault)
	}
}

func TestWizardDecision(t *testing.T) {
	tests := []struct {
		name       string
		config     bool
		repo       bool
		git        bool
		wantWizard bool
	}{
		{"configured repo", true, true, true, false},
		{"no config file", false, true, true, true},
		{"vault not a repo", true, false, true, true},
		{"no config, no repo", false, false, true, true},
		{"git missing", false, false, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			f.gitFound = tt.git
			if tt.repo {
				f.makeRepo(t)
			}
			if tt.config {
				f.writeConfig(t, "theme = \"nord\"\n")
			}
			if code := run([]string{"--vault", f.vaultDir}, f.env()); code != 0 {
				t.Fatalf("exit code %d, stderr %q", code, f.stderr.String())
			}
			if f.got.WizardNeeded != tt.wantWizard {
				t.Errorf("WizardNeeded = %v, want %v", f.got.WizardNeeded, tt.wantWizard)
			}
			if tt.wantWizard {
				if f.got.Vault != nil {
					t.Error("wizard mode opened the vault")
				}
				if !tt.repo {
					if _, err := os.Stat(f.vaultDir); !os.IsNotExist(err) {
						t.Error("wizard mode created the vault directory")
					}
				}
			} else if f.got.Vault == nil {
				t.Error("vault not opened")
			}
		})
	}
}

func TestUnknownThemeFallsBack(t *testing.T) {
	f := newFixture(t)
	f.makeRepo(t)
	f.writeConfig(t, "theme = \"no-such-theme\"\n")
	if code := run([]string{"--vault", f.vaultDir}, f.env()); code != 0 {
		t.Fatalf("exit code %d", code)
	}
	if f.got.Palette.Name != "catppuccin-mocha" {
		t.Errorf("palette = %q, want catppuccin-mocha", f.got.Palette.Name)
	}
}

func TestErrors(t *testing.T) {
	t.Run("bad config", func(t *testing.T) {
		f := newFixture(t)
		f.writeConfig(t, "theme = [broken\n")
		if code := run([]string{"--vault", f.vaultDir}, f.env()); code != 1 {
			t.Errorf("exit code %d, want 1", code)
		}
		if !strings.Contains(f.stderr.String(), "notty:") {
			t.Errorf("stderr = %q", f.stderr.String())
		}
	})
	t.Run("tui error", func(t *testing.T) {
		f := newFixture(t)
		f.makeRepo(t)
		f.writeConfig(t, "")
		f.tuiErr = errors.New("boom")
		if code := run([]string{"--vault", f.vaultDir}, f.env()); code != 1 {
			t.Errorf("exit code %d, want 1", code)
		}
		if !strings.Contains(f.stderr.String(), "boom") {
			t.Errorf("stderr = %q", f.stderr.String())
		}
	})
}
