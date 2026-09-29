package syncer

import (
	"testing"

	"github.com/mathieucroset/notty/internal/gitsync"
)

func TestCommitMessage(t *testing.T) {
	a := func(p string) gitsync.Change { return gitsync.Change{Status: 'A', Path: p} }
	m := func(p string) gitsync.Change { return gitsync.Change{Status: 'M', Path: p} }
	d := func(p string) gitsync.Change { return gitsync.Change{Status: 'D', Path: p} }
	r := func(from, to string) gitsync.Change { return gitsync.Change{Status: 'R', Path: to, OldPath: from} }

	tests := []struct {
		name    string
		changes []gitsync.Change
		want    string
	}{
		{"one update", []gitsync.Change{m("Work/Standup notes.md")}, "Update Standup notes.md · laptop"},
		{"two updates sorted by path", []gitsync.Change{m("z/ideas.md"), m("a/Standup notes.md")}, "Update Standup notes.md, ideas.md · laptop"},
		{"three names", []gitsync.Change{m("a.md"), m("b.md"), m("c.md")}, "Update a.md, b.md, c.md · laptop"},
		{"more than three", []gitsync.Change{m("a.md"), m("b.md"), m("c.md"), m("d.md"), m("e.md"), m("f.md"), m("g.md")}, "Update 7 notes · laptop"},
		{"create", []gitsync.Change{a("New note.md")}, "Create New note.md · laptop"},
		{"create many", []gitsync.Change{a("a.md"), a("b.md"), a("c.md"), a("d.md")}, "Create 4 notes · laptop"},
		{"permanent delete", []gitsync.Change{d(".trash/20260929-laptop-ab12/x.md"), d(".trash/20260929-laptop-ab12/meta.json")}, "Delete x.md · laptop"},
		{"plain delete", []gitsync.Change{d("x.md")}, "Delete x.md · laptop"},
		{"move", []gitsync.Change{r("Inbox/x.md", "Work/x.md")}, "Move x.md · laptop"},
		{"rename", []gitsync.Change{r("x.md", "y.md")}, "Move y.md · laptop"},
		{"trash is a delete", []gitsync.Change{r("Work/x.md", ".trash/20260929101500-laptop-ab12/x.md"), a(".trash/20260929101500-laptop-ab12/meta.json")}, "Delete x.md · laptop"},
		{"restore", []gitsync.Change{r(".trash/20260929101500-laptop-ab12/x.md", "Work/x.md"), d(".trash/20260929101500-laptop-ab12/meta.json")}, "Restore x.md · laptop"},
		{"mixed kinds", []gitsync.Change{a("new.md"), m("old.md")}, "Update new.md, old.md · laptop"},
		{"mixed many", []gitsync.Change{a("a.md"), m("b.md"), d("c.md"), m("d.md")}, "Update 4 notes · laptop"},
		{"pins", []gitsync.Change{m(".notty/state.json")}, "Update state.json · laptop"},
		{"only trash metadata", []gitsync.Change{m(".trash/id/meta.json")}, "Update meta.json · laptop"},
		{"empty", nil, "Update · laptop"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CommitMessage(tt.changes, "laptop"); got != tt.want {
				t.Errorf("CommitMessage() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCommitMessageHostParsesBack(t *testing.T) {
	msg := CommitMessage([]gitsync.Change{{Status: 'M', Path: "a b.md"}}, "desk-01")
	if got := gitsync.HostFromSubject(msg); got != "desk-01" {
		t.Fatalf("HostFromSubject(%q) = %q, want desk-01", msg, got)
	}
}
