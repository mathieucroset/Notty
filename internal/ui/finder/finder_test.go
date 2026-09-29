package finder

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/mathieucroset/notty/internal/index"
	"github.com/mathieucroset/notty/internal/ui/msgs"
	"github.com/mathieucroset/notty/internal/ui/theme"
)

func testStyles(t *testing.T) theme.Styles {
	t.Helper()
	p, ok := theme.Get("catppuccin-mocha")
	if !ok {
		t.Fatal("palette missing")
	}
	return theme.NewStyles(p)
}

func testPalette(t *testing.T) theme.Palette {
	t.Helper()
	p, ok := theme.Get("catppuccin-mocha")
	if !ok {
		t.Fatal("palette missing")
	}
	return p
}

func note(path, content string) *index.Note {
	return index.NewNote(path, content, time.Time{})
}

// testNotes builds a small fixture vault.
func testNotes() []*index.Note {
	return []*index.Note{
		note("Work/Standup notes.md", "# Standup notes\n\nShipped the auth flow today.\n"),
		note("Work/ideas.md", "# Ideas\n\nBrainstorm: dark mode toggle.\n"),
		note("Personal/journal.md", "# Journal\n\nWent for a run in the café this morning.\n"),
		note("readme.md", "# Readme\n\nWelcome to the vault.\n"),
	}
}

func key(s string) tea.KeyPressMsg {
	switch s {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "pgup":
		return tea.KeyPressMsg{Code: tea.KeyPgUp}
	case "pgdown":
		return tea.KeyPressMsg{Code: tea.KeyPgDown}
	case "backspace":
		return tea.KeyPressMsg{Code: tea.KeyBackspace}
	case "space":
		return tea.KeyPressMsg{Code: ' ', Text: " "}
	}
	if strings.HasPrefix(s, "ctrl+") && len(s) == 6 {
		r := rune(s[5])
		return tea.KeyPressMsg{Code: r, Mod: tea.ModCtrl}
	}
	return tea.KeyPressMsg{Code: rune(s[0]), Text: s}
}

// send applies keys in order, returning the final model and the last
// non-nil message produced (running any tea.Cmd synchronously).
func send(m Model, keys ...string) (Model, tea.Msg) {
	var last tea.Msg
	for _, k := range keys {
		var cmd tea.Cmd
		m, cmd = m.Update(key(k))
		if cmd != nil {
			if msg := cmd(); msg != nil {
				last = msg
			}
		}
	}
	return m, last
}

func typeText(m Model, s string) Model {
	for _, r := range s {
		var cmd tea.Cmd
		m, cmd = m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
		if cmd != nil {
			cmd() // drain synchronous fuzzy recompute / drop debounce ticks
		}
	}
	return m
}

func TestFuzzyTypingFiltersResults(t *testing.T) {
	m := New(Fuzzy, testNotes(), nil, testStyles(t), testPalette(t))
	m = m.SetSize(100, 40)

	m = typeText(m, "standup")
	if len(m.fuzzy) != 1 || m.fuzzy[0].Path != "Work/Standup notes.md" {
		t.Fatalf("query %q: fuzzy results = %+v, want exactly Work/Standup notes.md", "standup", m.fuzzy)
	}
}

func TestFuzzyEnterEmitsOpenNoteWithLineMinus1(t *testing.T) {
	m := New(Fuzzy, testNotes(), nil, testStyles(t), testPalette(t))
	m = m.SetSize(100, 40)
	m = typeText(m, "readme")

	_, msg := send(m, "enter")
	on, ok := msg.(msgs.OpenNoteMsg)
	if !ok {
		t.Fatalf("enter produced %#v, want msgs.OpenNoteMsg", msg)
	}
	if on.Path != "readme.md" || on.Line != -1 {
		t.Errorf("OpenNoteMsg = %+v, want {Path: readme.md, Line: -1}", on)
	}
}

