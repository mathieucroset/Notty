package finder

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// In pick mode enter hands the chosen path back instead of opening it, and
// the excluded note (the open one) is never offered.
func TestPickEmitsPickedMsg(t *testing.T) {
	tests := []struct {
		name     string
		query    string
		recents  []string
		exclude  string
		wantPath string // "" means enter emits nothing
	}{
		{"query", "readme", nil, "Work/ideas.md", "readme.md"},
		{"recents", "", []string{"Work/ideas.md", "readme.md"}, "Work/ideas.md", "readme.md"},
		{"excluded note unmatched", "ideas", nil, "Work/ideas.md", ""},
		{"no exclusion", "ideas", nil, "", "Work/ideas.md"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := New(Fuzzy, testNotes(), tt.recents, testStyles(t), testPalette(t)).WithPick("Move to note", tt.exclude)
			m, _ = m.SetSize(100, 40).Init()
			m = typeText(m, tt.query)
			_, msg := send(m, "enter")
			if tt.wantPath == "" {
				if msg != nil {
					t.Fatalf("enter produced %#v, want nothing", msg)
				}
				return
			}
			p, ok := msg.(PickedMsg)
			if !ok {
				t.Fatalf("enter produced %#v, want finder.PickedMsg", msg)
			}
			if p.Path != tt.wantPath {
				t.Errorf("PickedMsg.Path = %q, want %q", p.Path, tt.wantPath)
			}
		})
	}
}

func TestPickExcludesFromEveryList(t *testing.T) {
	recents := []string{"Work/ideas.md", "readme.md", "Personal/journal.md"}
	m := New(Fuzzy, testNotes(), recents, testStyles(t), testPalette(t)).WithPick("Move to note", "Work/ideas.md")
	m = m.SetSize(100, 40)
	for _, q := range []string{"", "i", "md"} {
		m = typeText(m, q)
		for _, r := range m.fuzzy {
			if r.Path == "Work/ideas.md" {
				t.Errorf("query %q: excluded note offered", m.query)
			}
		}
	}
}

func TestPickEscCloses(t *testing.T) {
	m := New(Fuzzy, testNotes(), nil, testStyles(t), testPalette(t)).WithPick("Move to note", "")
	m = m.SetSize(100, 40)
	if _, msg := send(m, "esc"); msg != (CloseMsg{}) {
		t.Errorf("esc produced %#v, want finder.CloseMsg", msg)
	}
}

func TestPickTitle(t *testing.T) {
	m := New(Fuzzy, testNotes(), nil, testStyles(t), testPalette(t)).WithPick("Move to note", "")
	m = m.SetSize(100, 40)
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "Move to note") || strings.Contains(view, "Find note") {
		t.Errorf("view header does not say Move to note:\n%s", view)
	}
	if !strings.Contains(view, "enter choose · esc cancel") || strings.Contains(view, "enter open") {
		t.Errorf("view footer does not say enter chooses:\n%s", view)
	}
}
