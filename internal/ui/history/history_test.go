package history

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/mathieucroset/notty/internal/gitsync"
	"github.com/mathieucroset/notty/internal/ui/theme"
)

func testTheme(t *testing.T) (theme.Styles, theme.Palette) {
	t.Helper()
	p, ok := theme.Get("catppuccin-mocha")
	if !ok {
		t.Fatal("palette missing")
	}
	return theme.NewStyles(p), p
}

var baseDate = time.Date(2026, 9, 29, 14, 3, 0, 0, time.UTC)

func threeEntries() []gitsync.LogEntry {
	return []gitsync.LogEntry{
		{Rev: "c3", Date: baseDate, Subject: "Update a.md · laptop", Host: "laptop", Added: 3, Deleted: 1},
		{Rev: "c2", Date: baseDate.Add(-24 * time.Hour), Subject: "Update a.md · desktop", Host: "desktop", Added: 5, Deleted: 0},
		{Rev: "c1", Date: baseDate.Add(-72 * time.Hour), Subject: "Create a.md · laptop", Host: "laptop", Added: 10, Deleted: 0},
	}
}

func newTest(t *testing.T) Model {
	t.Helper()
	styles, palette := testTheme(t)
	m := New("Work/a.md", "line one\nline two\nline three\n", styles, palette)
	m = m.SetSize(80, 24)
	m = m.SetEntries(threeEntries())
	return m
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
	}
	if strings.HasPrefix(s, "ctrl+") {
		return tea.KeyPressMsg{Code: rune(s[5]), Mod: tea.ModCtrl}
	}
	return tea.KeyPressMsg{Code: rune(s[0]), Text: s}
}

func send(m Model, k string) (Model, tea.Msg) {
	m, cmd := m.Update(key(k))
	if cmd == nil {
		return m, nil
	}
	return m, cmd()
}

func TestSetEntriesKeepsSelectionByRev(t *testing.T) {
	m := newTest(t)
	m, _ = send(m, "j")
	if !selected(t, m, "c2") {
		t.Fatalf("selection after j should be c2")
	}
	m = m.SetEntries(threeEntries())
	if !selected(t, m, "c2") {
		t.Fatalf("selection should survive SetEntries with the same revs")
	}
	m = m.SetEntries([]gitsync.LogEntry{threeEntries()[0], threeEntries()[2]})
	if !selected(t, m, "c3") {
		t.Fatalf("selection should fall back to the newest entry when c2 is gone")
	}
}

func selected(t *testing.T, m Model, rev string) bool {
	t.Helper()
	e, ok := m.selected()
	return ok && e.Rev == rev
}

func TestUpdateKeyMessages(t *testing.T) {
	tests := []struct {
		name string
		key  string
		want tea.Msg
	}{
		{"j moves down", "j", SelectionChangedMsg{Rev: "c2"}},
		{"esc closes", "esc", CloseMsg{}},
		{"q closes", "q", CloseMsg{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newTest(t)
			_, got := send(m, tt.key)
			if got != tt.want {
				t.Errorf("after %q, msg = %#v, want %#v", tt.key, got, tt.want)
			}
		})
	}
}

func TestGGAndGJumpToEnds(t *testing.T) {
	m := newTest(t)
	m, got := send(m, "G")
	if got != (SelectionChangedMsg{Rev: "c1"}) {
		t.Errorf("G: msg = %#v, want select c1", got)
	}

	// A lone "g" is only the first half of the "gg" chord: it moves nothing.
	m, got = send(m, "g")
	if got != nil {
		t.Errorf("single g: msg = %#v, want nil (waiting for a second g)", got)
	}
	m, got = send(m, "g")
	if got != (SelectionChangedMsg{Rev: "c3"}) {
		t.Errorf("gg: msg = %#v, want select c3", got)
	}
	// Now at the top (c3): gg again emits nothing (no change).
	m, _ = send(m, "g")
	m, got = send(m, "g")
	if got != nil {
		t.Errorf("gg at the top emitted %#v, want nil", got)
	}

	// Move back down, then show a pending "g" is cancelled by any other
	// key: "gj" moves down by one (to c2), not a jump to the top.
	m, got = send(m, "j")
	if got != (SelectionChangedMsg{Rev: "c2"}) {
		t.Fatalf("j: msg = %#v, want select c2", got)
	}
	m, _ = send(m, "g")
	m, got = send(m, "j")
	if got != (SelectionChangedMsg{Rev: "c1"}) {
		t.Errorf("gj: msg = %#v, want plain j (select c1), not a gg jump", got)
	}
}

