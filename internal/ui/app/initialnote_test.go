package app

import "testing"

// notty new starts the app on the note it just created (spec §11), in
// place of the note open when the app last quit.
func TestInitialNoteOpenedOnStart(t *testing.T) {
	opts := testOptions(t)
	opts.Local.LastNote = "ideas.md"
	opts.InitialNote = "Work/Standup notes.md"
	m := start(t, opts, 120, 30)
	if m.NotePath() != "Work/Standup notes.md" {
		t.Errorf("NotePath = %q, want the initial note", m.NotePath())
	}
}
