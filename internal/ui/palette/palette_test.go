package palette

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/sahilm/fuzzy"

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
	m := New(DefaultCommands(), testStyles(t)).WithCurrentTheme("catppuccin-mocha")
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

// findCommand moves the palette's cursor to the command with the given ID.
func findCommand(t *testing.T, m Model, id string) Model {
	t.Helper()
	for i, mt := range m.matches {
		if mt.cmd.ID == id {
			m.cursor = i
			return m
		}
	}
	t.Fatalf("command %q not found", id)
	return m
}

func TestSwitchThemeOpensPickerAndPreviewsOnMove(t *testing.T) {
	m := newTestPalette(t)
	m = findCommand(t, m, idSwitchTheme)

	m, cmd := m.Update(key("enter"))
	if cmd != nil {
		t.Errorf("expected no message when opening the theme picker, got %v", collectMsgs(cmd))
	}
	if m.mode != modeTheme {
		t.Fatal("expected the palette to be in theme mode")
	}
	if len(m.themeNames) == 0 {
		t.Fatal("expected theme names to be populated")
	}

	before := m.themeCursor
	m, cmd = m.Update(key("down"))
	got := collectMsgs(cmd)
	if len(got) != 1 {
		t.Fatalf("expected exactly one message on move, got %v", got)
	}
	preview, ok := got[0].(ThemePreviewMsg)
	if !ok {
		t.Fatalf("expected ThemePreviewMsg, got %T", got[0])
	}
	if preview.Name != m.themeNames[m.themeCursor] {
		t.Errorf("preview name = %q, want %q", preview.Name, m.themeNames[m.themeCursor])
	}
	if m.themeCursor == before {
		t.Error("expected the theme cursor to move")
	}
}

func TestSwitchThemeEnterChoosesAndCloses(t *testing.T) {
	m := newTestPalette(t)
	m = findCommand(t, m, idSwitchTheme)
	m, _ = m.Update(key("enter"))

	m, _ = m.Update(key("down"))
	want := m.themeNames[m.themeCursor]

	_, cmd := m.Update(key("enter"))
	got := collectMsgs(cmd)

	chosen, ok := findMsg[ThemeChosenMsg](got)
	if !ok {
		t.Fatalf("expected ThemeChosenMsg among %v", got)
	}
	if chosen.Name != want {
		t.Errorf("chosen theme = %q, want %q", chosen.Name, want)
	}
	if !hasMsgType(got, CloseMsg{}) {
		t.Errorf("expected CloseMsg among %v", got)
	}
}

func TestSwitchThemeEscCancelsThenClosesOnSecondEsc(t *testing.T) {
	m := newTestPalette(t)
	m = findCommand(t, m, idSwitchTheme)
	m, _ = m.Update(key("enter"))

	m, cmd := m.Update(key("esc"))
	got := collectMsgs(cmd)
	cancel, ok := findMsg[ThemeCancelMsg](got)
	if !ok {
		t.Fatalf("expected ThemeCancelMsg among %v", got)
	}
	if cancel.Original != "catppuccin-mocha" {
		t.Errorf("cancel original = %q, want %q", cancel.Original, "catppuccin-mocha")
	}
	if m.mode != modeCommands {
		t.Fatal("expected esc to return to the command list")
	}

	_, cmd = m.Update(key("esc"))
	got = collectMsgs(cmd)
	if !hasMsgType(got, CloseMsg{}) {
		t.Errorf("expected the second esc to close the palette, got %v", got)
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

func TestSetSizeNeverExceedsTinyTerminal(t *testing.T) {
	for _, tc := range []struct{ w, h int }{{20, 4}, {10, 3}} {
		m := New(DefaultCommands(), testStyles(t)).SetSize(tc.w, tc.h)
		if m.width > tc.w {
			t.Errorf("SetSize(%d,%d): width = %d, exceeds terminal width %d", tc.w, tc.h, m.width, tc.w)
		}
		if m.height > tc.h {
			t.Errorf("SetSize(%d,%d): height = %d, exceeds terminal height %d", tc.w, tc.h, m.height, tc.h)
		}
		// Rendering at this size must not panic.
		_ = m.View()
	}
}

func TestHighlightUsesByteOffsetsNotRuneIndexes(t *testing.T) {
	styles := testStyles(t)
	// "café bar": é is a 2-byte UTF-8 rune, so from there on the byte
	// offset of every following rune is one ahead of its rune index. The
	// 'b' in "bar" sits at rune index 5 but byte offset 6.
	s := "café bar"

	matches := fuzzy.Find("b", []string{s})
	if len(matches) != 1 || len(matches[0].MatchedIndexes) != 1 {
		t.Fatalf("unexpected fuzzy match for %q: %+v", s, matches)
	}

	got := highlight(s, matches[0].MatchedIndexes, styles.Match)

	if plain := ansi.Strip(got); plain != s {
		t.Fatalf("highlight corrupted the string: got %q, want %q", plain, s)
	}

	wantStyled := styles.Match.Render("b")
	if !strings.Contains(got, wantStyled) {
		t.Errorf("expected the matched %q to be styled (looking for %q) in %q", "b", wantStyled, got)
	}
	// A rune-index interpretation of the same byte offset would
	// incorrectly land one rune early, on the 'a' in "bar".
	wrongStyled := styles.Match.Render("a")
	if strings.Contains(got, wrongStyled) {
		t.Errorf("no %q should be styled for a match on %q, got %q", "a", "b", got)
	}
}

func TestRenderRowTruncatesNameBeforeKey(t *testing.T) {
	styles := testStyles(t)
	m := New([]Command{{
		ID:   "x",
		Name: "A very long command name that will not fit",
		Key:  "ctrl+shift+x",
	}}, styles).SetSize(100, 40)

	const width = 24 // deliberately narrow so name+key don't both fit
	row := m.renderRow(m.matches[0], false, width)
	plain := ansi.Strip(row)

	if got := ansi.StringWidth(plain); got != width {
		t.Errorf("row width = %d, want %d: %q", got, width, plain)
	}
	if !strings.Contains(plain, "…") {
		t.Errorf("expected the name to be truncated with an ellipsis, got %q", plain)
	}
	if !strings.Contains(plain, "ctrl+shift+x") {
		t.Errorf("expected the key to remain intact, got %q", plain)
	}
	if strings.Contains(plain, "will not fit") {
		t.Errorf("expected the tail of the long name to be cut, got %q", plain)
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

func findMsg[T any](list []tea.Msg) (T, bool) {
	var zero T
	for _, m := range list {
		if v, ok := m.(T); ok {
			return v, true
		}
	}
	return zero, false
}

func latteStyles(t *testing.T) theme.Styles {
	t.Helper()
	p, ok := theme.Get("catppuccin-latte")
	if !ok {
		t.Fatal("catppuccin-latte palette missing")
	}
	return theme.NewStyles(p)
}

func TestSetStylesRethemes(t *testing.T) {
	m := newTestPalette(t)
	before := m.View()
	after := m.SetStyles(latteStyles(t)).View()
	if before == after {
		t.Error("SetStyles did not change the rendering")
	}
	if ansi.Strip(before) != ansi.Strip(after) {
		t.Error("SetStyles changed the content")
	}
}