func TestEnterOnlyRestoresLoadedVersion(t *testing.T) {
	m := newTest(t)
	_, got := send(m, "enter")
	if got != nil {
		t.Errorf("enter with no version loaded emitted %#v, want nil", got)
	}

	e, _ := m.selected()
	m = m.SetVersion(e.Rev, "old content\n")
	_, got = send(m, "enter")
	want := RestoreVersionMsg{Path: "Work/a.md", Rev: e.Rev, Content: "old content\n"}
	if got != want {
		t.Errorf("enter with a loaded version = %#v, want %#v", got, want)
	}

	// Moving away from the loaded revision means enter does nothing again.
	m, _ = send(m, "j")
	_, got = send(m, "enter")
	if got != nil {
		t.Errorf("enter after moving off the loaded revision emitted %#v, want nil", got)
	}
}

func TestTabTogglesMode(t *testing.T) {
	m := newTest(t)
	if m.mode != modeRendered {
		t.Fatalf("initial mode = %v, want modeRendered", m.mode)
	}
	m, cmd := m.Update(key("tab"))
	if cmd != nil {
		t.Errorf("tab returned a non-nil command")
	}
	if m.mode != modeDiff {
		t.Errorf("mode after tab = %v, want modeDiff", m.mode)
	}
	m, _ = m.Update(key("tab"))
	if m.mode != modeRendered {
		t.Errorf("mode after second tab = %v, want modeRendered", m.mode)
	}
}

// TestRightContentCachedAcrossScrollAndView proves the right pane's content
// (a Glamour render, here) is computed once for a loaded revision and then
// only read from the cache: ten pgdown presses plus ten View() calls must
// not trigger a second render.
func TestRightContentCachedAcrossScrollAndView(t *testing.T) {
	m := newTest(t)
	e, _ := m.selected()
	var lines []string
	for i := 0; i < 50; i++ {
		lines = append(lines, fmt.Sprintf("line %d", i))
	}
	content := strings.Join(lines, "\n")

	count := 0
	m.onRender = func() { count++ }

	m = m.SetVersion(e.Rev, content)
	if count != 1 {
		t.Fatalf("SetVersion (a cache miss) rendered %d times, want 1", count)
	}

	for i := 0; i < 10; i++ {
		m, _ = m.Update(key("pgdown"))
	}
	for i := 0; i < 10; i++ {
		_ = m.View()
	}
	if count != 1 {
		t.Errorf("pgdown ×10 + View ×10 rendered %d times in total, want 1 (all cache hits after the initial load)", count)
	}

	// Re-setting the very same revision and content is also a cache hit.
	m = m.SetVersion(e.Rev, content)
	if count != 1 {
		t.Errorf("SetVersion with an unchanged key re-rendered: total = %d, want 1", count)
	}
}

// TestRightContentRecomputesOnCacheKeyChange proves the cache key really
// does invalidate the cache: a mode toggle or a width change must trigger
// exactly one recompute, and re-applying an unchanged size must not.
func TestRightContentRecomputesOnCacheKeyChange(t *testing.T) {
	m := newTest(t)
	e, _ := m.selected()
	m = m.SetVersion(e.Rev, "line one\nline two\n")

	count := 0
	m.onRender = func() { count++ }

	m, _ = m.Update(key("tab")) // rendered -> diff: a mode change
	if count != 1 {
		t.Errorf("tab (mode change) rendered %d times, want 1", count)
	}
	m, _ = m.Update(key("tab")) // diff -> rendered: a mode change back
	if count != 2 {
		t.Errorf("tab back (mode change) total renders = %d, want 2", count)
	}

	m = m.SetSize(m.width*2, m.height) // a width change
	if count != 3 {
		t.Errorf("SetSize (width change) total renders = %d, want 3", count)
	}

	m = m.SetSize(m.width, m.height) // the same size again: a cache hit
	if count != 3 {
		t.Errorf("SetSize with an unchanged size re-rendered: total = %d, want 3", count)
	}
}

func TestNoOpKeysReturnNil(t *testing.T) {
	m := newTest(t)
	if _, cmd := m.Update(tea.KeyPressMsg{Code: 'z', Text: "z"}); cmd != nil {
		t.Error("unmapped key returned a non-nil command")
	}
	if _, cmd := m.Update(tea.WindowSizeMsg{}); cmd != nil {
		t.Error("non-key message returned a non-nil command")
	}
}

// wideRuneEntries exercises CJK (double-width) and emoji (often
// double-width, sometimes built from several runes) characters in the
// subject/host fields that end up in the rendered list rows.
func wideRuneEntries() []gitsync.LogEntry {
	return []gitsync.LogEntry{
		{Rev: "w3", Date: baseDate, Subject: "更新笔记 · 笔记本💻", Host: "笔记本💻", Added: 3, Deleted: 1},
		{Rev: "w2", Date: baseDate.Add(-24 * time.Hour), Subject: "emoji party 🎉😀🚀", Host: "desktop🖥️", Added: 5, Deleted: 0},
		{Rev: "w1", Date: baseDate.Add(-72 * time.Hour), Subject: "Create a.md", Host: "laptop", Added: 10, Deleted: 0},
	}
}