func TestFuzzyEmptyQueryShowsRecents(t *testing.T) {
	recents := []string{"Personal/journal.md", "readme.md"}
	m := New(Fuzzy, testNotes(), recents, testStyles(t), testPalette(t))
	m = m.SetSize(100, 40)

	if len(m.fuzzy) != 2 {
		t.Fatalf("empty query: fuzzy results = %+v, want the 2 recents", m.fuzzy)
	}
	if m.fuzzy[0].Path != "Personal/journal.md" || m.fuzzy[1].Path != "readme.md" {
		t.Errorf("empty query: fuzzy results = %+v, want recents order", m.fuzzy)
	}
	if got := m.fuzzyStatus(); got != "Recent" {
		t.Errorf("fuzzyStatus() = %q, want %q", got, "Recent")
	}
}

func TestEscEmitsCloseMsg(t *testing.T) {
	for _, mode := range []Mode{Fuzzy, FullText} {
		m := New(mode, testNotes(), nil, testStyles(t), testPalette(t))
		m = m.SetSize(100, 40)
		_, msg := send(m, "esc")
		if _, ok := msg.(CloseMsg); !ok {
			t.Errorf("mode %v: esc produced %#v, want finder.CloseMsg", mode, msg)
		}
	}
}

func TestNavigationCtrlJK(t *testing.T) {
	recents := []string{"Work/Standup notes.md", "Work/ideas.md", "Personal/journal.md", "readme.md"}
	m := New(Fuzzy, testNotes(), recents, testStyles(t), testPalette(t))
	m = m.SetSize(100, 40)

	if m.cursor != 0 {
		t.Fatalf("initial cursor = %d, want 0", m.cursor)
	}
	m, _ = send(m, "ctrl+j")
	if m.cursor != 1 {
		t.Errorf("after ctrl+j, cursor = %d, want 1", m.cursor)
	}
	m, _ = send(m, "down")
	if m.cursor != 2 {
		t.Errorf("after down, cursor = %d, want 2", m.cursor)
	}
	m, _ = send(m, "ctrl+k")
	if m.cursor != 1 {
		t.Errorf("after ctrl+k, cursor = %d, want 1", m.cursor)
	}
	m, _ = send(m, "up")
	if m.cursor != 0 {
		t.Errorf("after up, cursor = %d, want 0", m.cursor)
	}
	// Cursor should not move past the top.
	m, _ = send(m, "up")
	if m.cursor != 0 {
		t.Errorf("cursor moved above the first result: %d", m.cursor)
	}
}

func TestViewWidthAndHeightBounds(t *testing.T) {
	cases := []struct{ termW, termH int }{
		{100, 40}, {200, 60}, {80, 24}, {45, 12},
	}
	for _, tc := range cases {
		for _, mode := range []Mode{Fuzzy, FullText} {
			m := New(mode, testNotes(), []string{"readme.md"}, testStyles(t), testPalette(t))
			m = m.SetSize(tc.termW, tc.termH)
			out := m.View()
			lines := strings.Split(out, "\n")
			if len(lines) != m.height {
				t.Fatalf("term %dx%d mode %v: View() has %d lines, want %d", tc.termW, tc.termH, mode, len(lines), m.height)
			}
			for i, l := range lines {
				if w := ansi.StringWidth(l); w != m.width {
					t.Errorf("term %dx%d mode %v: line %d width = %d, want %d (line: %q)", tc.termW, tc.termH, mode, i, w, m.width, l)
				}
			}
		}
	}
}

func TestSetSizeBounds(t *testing.T) {
	m := New(Fuzzy, nil, nil, testStyles(t), testPalette(t))
	m = m.SetSize(100, 50)
	if m.width <= 0 || m.width > 100 {
		t.Errorf("width = %d, want in (0, 100]", m.width)
	}
	if m.height <= 0 || m.height > 50 {
		t.Errorf("height = %d, want in (0, 50]", m.height)
	}
	wantW, wantH := 80, 35
	if m.width != wantW {
		t.Errorf("width = %d, want %d (80%% of 100)", m.width, wantW)
	}
	if m.height != wantH {
		t.Errorf("height = %d, want %d (70%% of 50)", m.height, wantH)
	}
}
