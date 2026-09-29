package trash

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/mathieucroset/notty/internal/ui/msgs"
	"github.com/mathieucroset/notty/internal/ui/theme"
	"github.com/mathieucroset/notty/internal/vault"
)

func testTheme(t *testing.T) (theme.Styles, theme.Palette) {
	t.Helper()
	p, ok := theme.Get("catppuccin-mocha")
	if !ok {
		t.Fatal("palette missing")
	}
	return theme.NewStyles(p), p
}

func item(id, originalPath, host string, deletedAt time.Time) vault.TrashItem {
	return vault.TrashItem{
		ID:           id,
		OriginalPath: originalPath,
		Name:         originalPath[strings.LastIndex(originalPath, "/")+1:],
		Host:         host,
		DeletedAt:    deletedAt,
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

var now = time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)

func threeItems() []vault.TrashItem {
	return []vault.TrashItem{
		item("3", "Work/Standup notes.md", "laptop", now.Add(-3*time.Hour)),
		item("2", "Personal/journal.md", "desktop", now.Add(-2*24*time.Hour)),
		item("1", "readme.md", "", now.Add(-40*24*time.Hour)),
	}
}

func newTest(t *testing.T) Model {
	t.Helper()
	styles, palette := testTheme(t)
	m := New(styles, palette)
	m = m.SetSize(60, 20)
	m = m.SetItems(threeItems(), now)
	return m
}

func TestRelativeTime(t *testing.T) {
	tests := []struct {
		name string
		ago  time.Duration
		want string
	}{
		{"just now", 0, "just now"},
		{"seconds", 45 * time.Second, "just now"},
		{"one minute", time.Minute, "1m ago"},
		{"minutes", 5 * time.Minute, "5m ago"},
		{"one hour", time.Hour, "1h ago"},
		{"hours", 3 * time.Hour, "3h ago"},
		{"one day", 24 * time.Hour, "1d ago"},
		{"days", 2 * 24 * time.Hour, "2d ago"},
		{"29 days", 29 * 24 * time.Hour, "29d ago"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RelativeTime(now.Add(-tt.ago), now)
			if got != tt.want {
				t.Errorf("RelativeTime(-%v) = %q, want %q", tt.ago, got, tt.want)
			}
		})
	}
	// Beyond 30 days: an absolute date.
	past := now.Add(-31 * 24 * time.Hour)
	if got, want := RelativeTime(past, now), past.Format("2006-01-02"); got != want {
		t.Errorf("RelativeTime(-31d) = %q, want %q", got, want)
	}
}

func TestSetItemsKeepsSelectionByID(t *testing.T) {
	m := newTest(t)
	it, ok := m.Selected()
	if !ok || it.ID != "3" {
		t.Fatalf("initial selection = %+v, %v, want id 3", it, ok)
	}
	m, _ = send(m, "j")
	it, ok = m.Selected()
	if !ok || it.ID != "2" {
		t.Fatalf("after j, selection = %+v, %v, want id 2", it, ok)
	}

	// Re-set with the same items in a different slice: selection survives.
	m = m.SetItems(threeItems(), now)
	it, ok = m.Selected()
	if !ok || it.ID != "2" {
		t.Fatalf("after SetItems, selection = %+v, %v, want id 2 (kept)", it, ok)
	}

	// Re-set without the selected item: falls back to the newest.
	m = m.SetItems([]vault.TrashItem{threeItems()[0], threeItems()[2]}, now)
	it, ok = m.Selected()
	if !ok || it.ID != "3" {
		t.Fatalf("after SetItems without id 2, selection = %+v, %v, want id 3", it, ok)
	}
}

func TestSetItemsEmpty(t *testing.T) {
	m := newTest(t)
	m = m.SetItems(nil, now)
	if _, ok := m.Selected(); ok {
		t.Error("Selected() ok = true for an empty trash")
	}
}

func TestTitle(t *testing.T) {
	m := newTest(t)
	if got, want := m.Title(), "Trash · 3 items"; got != want {
		t.Errorf("Title() = %q, want %q", got, want)
	}
	m = m.SetItems(nil, now)
	if got, want := m.Title(), "Trash · 0 items"; got != want {
		t.Errorf("Title() (empty) = %q, want %q", got, want)
	}
}