func TestViewExactBounds(t *testing.T) {
	sizes := [][2]int{{80, 24}, {60, 10}, {40, 5}, {30, 2}, {30, 1}}
	datasets := [][]gitsync.LogEntry{threeEntries(), wideRuneEntries()}
	for _, entries := range datasets {
		for _, sz := range sizes {
			m := newTest(t)
			m = m.SetEntries(entries)
			m = m.SetSize(sz[0], sz[1])
			v := m.View()
			lines := strings.Split(v, "\n")
			if len(lines) != sz[1] {
				t.Errorf("size %v: got %d lines, want %d", sz, len(lines), sz[1])
			}
			for i, l := range lines {
				if w := ansi.StringWidth(l); w != sz[0] {
					t.Errorf("size %v: line %d width = %d, want %d (%q)", sz, i, w, sz[0], l)
				}
			}
		}
	}
}

func TestViewZeroSize(t *testing.T) {
	m := newTest(t)
	m = m.SetSize(0, 0)
	if v := m.View(); v != "" {
		t.Errorf("View() at 0x0 = %q, want empty", v)
	}
}

func TestViewEmptyHistory(t *testing.T) {
	m := newTest(t)
	m = m.SetEntries(nil)
	v := ansi.Strip(m.View())
	if !strings.Contains(v, "No history yet") {
		t.Errorf("View() for empty history = %q, want it to contain %q", v, "No history yet")
	}
}

func TestViewHeaderShowsNotePath(t *testing.T) {
	m := newTest(t)
	v := ansi.Strip(m.View())
	if !strings.Contains(v, "History · Work/a.md") {
		t.Errorf("View() header missing note path:\n%s", v)
	}
}

func TestViewShowsLoadingBeforeVersionArrives(t *testing.T) {
	m := newTest(t)
	v := ansi.Strip(m.View())
	if !strings.Contains(v, "loading") {
		t.Errorf("View() before the version loads does not contain %q:\n%s", "loading", v)
	}
}

func TestViewRenderedShowsMarkdown(t *testing.T) {
	m := newTest(t)
	e, _ := m.selected()
	m = m.SetVersion(e.Rev, "# Hello\n\nWorld")
	v := ansi.Strip(m.View())
	if !strings.Contains(v, "Hello") {
		t.Errorf("View() rendered mode missing content:\n%s", v)
	}
}

func TestViewDiffShowsInsertAndDelete(t *testing.T) {
	m := newTest(t)
	e, _ := m.selected()
	// currentContent (from newTest) is "line one\nline two\nline three\n".
	m = m.SetVersion(e.Rev, "line one\nline two\nline four\n")
	m, _ = m.Update(key("tab")) // switch to diff mode
	if m.mode != modeDiff {
		t.Fatalf("mode = %v, want modeDiff", m.mode)
	}
	raw := m.View()
	if !strings.Contains(raw, "+ line three") {
		t.Errorf("diff view missing insert line:\n%s", ansi.Strip(raw))
	}
	if !strings.Contains(raw, "- line four") {
		t.Errorf("diff view missing delete line:\n%s", ansi.Strip(raw))
	}
}

func TestViewDiffNoChanges(t *testing.T) {
	m := newTest(t)
	e, _ := m.selected()
	m = m.SetVersion(e.Rev, m.currentContent)
	m, _ = m.Update(key("tab"))
	v := ansi.Strip(m.View())
	if !strings.Contains(v, "No changes") {
		t.Errorf("View() diff with no changes = %q, want it to contain %q", v, "No changes")
	}
}

func TestScrollClampedToContent(t *testing.T) {
	m := newTest(t)
	m = m.SetSize(80, 6) // small body, so the content overflows
	e, _ := m.selected()
	var lines []string
	for i := 0; i < 50; i++ {
		lines = append(lines, "line")
	}
	m = m.SetVersion(e.Rev, strings.Join(lines, "\n"))

	for i := 0; i < 100; i++ {
		m, _ = m.Update(key("pgdown"))
	}
	afterMax := m.scroll
	m, _ = m.Update(key("pgdown"))
	if m.scroll != afterMax {
		t.Errorf("scroll kept growing past the max: %d -> %d", afterMax, m.scroll)
	}

	for i := 0; i < 100; i++ {
		m, _ = m.Update(key("pgup"))
	}
	if m.scroll != 0 {
		t.Errorf("scroll after pgup past the top = %d, want 0", m.scroll)
	}
}
