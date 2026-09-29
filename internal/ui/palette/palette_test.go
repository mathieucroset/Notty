package palette

import (
	"fmt"
	"slices"
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

func TestSwitchThemeEnterEmitsOnlyTheChoice(t *testing.T) {
	m := newTestPalette(t)
	m = findCommand(t, m, idSwitchTheme)
	m, _ = m.Update(key("enter"))

	m, _ = m.Update(key("down"))
	want := m.themeNames[m.themeCursor]

	_, cmd := m.Update(key("enter"))
	got := collectMsgs(cmd)

	// The app resolves the choice and closes the palette itself, so the
	// choice comes alone, not batched with a CloseMsg.
	if len(got) != 1 {
		t.Fatalf("enter emitted %v, want exactly one ThemeChosenMsg", got)
	}
	chosen, ok := got[0].(ThemeChosenMsg)
	if !ok {
		t.Fatalf("enter emitted %T, want ThemeChosenMsg", got[0])
	}
	if chosen.Name != want {
		t.Errorf("chosen theme = %q, want %q", chosen.Name, want)
	}
}

func TestSwitchThemeEscCancelsThenClosesOnSecondEsc(t *testing.T) {
	m := newTestPalette(t)
	m = findCommand(t, m, idSwitchTheme)
	m, _ = m.Update(key("enter"))
	m, _ = m.Update(key("down"))

	m, cmd := m.Update(key("esc"))
	got := collectMsgs(cmd)
	if len(got) != 1 || got[0] != (ThemeCancelMsg{}) {
		t.Fatalf("esc emitted %v, want exactly one ThemeCancelMsg{}", got)
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

// openThemeMode opens the theme picker of a palette listing names.
func openThemeMode(t *testing.T, names []string, current string) Model {
	t.Helper()
	m := New(DefaultCommands(), testStyles(t)).WithCurrentTheme(current).WithThemeNames(names).SetSize(100, 40)
	m = findCommand(t, m, idSwitchTheme)
	m, _ = m.Update(key("enter"))
	if m.mode != modeTheme {
		t.Fatal("expected the palette to be in theme mode")
	}
	return m
}

func TestWithThemeNamesListsExactlyThose(t *testing.T) {
	m := openThemeMode(t, []string{"a", "b"}, "b")
	if !slices.Equal(m.themeNames, []string{"a", "b"}) {
		t.Errorf("theme names = %v, want [a b]", m.themeNames)
	}
	if m.themeCursor != 1 {
		t.Errorf("cursor = %d, want 1 (the current theme)", m.themeCursor)
	}
	lines := strings.Split(ansi.Strip(m.View()), "\n")
	var listed []string
	for _, l := range lines {
		l = strings.Trim(l, "│ ")
		l = strings.TrimSpace(strings.TrimPrefix(l, m.styles.Icons.Check))
		if l == "a" || l == "b" {
			listed = append(listed, l)
		}
	}
	if !slices.Equal(listed, []string{"a", "b"}) {
		t.Errorf("view lists %v, want [a b]:\n%s", listed, ansi.Strip(m.View()))
	}
}

func TestSetThemeNames(t *testing.T) {
	tests := []struct {
		name       string
		before     []string
		cursorOn   string
		after      []string
		wantCursor int
	}{
		{"cursor name kept", []string{"a", "b", "c"}, "b", []string{"x", "b", "y"}, 1},
		{"cursor name moved", []string{"a", "b", "c"}, "c", []string{"c", "d"}, 0},
		{"cursor name removed, clamped", []string{"a", "b", "c"}, "c", []string{"a", "b"}, 1},
		{"list emptied", []string{"a", "b"}, "b", nil, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := openThemeMode(t, tt.before, tt.cursorOn)
			m = m.SetThemeNames(tt.after)
			if !slices.Equal(m.themeNames, tt.after) {
				t.Errorf("theme names = %v, want %v", m.themeNames, tt.after)
			}
			if m.themeCursor != tt.wantCursor {
				t.Errorf("cursor = %d, want %d", m.themeCursor, tt.wantCursor)
			}
			_ = m.View() // must not panic
		})
	}
}

func TestSetThemeNamesBeforeThemeMode(t *testing.T) {
	m := New(DefaultCommands(), testStyles(t)).WithCurrentTheme("b").WithThemeNames([]string{"a"}).SetSize(100, 40)
	m = m.SetThemeNames([]string{"a", "b"})
	m = findCommand(t, m, idSwitchTheme)
	m, _ = m.Update(key("enter"))
	if !slices.Equal(m.themeNames, []string{"a", "b"}) || m.themeCursor != 1 {
		t.Errorf("theme names = %v, cursor %d; want [a b], 1", m.themeNames, m.themeCursor)
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

// TestThemePreviewSeq: each preview carries a larger Seq, so the app can
// drop one that arrives after a newer one.
func TestThemePreviewSeq(t *testing.T) {
	m := openThemeMode(t, []string{"a", "b", "c"}, "a")
	var seqs []uint64
	for _, k := range []string{"down", "down", "up"} {
		var cmd tea.Cmd
		m, cmd = m.Update(key(k))
		got := collectMsgs(cmd)
		if len(got) != 1 {
			t.Fatalf("%s emitted %v, want one preview", k, got)
		}
		p, ok := got[0].(ThemePreviewMsg)
		if !ok {
			t.Fatalf("%s emitted %T, want ThemePreviewMsg", k, got[0])
		}
		seqs = append(seqs, p.Seq)
	}
	for i := 1; i < len(seqs); i++ {
		if seqs[i] <= seqs[i-1] {
			t.Errorf("preview seqs %v are not increasing", seqs)
		}
	}
}

func TestInThemeMode(t *testing.T) {
	m := newTestPalette(t)
	if m.InThemeMode() {
		t.Error("a new palette is in theme mode")
	}
	m = findCommand(t, m, idSwitchTheme)
	m, _ = m.Update(key("enter"))
	if !m.InThemeMode() {
		t.Error("the theme picker is not in theme mode")
	}
	m, _ = m.Update(key("esc"))
	if m.InThemeMode() {
		t.Error("esc left the palette in theme mode")
	}
}
