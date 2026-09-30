package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestGoBinDir(t *testing.T) {
	sep := string(os.PathListSeparator)
	home := filepath.Join("home", "me")
	tests := []struct {
		name               string
		gobin, gopath, hom string
		want               string
	}{
		{"GOBIN wins", filepath.Join("opt", "gobin"), filepath.Join("gp"), home, filepath.Join("opt", "gobin")},
		{"GOPATH", "", filepath.Join("gp"), home, filepath.Join("gp", "bin")},
		{"first GOPATH entry", "", filepath.Join("gp1") + sep + filepath.Join("gp2"), home, filepath.Join("gp1", "bin")},
		{"empty first GOPATH entry skipped", "", sep + filepath.Join("gp2"), home, filepath.Join("gp2", "bin")},
		{"default", "", "", home, filepath.Join(home, "go", "bin")},
		{"nothing known", "", "", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := goBinDir(tt.gobin, tt.gopath, tt.hom); got != tt.want {
				t.Errorf("goBinDir(%q, %q, %q) = %q, want %q", tt.gobin, tt.gopath, tt.hom, got, tt.want)
			}
		})
	}
}

func TestResolvePath(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	real := filepath.Join(dir, "real")
	if err := os.Mkdir(real, 0o755); err != nil {
		t.Fatal(err)
	}
	sep := string(filepath.Separator)
	missing := dir + sep + "missing" + sep + ".." + sep + "gone"
	if got, want := resolvePath(missing), filepath.Join(dir, "gone"); got != want {
		t.Errorf("resolvePath(missing) = %q, want the cleaned %q", got, want)
	}
	if got := resolvePath(real); got != real {
		t.Errorf("resolvePath(real) = %q, want %q", got, real)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(real, link); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("no symlinks: %v", err)
		}
		t.Fatal(err)
	}
	if got := resolvePath(link); got != real {
		t.Errorf("resolvePath(link) = %q, want %q", got, real)
	}
}

// TestPrepareFillsUpdateCheck checks that every TUI start, the first-run
// wizard included, gets the release check options.
func TestPrepareFillsUpdateCheck(t *testing.T) {
	tests := []struct {
		name         string
		repo         bool
		env          string
		wantDisabled bool
	}{
		{"normal start", true, "", false},
		{"wizard", false, "", false},
		{"disabled by env", true, "1", true},
		{"only 1 disables", true, "true", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("NOTTY_NO_UPDATE_CHECK", tt.env)
			gobin := filepath.Join(t.TempDir(), "gobin")
			t.Setenv("GOBIN", gobin)
			f := newFixture(t)
			if tt.repo {
				f.makeRepo(t)
				f.writeConfig(t, "vault = \""+filepath.ToSlash(f.vaultDir)+"\"\n")
			}
			if code := run(nil, f.env()); code != 0 {
				t.Fatalf("exit code %d, stderr %q", code, f.stderr.String())
			}
			o := f.got
			if o == nil {
				t.Fatal("TUI not started")
			}
			if o.WizardNeeded == tt.repo {
				t.Fatalf("WizardNeeded = %v", o.WizardNeeded)
			}
			if o.Version != version {
				t.Errorf("Version = %q, want %q", o.Version, version)
			}
			uc := o.UpdateCheck
			if uc.EnvDisabled != tt.wantDisabled {
				t.Errorf("EnvDisabled = %v, want %v", uc.EnvDisabled, tt.wantDisabled)
			}
			if want := filepath.Join(f.dir, "state", "update-check.json"); uc.StatePath != want {
				t.Errorf("StatePath = %q, want %q", uc.StatePath, want)
			}
			if uc.Exe == "" || !filepath.IsAbs(uc.Exe) {
				t.Errorf("Exe = %q, want an absolute path", uc.Exe)
			}
			if uc.GoBin != gobin {
				t.Errorf("GoBin = %q, want %q", uc.GoBin, gobin)
			}
			if uc.APIURL != "" || uc.Client != nil {
				t.Errorf("APIURL = %q, Client = %v; want the defaults", uc.APIURL, uc.Client)
			}
		})
	}
}
