package finder

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/mathieucroset/notty/internal/index"
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

// TestEnterCancelsInFlightSearch checks that choosing a result (enter)
// cancels any in-flight full-text search context, the same as esc does, so
// an abandoned search never keeps running after the overlay session ends.
func TestEnterCancelsInFlightSearch(t *testing.T) {
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
	if m.cancel == nil {
		t.Fatal("expected an in-flight search context after a completed search (handleDebounce always sets one)")
	}

	cancelled := false
	m.cancel = func() { cancelled = true }

	m, _ = send(m, "enter")
	if !cancelled {
		t.Error("enter did not cancel the in-flight search context")
	}
}

// TestFullTextHitRowsShowContext checks spec §8: each full-text hit shows
// one line of context (hit.Context, dimmed) in addition to its location and
// matching text, so each item occupies 3 rows.
func TestFullTextHitRowsShowContext(t *testing.T) {
	if got := (Model{mode: FullText}).rowHeight(); got != 3 {
		t.Fatalf("rowHeight() for FullText = %d, want 3", got)
	}
	if got := (Model{mode: Fuzzy}).rowHeight(); got != 2 {
		t.Fatalf("rowHeight() for Fuzzy = %d, want 2", got)
	}

	n := note("notes.md", "# Notes\n\nFirst context line\nShipped the auth flow today.\nAfter line\n")
	m := New(FullText, []*index.Note{n}, nil, testStyles(t), testPalette(t))
	m = m.SetSize(100, 40)

	m, cmd := m.Update(tea.KeyPressMsg{Code: 'a', Text: "auth"})
	dm := cmd().(debounceMsg)
	m, searchCmd := m.Update(dm)
	res := searchCmd().(ftResultMsg)
	m, _ = m.Update(res)

	if len(m.hits) != 1 {
		t.Fatalf("hits = %+v, want exactly 1", m.hits)
	}
	if m.hits[0].Context != "After line" {
		t.Fatalf("hit.Context = %q, want %q", m.hits[0].Context, "After line")
	}

	rows := m.hitItemLines(0, 40, false)
	if len(rows) != 3 {
		t.Fatalf("hitItemLines returned %d rows, want 3 (loc, text, context)", len(rows))
	}
	loc, text, context := ansi.Strip(rows[0]), ansi.Strip(rows[1]), ansi.Strip(rows[2])
	if !strings.Contains(loc, "notes.md:4") {
		t.Errorf("row 0 (location) = %q, want it to contain %q", loc, "notes.md:4")
	}
	if !strings.Contains(text, "Shipped the auth flow today.") {
		t.Errorf("row 1 (text) = %q, want it to contain the matching line", text)
	}
	if !strings.Contains(context, "After line") {
		t.Errorf("row 2 (context) = %q, want it to contain the context line %q", context, "After line")
	}
}

// TestListPaneGroupsFullTextHitsInThrees checks that the list pane lays out
// full-text results 3 lines per item (not 2, as for fuzzy).
func TestListPaneGroupsFullTextHitsInThrees(t *testing.T) {
	n := note("notes.md", "alpha match\nbeta match\ngamma match\ndelta match\n")
	m := New(FullText, []*index.Note{n}, nil, testStyles(t), testPalette(t))
	m = m.SetSize(100, 40)

	m, cmd := m.Update(tea.KeyPressMsg{Code: 'm', Text: "match"})
	dm := cmd().(debounceMsg)
	m, searchCmd := m.Update(dm)
	res := searchCmd().(ftResultMsg)
	m, _ = m.Update(res)
	if len(m.hits) < 2 {
		t.Fatalf("expected at least 2 hits, got %d", len(m.hits))
	}

	lines := m.listPane(40, m.bodyHeight())
	// The second item's location line must be exactly rowHeight (3) lines
	// after the first item's.
	firstLoc := ansi.Strip(lines[0])
	secondLoc := ansi.Strip(lines[3])
	if !strings.Contains(firstLoc, ":1") {
		t.Errorf("line 0 = %q, want the first hit's location (line 1)", firstLoc)
	}
	if !strings.Contains(secondLoc, ":2") {
		t.Errorf("line 3 = %q, want the second hit's location (line 2), i.e. rows are grouped in 3s", secondLoc)
	}
}
