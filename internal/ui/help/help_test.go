package help

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

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
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "pgdown":
		return tea.KeyPressMsg{Code: tea.KeyPgDown}
	case "pgup":
		return tea.KeyPressMsg{Code: tea.KeyPgUp}
	case "f1":
		return tea.KeyPressMsg{Code: tea.KeyF1}
	}
	return tea.KeyPressMsg{Code: rune(s[0]), Text: s}
}

func TestEscQF1Close(t *testing.T) {
	for _, k := range []string{"esc", "q", "f1"} {
		m := New(testStyles(t)).SetSize(120, 40)
		_, cmd := m.Update(key(k))
		if cmd == nil {
			t.Fatalf("%q: expected a command", k)
		}
		msg := cmd()
		if _, ok := msg.(CloseMsg); !ok {
			t.Errorf("%q: got %T, want CloseMsg", k, msg)
		}
	}
}

func TestScrollDownIsBoundedAtEnd(t *testing.T) {
	m := New(testStyles(t)).SetSize(80, 20)
	for i := 0; i < 500; i++ {
		m, _ = m.Update(key("down"))
	}
	if m.offset != m.maxOffset() {
		t.Errorf("offset = %d, want maxOffset %d", m.offset, m.maxOffset())
	}
	if m.offset < 0 {
		t.Error("offset went negative")
	}
}

func TestScrollUpIsBoundedAtStart(t *testing.T) {
	m := New(testStyles(t)).SetSize(80, 20)
	for i := 0; i < 500; i++ {
		m, _ = m.Update(key("up"))
	}
	if m.offset != 0 {
		t.Errorf("offset = %d, want 0", m.offset)
	}
}

func TestGAndCapitalGJumpToEnds(t *testing.T) {
	m := New(testStyles(t)).SetSize(80, 20)

	m, _ = m.Update(key("G"))
	if m.offset != m.maxOffset() {
		t.Errorf("G: offset = %d, want maxOffset %d", m.offset, m.maxOffset())
	}

	m, _ = m.Update(key("g"))
	if m.offset != 0 {
		t.Errorf("g: offset = %d, want 0", m.offset)
	}
}

func TestPageUpDownStayInBounds(t *testing.T) {
	m := New(testStyles(t)).SetSize(80, 20)

	m, _ = m.Update(key("pgdown"))
	if m.offset < 0 || m.offset > m.maxOffset() {
		t.Errorf("pgdown: offset %d out of bounds [0, %d]", m.offset, m.maxOffset())
	}

	for i := 0; i < 100; i++ {
		m, _ = m.Update(key("pgdown"))
	}
	if m.offset != m.maxOffset() {
		t.Errorf("pgdown (repeated): offset = %d, want maxOffset %d", m.offset, m.maxOffset())
	}

	for i := 0; i < 100; i++ {
		m, _ = m.Update(key("pgup"))
	}
	if m.offset != 0 {
		t.Errorf("pgup (repeated): offset = %d, want 0", m.offset)
	}
}

func TestSetSizeSizesRelativeToTerminal(t *testing.T) {
	m := New(testStyles(t)).SetSize(120, 40)
	if want := 120 * widthPct / 100; m.width != want {
		t.Errorf("width = %d, want %d", m.width, want)
	}
	if want := 40 * heightPct / 100; m.height != want {
		t.Errorf("height = %d, want %d", m.height, want)
	}
}

func TestSetSizeNeverExceedsTinyTerminal(t *testing.T) {
	for _, tc := range []struct{ w, h int }{{20, 4}, {10, 3}} {
		m := New(testStyles(t)).SetSize(tc.w, tc.h)
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

// TestFormatSectionNeverTruncatesLongKeyLabel guards against silently
// clipping a long multi-key label (like the vim text-object row, whose
// Keys ends in "ip ap") when the key column is capped below its width: the
// label must wrap onto its own line instead of losing characters.
func TestFormatSectionNeverTruncatesLongKeyLabel(t *testing.T) {
	var longRow Row
	for _, sec := range Sections() {
		for _, r := range sec.Rows {
			if strings.Contains(r.Keys, "ip ap") {
				longRow = r
			}
		}
	}
	if longRow.Keys == "" {
		t.Fatal("expected to find the vim text-object row (Keys containing \"ip ap\") in Sections()")
	}

	sec := Section{Title: "Test", Rows: []Row{longRow}}
	// Narrower than the full key label, but wide enough to hold it on a
	// line of its own.
	width := 40
	if w := ansi.StringWidth(longRow.Keys); w >= width {
		width = w + 10
	}

	lines := formatSection(sec, testStyles(t), width)
	plain := ansi.Strip(strings.Join(lines, "\n"))

	if !strings.Contains(plain, longRow.Keys) {
		t.Errorf("expected the complete key label %q to appear verbatim, got:\n%s", longRow.Keys, plain)
	}
	if !strings.Contains(plain, "ip ap") {
		t.Errorf("expected the label's tail \"ip ap\" to survive un-truncated, got:\n%s", plain)
	}
	for _, l := range lines {
		if w := ansi.StringWidth(l); w > width {
			t.Errorf("line exceeds width %d: %q", width, l)
		}
	}
}

func TestViewFitsWithinSize(t *testing.T) {
	m := New(testStyles(t)).SetSize(120, 40)
	view := m.View()

	lines := splitLines(view)
	if len(lines) > m.height {
		t.Errorf("view has %d lines, exceeds box height %d", len(lines), m.height)
	}
	for _, l := range lines {
		if w := ansi.StringWidth(l); w > m.width {
			t.Errorf("line width %d exceeds box width %d: %q", w, m.width, l)
		}
	}
}

func TestRenderContentUsesTwoColumnsAboveThreshold(t *testing.T) {
	wide := New(testStyles(t)).SetSize(200, 60)
	narrow := New(testStyles(t)).SetSize(80, 60)

	wideContent := wide.renderContent()
	narrowContent := narrow.renderContent()

	if len(wideContent) >= len(narrowContent) {
		t.Errorf("two-column content (%d lines) should be shorter than one-column content (%d lines)",
			len(wideContent), len(narrowContent))
	}
}

func TestTwoColumnHalvesLineCountRoughly(t *testing.T) {
	m := New(testStyles(t))
	width := 140

	one := m.oneColumn(width)
	two := m.twoColumn(width)

	if len(two) >= len(one) {
		t.Errorf("two-column line count %d should be less than one-column %d", len(two), len(one))
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
