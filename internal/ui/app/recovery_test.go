package app

import (
	"strings"
	"testing"

	"github.com/mathieucroset/notty/internal/recovery"
	"github.com/mathieucroset/notty/internal/ui/msgs"
)

// TestSnapshotFollowsTheBuffer checks that the recovery snapshot holds the
// open note's path, text and dirty state after each step.
func TestSnapshotFollowsTheBuffer(t *testing.T) {
	opts := testOptions(t)
	snap := &recovery.Snapshot{}
	opts.Snapshot = snap
	m := openNote(t, opts, "ideas.md")
	steps := []struct {
		name       string
		do         func()
		rel        string
		wantPrefix string
		dirty      bool
	}{
		{"opened", func() {}, "ideas.md", "# Ideas", false},
		{"typed", func() { insertText(t, m, "typed ") }, "ideas.md", "typed # Ideas", true},
		{"saved", func() { run(t, m, msgs.SaveRequestMsg{}) }, "ideas.md", "typed # Ideas", false},
		{"typed again", func() { insertText(t, m, "more ") }, "ideas.md", "typedmore  # Ideas", true},
		{"other note", func() { run(t, m, msgs.OpenNoteMsg{Path: "Work/Standup notes.md", Line: -1}) },
			"Work/Standup notes.md", "# Standup notes", false},
	}
	for _, s := range steps {
		s.do()
		root, rel, content, dirty := snap.Get()
		if root != opts.Vault.Root || rel != s.rel || !strings.HasPrefix(content, s.wantPrefix) || dirty != s.dirty {
			t.Errorf("%s: snapshot = %q %q %.30q dirty %v; want %q %q %q dirty %v",
				s.name, root, rel, content, dirty, opts.Vault.Root, s.rel, s.wantPrefix, s.dirty)
		}
	}
}

// Without a snapshot the app runs as before.
func TestNoSnapshot(t *testing.T) {
	m := openNote(t, testOptions(t), "ideas.md")
	insertText(t, m, "typed ")
	if !m.editor.Dirty() {
		t.Error("edit lost")
	}
}
