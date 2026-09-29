package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/colorprofile"

	"github.com/mathieucroset/notty/internal/imgrender"
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
	during     func() // runs while the TUI would be running
	// detectedWith records the protocol setting each detection ran with.
	detectedWith []string
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
		lockWait: 300 * time.Millisecond,
		detectCaps: func(protocol string) imgrender.Caps {
			f.detectedWith = append(f.detectedWith, protocol)
			return imgrender.Caps{Inline: imgrender.ProtoKitty, Viewer: imgrender.ProtoKitty, CellW: 10, CellH: 20}
		},
		runTUI: func(opts app.Options) error {
			f.got = &opts
			if f.during != nil {
				f.during()
			}
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
	if o.Watcher == nil {
		t.Fatal("no vault watcher")
	}
	// The watcher is closed once the TUI exits.
	if _, open := <-o.Watcher.Events(); open {
		t.Error("watcher still running after the TUI exited")
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
	if got := f.stderr.String(); !strings.Contains(got, `unknown theme "no-such-theme"`) {
		t.Errorf("stderr = %q, want an unknown-theme warning", got)
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

func TestVaultLock(t *testing.T) {
	f := newFixture(t)
	f.makeRepo(t)
	f.writeConfig(t, "")
	lockPath := filepath.Join(f.vaultDir, ".notty", "lock")

	var secondCode int
	var secondErr string
	f.during = func() {
		if _, err := os.Stat(lockPath); err != nil {
			t.Errorf("lock not held while the TUI runs: %v", err)
		}
		g := newFixture(t)
		g.configPath = f.configPath
		secondCode = run([]string{"--vault", f.vaultDir}, g.env())
		secondErr = g.stderr.String()
		if g.got != nil {
			t.Error("second instance started its TUI")
		}
	}
	if code := run([]string{"--vault", f.vaultDir}, f.env()); code != 0 {
		t.Fatalf("first run exit code %d, stderr %q", code, f.stderr.String())
	}
	if secondCode != 1 {
		t.Errorf("second run exit code %d, want 1", secondCode)
	}
	for _, want := range []string{"waiting for vault lock", "pid " + strconv.Itoa(os.Getpid())} {
		if !strings.Contains(secondErr, want) {
			t.Errorf("second run stderr %q missing %q", secondErr, want)
		}
	}
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Errorf("lock not released after the TUI exits: %v", err)
	}
}

func TestWizardTakesNoLock(t *testing.T) {
	f := newFixture(t)
	f.makeRepo(t) // repo but no config file: wizard
	if code := run([]string{"--vault", f.vaultDir}, f.env()); code != 0 {
		t.Fatalf("exit code %d", code)
	}
	if !f.got.WizardNeeded {
		t.Fatal("expected wizard mode")
	}
	if _, err := os.Stat(filepath.Join(f.vaultDir, ".notty", "lock")); !os.IsNotExist(err) {
		t.Errorf("wizard mode created a lock: %v", err)
	}
}

func TestImageCapsDetected(t *testing.T) {
	for _, wizard := range []bool{false, true} {
		f := newFixture(t)
		f.makeRepo(t)
		if !wizard {
			f.writeConfig(t, "[images]\nprotocol = \"halfblocks\"\n")
		}
		if code := run([]string{"--vault", f.vaultDir}, f.env()); code != 0 {
			t.Fatalf("exit code %d, stderr %q", code, f.stderr.String())
		}
		want := "halfblocks"
		if wizard {
			want = "auto"
		}
		if !reflect.DeepEqual(f.detectedWith, []string{want}) {
			t.Errorf("wizard=%v: detection ran with %v, want [%s]", wizard, f.detectedWith, want)
		}
		if f.got.Caps.Inline != imgrender.ProtoKitty || f.got.Caps.CellW != 10 {
			t.Errorf("wizard=%v: Options.Caps = %+v", wizard, f.got.Caps)
		}
	}
}

func TestColorProfileFor(t *testing.T) {
	tests := []struct {
		inline imgrender.Protocol
		force  bool
	}{
		{imgrender.ProtoKitty, true},
		{imgrender.ProtoHalfBlocks, false},
		{imgrender.ProtoOff, false},
	}
	for _, tt := range tests {
		p, force := colorProfileFor(imgrender.Caps{Inline: tt.inline})
		if force != tt.force || (force && p != colorprofile.TrueColor) {
			t.Errorf("colorProfileFor(%v) = (%v, %v), want force=%v TrueColor", tt.inline, p, force, tt.force)
		}
	}
}
