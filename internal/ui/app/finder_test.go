package app

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/mathieucroset/notty/internal/buffer"
)

func typeQuery(t *testing.T, m *Model, q string) {
	t.Helper()
	for _, r := range q {
		run(t, m, keyMsg(string(r)))
	}
}

func TestFuzzyFinderOpensNote(t *testing.T) {
	m := start(t, testOptions(t), 120, 30)
	run(t, m, keyMsg("ctrl+p"))
	o := m.topOverlay()
	if o == nil || o.kind != overlayFinder {
		t.Fatal("ctrl+p did not open the finder")
	}
	assertSize(t, m, 120, 30)
	typeQuery(t, m, "stand")
	if !strings.Contains(screen(m), "Standup") {
		t.Fatalf("no match shown:\n%s", screen(m))
	}
	run(t, m, keyMsg("enter"))
	if m.overlayOpen() {
		t.Error("finder still open after choosing")
	}
	if m.NotePath() != "Work/Standup notes.md" || m.Focus() != FocusMain {
		t.Errorf("open note %q focus %v", m.NotePath(), m.Focus())
	}
}

func TestFullTextSearchOpensAtLine(t *testing.T) {
	m := openNote(t, testOptions(t), "ideas.md")
	run(t, m, keyMsg("ctrl+f"))
	if o := m.topOverlay(); o == nil || o.kind != overlayFinder {
		t.Fatal("ctrl+f did not open search")
	}
	typeQuery(t, m, "follow")
	if !strings.Contains(screen(m), "follow up with design") {
		t.Fatalf("no search hit shown:\n%s", screen(m))
	}
	run(t, m, keyMsg("enter"))
	if m.NotePath() != "Work/Standup notes.md" {
		t.Fatalf("open note = %q", m.NotePath())
	}
	if got := m.editor.Cursor(); got != (buffer.Pos{Line: 3}) {
		t.Errorf("cursor = %+v, want line 3", got)
	}
	if m.overlayOpen() {
		t.Error("search still open")
	}
}

func TestFinderSearchesUnsavedBuffer(t *testing.T) {
	m := openNote(t, testOptions(t), "ideas.md")
	insertText(t, m, "zebra ")
	run(t, m, keyMsg("ctrl+f"))
	typeQuery(t, m, "zebra")
	if !strings.Contains(screen(m), "ideas.md") {
		t.Errorf("search misses text typed in the buffer:\n%s", screen(m))
	}
	// Choosing the open note keeps its unsaved buffer.
	run(t, m, keyMsg("enter"))
	if !strings.HasPrefix(m.editor.Content(), "zebra ") {
		t.Errorf("buffer = %q", m.editor.Content())
	}
}

func TestFinderEscCloses(t *testing.T) {
	m := openNote(t, testOptions(t), "ideas.md")
	run(t, m, keyMsg("ctrl+p"))
	if c := m.View().Cursor; c != nil {
		t.Errorf("editor cursor shown under the finder: %+v", c)
	}
	run(t, m, keyMsg("esc"))
	if m.overlayOpen() {
		t.Error("esc did not close the finder")
	}
	if m.View().Cursor == nil {
		t.Error("editor cursor not back after closing the finder")
	}
}

func TestFinderResizes(t *testing.T) {
	m := start(t, testOptions(t), 120, 30)
	run(t, m, keyMsg("ctrl+p"))
	run(t, m, keyMsg("ctrl+g")) // global keys stay in the overlay
	run(t, m, tea.WindowSizeMsg{Width: 90, Height: 24})
	assertSize(t, m, 90, 24)
}
