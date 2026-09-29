package finder

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/mathieucroset/notty/internal/ui/msgs"
)

func TestFullTextDebounceProducesResults(t *testing.T) {
	m := New(FullText, testNotes(), nil, testStyles(t), testPalette(t))
	m = m.SetSize(100, 40)

	// Typing schedules a debounce tick (a tea.Cmd) rather than searching
	// immediately.
	m2, cmd := m.Update(tea.KeyPressMsg{Code: 's', Text: "s"})
	if cmd == nil {
		t.Fatal("typing did not return a command (expected a debounce tick)")
	}
	if !m2.searching {
		t.Error("searching should be true immediately after a keystroke")
	}
	if len(m2.hits) != 0 {
		t.Errorf("hits should be empty before the debounce fires, got %v", m2.hits)
	}

	// Firing the tick's command runs the debounce and returns a debounceMsg.
	msg := cmd()
	dm, ok := msg.(debounceMsg)
	if !ok {
		t.Fatalf("debounce tick produced %#v, want debounceMsg", msg)
	}

	// Feeding the debounceMsg back in kicks off the actual search command.
	m3, searchCmd := m2.Update(dm)
	if searchCmd == nil {
		t.Fatal("debounceMsg did not return a search command")
	}
	resMsg := searchCmd()
	rm, ok := resMsg.(ftResultMsg)
	if !ok {
		t.Fatalf("search command produced %#v, want ftResultMsg", resMsg)
	}

	m4, _ := m3.Update(rm)
	if m4.searching {
		t.Error("searching should be false once results land")
	}
	if len(m4.hits) == 0 {
		t.Fatalf("query %q: expected hits, got none", "s")
	}
}

func TestFullTextStaleResultDiscarded(t *testing.T) {
	m := New(FullText, testNotes(), nil, testStyles(t), testPalette(t))
	m = m.SetSize(100, 40)

	// First keystroke: schedule and fire its debounce, obtaining an
	// in-flight search command at seq 1.
	m, cmd := m.Update(tea.KeyPressMsg{Code: 's', Text: "s"})
	dm := cmd().(debounceMsg)
	m, searchCmd := m.Update(dm)
	if searchCmd == nil {
		t.Fatal("expected a search command after the first debounce")
	}

	// Second keystroke supersedes it before the first search's result
	// arrives.
	m, cmd2 := m.Update(tea.KeyPressMsg{Code: 't', Text: "t"})
	dm2 := cmd2().(debounceMsg)
	m, searchCmd2 := m.Update(dm2)
	if searchCmd2 == nil {
		t.Fatal("expected a search command after the second debounce")
	}
	res2 := searchCmd2().(ftResultMsg)
	m, _ = m.Update(res2)
	if m.query != "st" {
		t.Fatalf("query = %q, want %q", m.query, "st")
	}
	secondHits := m.hits

	// The stale first search's result now arrives; it must be ignored.
	res1 := searchCmd().(ftResultMsg)
	m, _ = m.Update(res1)
	if m.searching {
		t.Error("a stale result flipped searching back on")
	}
	if len(m.hits) != len(secondHits) {
		t.Errorf("a stale result overwrote the current hits: got %v, want %v", m.hits, secondHits)
	}
}

func TestFullTextDebounceTickItselfCanBeStale(t *testing.T) {
	m := New(FullText, testNotes(), nil, testStyles(t), testPalette(t))
	m = m.SetSize(100, 40)

	m, cmd := m.Update(tea.KeyPressMsg{Code: 's', Text: "s"})
	dm := cmd().(debounceMsg)

	// A second keystroke arrives before the first tick is delivered.
	m, cmd2 := m.Update(tea.KeyPressMsg{Code: 't', Text: "t"})

	// The first (now-stale) debounce tick must be a no-op.
	m3, staleCmd := m.Update(dm)
	if staleCmd != nil {
		t.Error("a stale debounce tick scheduled a search command")
	}
	if m3.query != "st" {
		t.Fatalf("query = %q, want %q", m3.query, "st")
	}

	// The second tick still works normally.
	dm2 := cmd2().(debounceMsg)
	_, searchCmd := m3.Update(dm2)
	if searchCmd == nil {
		t.Error("the current debounce tick did not schedule a search")
	}
}

func TestFullTextEnterEmitsOpenNoteWithHitLine(t *testing.T) {
	m := New(FullText, testNotes(), nil, testStyles(t), testPalette(t))
	m = m.SetSize(100, 40)

	m, cmd := m.Update(tea.KeyPressMsg{Code: 'a', Text: "auth"})
	dm := cmd().(debounceMsg)
	m, searchCmd := m.Update(dm)
	res := searchCmd().(ftResultMsg)
	m, _ = m.Update(res)

	if len(m.hits) == 0 {
		t.Fatal("expected at least one hit for \"auth\"")
	}
	want := m.hits[0]

	_, msg := send(m, "enter")
	on, ok := msg.(msgs.OpenNoteMsg)
	if !ok {
		t.Fatalf("enter produced %#v, want msgs.OpenNoteMsg", msg)
	}
	if on.Path != want.Path || on.Line != want.Line {
		t.Errorf("OpenNoteMsg = %+v, want {Path: %q, Line: %d}", on, want.Path, want.Line)
	}
}

func TestFullTextEmptyQueryClearsHitsWithoutDebounce(t *testing.T) {
	m := New(FullText, testNotes(), nil, testStyles(t), testPalette(t))
	m = m.SetSize(100, 40)

	m, cmd := m.Update(tea.KeyPressMsg{Code: 'a', Text: "auth"})
	dm := cmd().(debounceMsg)
	m, searchCmd := m.Update(dm)
	res := searchCmd().(ftResultMsg)
	m, _ = m.Update(res)
	if len(m.hits) == 0 {
		t.Fatal("expected hits for \"auth\"")
	}

	m, backspaceCmd := m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	m, backspaceCmd = m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	m, backspaceCmd = m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	m, backspaceCmd = m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	if backspaceCmd != nil {
		t.Error("clearing the query to empty should not schedule a debounce")
	}
	if len(m.hits) != 0 {
		t.Errorf("hits = %v, want none once the query is empty", m.hits)
	}
	if m.searching {
		t.Error("searching should be false for an empty query")
	}
}
