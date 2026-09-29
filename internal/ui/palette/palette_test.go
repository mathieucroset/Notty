package palette

import (
	"fmt"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

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

func key(s string) tea.KeyPressMsg {
	switch s {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "backspace":
		return tea.KeyPressMsg{Code: tea.KeyBackspace}
	}
	return tea.KeyPressMsg{Code: rune(s[0]), Text: s}
}

// collectMsgs runs cmd (and recursively flattens tea.Batch results) into the
// list of messages it produces.
func collectMsgs(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if msg == nil {
		return nil
	}
	if batch, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range batch {
			out = append(out, collectMsgs(c)...)
		}
		return out
	}
	return []tea.Msg{msg}
}

func hasMsgType(list []tea.Msg, want any) bool {
	for _, m := range list {
		if fmt.Sprintf("%T", m) == fmt.Sprintf("%T", want) {
			return true
		}
	}
	return false
}

func newTestPalette(t *testing.T) Model {
	t.Helper()
	m := New(DefaultCommands(), testStyles(t))
	return m.SetSize(100, 40)
}

func TestFilterNarrowsMatches(t *testing.T) {
	m := newTestPalette(t)
	m.input = "quit"
	m.refilter()

	if len(m.matches) == 0 {
		t.Fatal("expected at least one match for \"quit\"")
	}
	if m.matches[0].cmd.ID != "quit" {
		t.Errorf("top match = %q, want %q", m.matches[0].cmd.ID, "quit")
	}
}

func TestEmptyQueryListsEveryCommandInOrder(t *testing.T) {
	m := newTestPalette(t)
	cmds := DefaultCommands()
	if len(m.matches) != len(cmds) {
		t.Fatalf("got %d matches, want %d", len(m.matches), len(cmds))
	}
	for i, c := range cmds {
		if m.matches[i].cmd.ID != c.ID {
			t.Errorf("match[%d] = %q, want %q", i, m.matches[i].cmd.ID, c.ID)
		}
	}
}

func TestEnterRunsCommandAndCloses(t *testing.T) {
	m := newTestPalette(t)
	m.input = "quit"
	m.refilter()

	_, cmd := m.Update(key("enter"))
	got := collectMsgs(cmd)

	if !hasMsgType(got, msgs.QuitMsg{}) {
		t.Errorf("expected QuitMsg among %v", got)
	}
	if !hasMsgType(got, CloseMsg{}) {
		t.Errorf("expected CloseMsg among %v", got)
	}
}

func TestEscClosesPalette(t *testing.T) {
	m := newTestPalette(t)
	_, cmd := m.Update(key("esc"))
	got := collectMsgs(cmd)
	if !hasMsgType(got, CloseMsg{}) {
		t.Errorf("expected CloseMsg among %v", got)
	}
}

func TestTypingFiltersByRune(t *testing.T) {
	m := newTestPalette(t)
	for _, r := range "trash" {
		m, _ = m.Update(key(string(r)))
	}
	if len(m.matches) == 0 {
		t.Fatal("expected matches for \"trash\"")
	}
	if m.matches[0].cmd.ID != "trash" {
		t.Errorf("top match = %q, want %q", m.matches[0].cmd.ID, "trash")
	}

	m, _ = m.Update(key("backspace"))
	if m.input != "tras" {
		t.Errorf("input after backspace = %q, want %q", m.input, "tras")
	}
}

func TestCursorMoveIsBounded(t *testing.T) {
	m := newTestPalette(t)
	for i := 0; i < len(m.matches)+5; i++ {
		m, _ = m.Update(key("down"))
	}
	if m.cursor != len(m.matches)-1 {
		t.Errorf("cursor = %d, want %d (clamped at the end)", m.cursor, len(m.matches)-1)
	}
	for i := 0; i < len(m.matches)+5; i++ {
		m, _ = m.Update(key("up"))
	}
	if m.cursor != 0 {
		t.Errorf("cursor = %d, want 0 (clamped at the start)", m.cursor)
	}
}

func TestSetSizeClampsWidthAndHeight(t *testing.T) {
	m := New(DefaultCommands(), testStyles(t))

	m = m.SetSize(200, 100)
	if m.width != boxWidth {
		t.Errorf("width = %d, want %d for a wide terminal", m.width, boxWidth)
	}
	if m.height > 100*maxHeightPct/100 {
		t.Errorf("height = %d exceeds %d%% of the terminal", m.height, maxHeightPct)
	}

	m = m.SetSize(30, 10)
	if m.width > 30 {
		t.Errorf("width = %d exceeds the terminal width of 30", m.width)
	}
	if m.height > 10 {
		t.Errorf("height = %d exceeds the terminal height of 10", m.height)
	}
}

func TestViewFitsWithinSize(t *testing.T) {
	m := newTestPalette(t)
	view := m.View()

	lines := splitLines(view)
	for _, l := range lines {
		if w := ansi.StringWidth(l); w > m.width {
			t.Errorf("line width %d exceeds box width %d: %q", w, m.width, l)
		}
	}
	if len(lines) > m.height {
		t.Errorf("view has %d lines, exceeds box height %d", len(lines), m.height)
	}
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i, r := range s {
		if r == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	lines = append(lines, s[start:])
	return lines
}
