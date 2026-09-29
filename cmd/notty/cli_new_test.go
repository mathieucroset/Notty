package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParseInterspersed(t *testing.T) {
	tests := []struct {
		args       []string
		wantPos    []string
		wantFolder string
	}{
		{[]string{"Title"}, []string{"Title"}, ""},
		{[]string{"Title", "--folder", "Work"}, []string{"Title"}, "Work"},
		{[]string{"--folder=Work", "Title"}, []string{"Title"}, "Work"},
		{[]string{"a", "--folder", "W", "b"}, []string{"a", "b"}, "W"},
		{[]string{"--", "--folder"}, []string{"--folder"}, ""},
		{nil, nil, ""},
	}
	for _, tt := range tests {
		flags, _ := subcommandFlags("new", "", newFixture(t).env())
		folder := flags.String("folder", "", "")
		pos, err := parseInterspersed(flags, tt.args)
		if err != nil {
			t.Fatalf("%v: %v", tt.args, err)
		}
		if !reflect.DeepEqual(pos, tt.wantPos) || *folder != tt.wantFolder {
			t.Errorf("%v: pos %q folder %q, want %q %q", tt.args, pos, *folder, tt.wantPos, tt.wantFolder)
		}
	}
}

func TestNewCreatesNoteAndOpensIt(t *testing.T) {
	tests := []struct {
		name     string
		args     func(vault string) []string
		wantPath string
	}{
		{"configured vault", func(string) []string { return []string{"new", "Standup notes"} }, "Standup notes.md"},
		{"global vault flag and folder", func(v string) []string {
			return []string{"--vault", v, "new", "Standup notes", "--folder", "Work"}
		}, "Work/Standup notes.md"},
		{"flags after the subcommand", func(v string) []string {
			return []string{"new", "--folder", "Work/Sub", "--vault", v, "Plan"}
		}, "Work/Sub/Plan.md"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			f.makeRepo(t)
			f.writeConfig(t, "vault = \""+filepath.ToSlash(f.vaultDir)+"\"\n")
			lockPath := filepath.Join(f.vaultDir, ".notty", "lock")
			f.during = func() {
				if _, err := os.Stat(lockPath); err != nil {
					t.Errorf("lock not held while the TUI runs: %v", err)
				}
			}
			if code := run(tt.args(f.vaultDir), f.env()); code != 0 {
				t.Fatalf("exit code %d, stderr %q", code, f.stderr.String())
			}
			if f.got == nil {
				t.Fatal("TUI not started")
			}
			if f.got.InitialNote != tt.wantPath {
				t.Errorf("InitialNote = %q, want %q", f.got.InitialNote, tt.wantPath)
			}
			b, err := os.ReadFile(filepath.Join(f.vaultDir, filepath.FromSlash(tt.wantPath)))
			if err != nil {
				t.Fatalf("note not created: %v", err)
			}
			title := strings.TrimSuffix(filepath.Base(tt.wantPath), ".md")
			if string(b) != "# "+title+"\n\n" {
				t.Errorf("note content = %q", b)
			}
			if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
				t.Errorf("lock not released after the TUI exits: %v", err)
			}
		})
	}
}

func TestNewUsageErrors(t *testing.T) {
	for _, args := range [][]string{{"new"}, {"new", "a", "b"}, {"new", ""}, {"new", "--nope", "a"}, {"bogus"}} {
		f := newFixture(t)
		f.makeRepo(t)
		f.writeConfig(t, "")
		full := append([]string{"--vault", f.vaultDir}, args...)
		if code := run(full, f.env()); code != 2 {
			t.Errorf("run(%v) = %d, want 2", args, code)
		}
		if f.got != nil {
			t.Errorf("run(%v) started the TUI", args)
		}
		if !strings.Contains(f.stderr.String(), "usage:") {
			t.Errorf("run(%v): stderr %q has no usage", args, f.stderr.String())
		}
	}
}

func TestNewNeedsSetUpVault(t *testing.T) {
	tests := []struct {
		name   string
		config bool
		repo   bool
	}{
		{"no config file", false, true},
		{"vault not a repo", true, false},
		{"vault missing", true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			if tt.repo {
				f.makeRepo(t)
			} else if tt.name == "vault not a repo" {
				if err := os.MkdirAll(f.vaultDir, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			if tt.config {
				f.writeConfig(t, "")
			}
			if code := run([]string{"--vault", f.vaultDir, "new", "Idea"}, f.env()); code != 1 {
				t.Fatalf("exit code %d, want 1", code)
			}
			if f.got != nil {
				t.Error("TUI started")
			}
			if !strings.Contains(f.stderr.String(), "run notty first to set up your vault") {
				t.Errorf("stderr = %q", f.stderr.String())
			}
			if _, err := os.Stat(filepath.Join(f.vaultDir, "Idea.md")); !os.IsNotExist(err) {
				t.Errorf("note created: %v", err)
			}
			if tt.name == "vault missing" {
				if _, err := os.Stat(f.vaultDir); !os.IsNotExist(err) {
					t.Error("vault directory created")
				}
			}
		})
	}
}