func TestUpdateKeyMessages(t *testing.T) {
	tests := []struct {
		name string
		key  string
		want tea.Msg
	}{
		{"enter restores", "enter", RestoreMsg{Item: threeItems()[0]}},
		{"D deletes forever", "D", DeleteForeverMsg{Item: threeItems()[0]}},
		{"E empties trash", "E", EmptyTrashMsg{}},
		{"tab focuses sidebar", "tab", msgs.FocusSidebarMsg{}},
		{"esc goes back", "esc", BackMsg{}},
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

func TestMoveSelectionEmitsSelectionChanged(t *testing.T) {
	m := newTest(t)
	m, got := send(m, "j")
	want := SelectionChangedMsg{Item: threeItems()[1]}
	if got != want {
		t.Errorf("after j, msg = %#v, want %#v", got, want)
	}

	m, got = send(m, "k")
	want = SelectionChangedMsg{Item: threeItems()[0]}
	if got != want {
		t.Errorf("after k, msg = %#v, want %#v", got, want)
	}

	// k at the top does nothing (no message, no change).
	m, got = send(m, "k")
	if got != nil {
		t.Errorf("k at the top emitted %#v, want nil", got)
	}
	if it, _ := m.Selected(); it.ID != "3" {
		t.Errorf("selection after k at the top = %q, want 3", it.ID)
	}

	// j at the bottom does nothing.
	m, _ = send(m, "j")
	m, _ = send(m, "j")
	m, got = send(m, "j")
	if got != nil {
		t.Errorf("j at the bottom emitted %#v, want nil", got)
	}
}

// TestPreviewCachedAcrossViewAndUnrelatedMoves proves the preview's Glamour
// render is computed once per (item, width, palette) and then only read
// from the cache: repeated View() calls, and moving the selection away
// from and back to the previewed item, must not trigger a second render.
func TestPreviewCachedAcrossViewAndUnrelatedMoves(t *testing.T) {
	m := newTest(t)
	it, _ := m.Selected()

	count := 0
	m.onRender = func() { count++ }

	m = m.SetPreview(it.ID, "# Hello\n\nWorld")
	if count != 1 {
		t.Fatalf("SetPreview (a cache miss) rendered %d times, want 1", count)
	}

	for i := 0; i < 10; i++ {
		_ = m.View()
	}
	if count != 1 {
		t.Errorf("View ×10 rendered %d times in total, want 1 (all cache hits)", count)
	}

	// Re-setting the same preview content is also a cache hit.
	m = m.SetPreview(it.ID, "# Hello\n\nWorld")
	if count != 1 {
		t.Errorf("SetPreview with an unchanged key re-rendered: total = %d, want 1", count)
	}

	// Moving away (the preview no longer matches the selection, so nothing
	// to render) and back again does not re-render either: the item's
	// cached render is still valid.
	m, _ = send(m, "j")
	m, _ = send(m, "k")
	if it2, _ := m.Selected(); it2.ID != it.ID {
		t.Fatalf("selection after j,k = %q, want back to %q", it2.ID, it.ID)
	}
	if count != 1 {
		t.Errorf("moving away and back re-rendered: total = %d, want 1", count)
	}
}

// TestPreviewRecomputesOnCacheKeyChange proves the cache key really does
// invalidate the cache: a width change or a different item must trigger a
// recompute, and re-applying an unchanged size must not.
func TestPreviewRecomputesOnCacheKeyChange(t *testing.T) {
	m := newTest(t)
	it, _ := m.Selected()
	m = m.SetPreview(it.ID, "# Hello\n\nWorld")

	count := 0
	m.onRender = func() { count++ }

	m = m.SetSize(m.width*2, m.height) // a width change
	if count != 1 {
		t.Errorf("SetSize (width change) rendered %d times, want 1", count)
	}
	m = m.SetSize(m.width, m.height) // the same size again: a cache hit
	if count != 1 {
		t.Errorf("SetSize with an unchanged size re-rendered: total = %d, want 1", count)
	}

	// Selecting a different item and loading its preview is a new key.
	m, _ = send(m, "j")
	next, _ := m.Selected()
	m = m.SetPreview(next.ID, "# Another\n\nNote")
	if count != 2 {
		t.Errorf("SetPreview for a different item total renders = %d, want 2", count)
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

// wideRuneItems exercises CJK (double-width) and emoji (often double-width,
// sometimes built from several runes) characters in the name, folder, and
// host fields that end up in the rendered list rows.
func wideRuneItems() []vault.TrashItem {
	return []vault.TrashItem{
		item("w3", "笔记/会议记录 🎉.md", "笔记本💻", now.Add(-time.Hour)),
		item("w2", "emoji folder 😀😃😄/note.md", "desktop", now.Add(-2*time.Hour)),
		item("w1", "readme.md", "台式机🖥️", now.Add(-3*time.Hour)),
	}
}

func TestViewExactBounds(t *testing.T) {
	sizes := [][2]int{{60, 20}, {40, 10}, {80, 5}, {20, 3}}
	datasets := [][]vault.TrashItem{threeItems(), wideRuneItems()}
	for _, items := range datasets {
		for _, sz := range sizes {
			m := newTest(t)
			m = m.SetItems(items, now)
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

func TestViewEmptyState(t *testing.T) {
	m := newTest(t)
	m = m.SetItems(nil, now)
	v := m.View()
	if !strings.Contains(ansi.Strip(v), "Trash is empty") {
		t.Errorf("View() for an empty trash = %q, want it to contain %q", v, "Trash is empty")
	}
	lines := strings.Split(v, "\n")
	if len(lines) != 20 {
		t.Errorf("empty state height = %d, want 20", len(lines))
	}
}

func TestViewShowsPreviewOnceLoaded(t *testing.T) {
	m := newTest(t)
	it, _ := m.Selected()
	m = m.SetPreview(it.ID, "# Hello\n\nWorld")
	v := ansi.Strip(m.View())
	if !strings.Contains(v, "Hello") {
		t.Errorf("View() with a loaded preview does not contain %q:\n%s", "Hello", v)
	}
}

func TestViewShowsLoadingBeforePreviewArrives(t *testing.T) {
	m := newTest(t)
	v := ansi.Strip(m.View())
	if !strings.Contains(v, "loading") {
		t.Errorf("View() before the preview loads does not contain %q:\n%s", "loading", v)
	}
}

func TestSetThemeRethemes(t *testing.T) {
	m := newTest(t)
	m = m.SetPreview("3", "# Standup\n\nbody")
	before := m.View()
	p, ok := theme.Get("catppuccin-latte")
	if !ok {
		t.Fatal("palette missing")
	}
	after := m.SetTheme(theme.NewStyles(p), p).View()
	if before == after {
		t.Error("SetTheme did not change the rendering")
	}
	if ansi.Strip(before) != ansi.Strip(after) {
		t.Error("SetTheme changed the content")
	}
}
