package app

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/exp/teatest/v2"

	"github.com/mathieucroset/notty/internal/meta"
	"github.com/mathieucroset/notty/internal/ui/msgs"
	"github.com/mathieucroset/notty/internal/vault"
)

// writeFile writes content at the vault-relative path rel.
func writeFile(t *testing.T, v *vault.Vault, rel, content string) {
	t.Helper()
	abs := filepath.Join(v.Root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// taggedOptions is testOptions with a #work tag on the standup note.
func taggedOptions(t *testing.T) Options {
	t.Helper()
	opts := testOptions(t)
	writeFile(t, opts.Vault, "Work/Standup notes.md", "# Standup notes #work\n\n- shipped auth flow\n")
	writeFile(t, opts.Vault, "Work/Retro.md", "# Retro\n\nnothing tagged\n")
	return opts
}

func TestIndexBuiltAtStartup(t *testing.T) {
	m := New(taggedOptions(t))
	run(t, m, tea.WindowSizeMsg{Width: 120, Height: 30})
	cmd := m.Init()
	if !strings.Contains(screen(m), "indexing…") {
		t.Errorf("status bar does not show indexing:\n%s", screen(m))
	}
	for _, msg := range execCmd(cmd) {
		run(t, m, msg)
	}
	s := screen(m)
	if strings.Contains(s, "indexing…") {
		t.Errorf("indexing still shown after the build:\n%s", s)
	}
	if !strings.Contains(s, "TAGS") || !strings.Contains(s, "#work") {
		t.Errorf("tag chips missing:\n%s", s)
	}
	if m.ix == nil || m.ix.Len() != 3 {
		t.Fatalf("index not installed: %v", m.ix)
	}
}

func TestIndexProblemsToast(t *testing.T) {
	opts := testOptions(t)
	writeFile(t, opts.Vault, "bad.md", "# Bad\n\xff\xfe\n")
	m := start(t, opts, 120, 30)
	s := screen(m)
	if !strings.Contains(s, "1 note has a problem") {
		t.Errorf("problem toast missing:\n%s", s)
	}
}

func TestTagFilter(t *testing.T) {
	opts := taggedOptions(t)
	opts.Local.Expanded = []string{"Work"}
	m := start(t, opts, 120, 30)
	run(t, m, msgs.FilterTagMsg{Tag: "work"})
	s := screen(m)
	if !strings.Contains(s, "Standup notes") {
		t.Errorf("tagged note hidden by the filter:\n%s", s)
	}
	for _, hidden := range []string{"Retro", "ideas"} {
		if strings.Contains(s, hidden) {
			t.Errorf("untagged %q shown under the filter:\n%s", hidden, s)
		}
	}
	run(t, m, msgs.ClearFilterMsg{})
	if s := screen(m); !strings.Contains(s, "Retro") || !strings.Contains(s, "ideas") {
		t.Errorf("clearing the filter did not restore the tree:\n%s", s)
	}
}

func TestPinTogglePersists(t *testing.T) {
	opts := testOptions(t)
	m := start(t, opts, 120, 30)
	run(t, m, msgs.TogglePinMsg{Path: "ideas.md"})
	if !strings.Contains(screen(m), "PINNED") {
		t.Errorf("PINNED missing after pinning:\n%s", screen(m))
	}
	saved, err := meta.Load(opts.Vault.Root)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(saved.Pins, []string{"ideas.md"}) {
		t.Errorf("saved pins = %v", saved.Pins)
	}
	run(t, m, msgs.TogglePinMsg{Path: "ideas.md"})
	if strings.Contains(screen(m), "PINNED") {
		t.Errorf("PINNED still shown after unpinning:\n%s", screen(m))
	}
	if saved, _ = meta.Load(opts.Vault.Root); len(saved.Pins) != 0 {
		t.Errorf("saved pins after unpin = %v", saved.Pins)
	}
}

// TestPinAndFilterFlow drives the sidebar keys in a real program: p pins
// the selected note, # opens the tag picker and enter filters by the tag.
func TestPinAndFilterFlow(t *testing.T) {
	opts := taggedOptions(t)
	tm := teatest.NewTestModel(t, New(opts), teatest.WithInitialTermSize(120, 30))
	waitScreen(t, tm, "#work")

	// Rows: Work, ideas.md. Pin ideas.
	tm.Send(keyMsg("j"))
	tm.Send(keyMsg("p"))
	waitScreen(t, tm, "PINNED")

	tm.Send(keyMsg("#"))
	for _, r := range "work" {
		tm.Send(keyMsg(string(r)))
	}
	tm.Send(keyMsg("enter"))
	waitScreen(t, tm, "filtered")

	tm.Send(keyMsg("ctrl+q"))
	tm.WaitFinished(t, teatest.WithFinalTimeout(5*time.Second))
	final := tm.FinalModel(t).(*Model)
	if final.filterTag != "work" {
		t.Errorf("filterTag = %q, want work", final.filterTag)
	}
	saved, err := meta.Load(opts.Vault.Root)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(saved.Pins, []string{"ideas.md"}) {
		t.Errorf("saved pins = %v", saved.Pins)
	}
}

// waitScreen waits until the program's output contains every want.
func waitScreen(t *testing.T, tm *teatest.TestModel, want ...string) {
	t.Helper()
	teatest.WaitFor(t, tm.Output(), func(b []byte) bool {
		s := ansi.Strip(string(b))
		for _, w := range want {
			if !strings.Contains(s, w) {
				return false
			}
		}
		return true
	}, teatest.WithDuration(5*time.Second))
}
