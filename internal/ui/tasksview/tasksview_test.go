package tasksview

import (
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/mathieucroset/notty/internal/index"
	"github.com/mathieucroset/notty/internal/tasks"
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

// today is the fixed "today" used across tests: 2026-09-29.
func testToday() time.Time {
	return time.Date(2026, 9, 29, 0, 0, 0, 0, time.Local)
}

func due(s string) *time.Time {
	t, err := time.ParseInLocation("2006-01-02", s, time.Local)
	if err != nil {
		panic(err)
	}
	return &t
}

// ref builds an index.TaskRef. text may contain an inline "@YYYY-MM-DD" due
// date, which is parsed the same way tasks.ParseLine would.
func ref(path, title string, line int, done bool, text string) index.TaskRef {
	checkbox := " "
	if done {
		checkbox = "x"
	}
	raw := "- [" + checkbox + "] " + text
	task, ok := tasks.ParseLine(raw)
	if !ok {
		panic("bad task line: " + raw)
	}
	task.Line = line
	return index.TaskRef{Path: path, Title: title, Task: task}
}

func newTest(t *testing.T) Model {
	t.Helper()
	m := New(testStyles(t), 7, false)
	m = m.SetSize(40, 20)
	return m
}

// selectedRef returns the (Path, Raw) of the selected task, or "".
func selectedID(m Model) string {
	it, ok := m.selected()
	if !ok {
		return ""
	}
	return it.id()
}

// headers returns the header texts in order, as shown by items.
func headers(m Model) []string {
	var out []string
	for _, it := range m.items {
		if it.kind == kindHeader {
			out = append(out, it.header)
		}
	}
	return out
}

// tasksIn returns the raw task lines in item order (across all groups).
func tasksIn(m Model) []string {
	var out []string
	for _, it := range m.items {
		if it.kind == kindTask {
			out = append(out, it.ref.Task.Raw)
		}
	}
	return out
}

func TestGrouping(t *testing.T) {
	refs := []index.TaskRef{
		ref("Work/admin.md", "admin", 0, false, "send invoice @2026-09-27"),
		ref("Work/standup.md", "standup", 0, false, "follow up with #design"),
		ref("Work/standup.md", "standup", 1, false, "fix login bug @2026-10-01"),
		ref("Work/standup.md", "standup", 2, true, "done one"),
		ref("Work/standup.md", "standup", 3, true, "done two"),
		ref("Work/standup.md", "standup", 4, false, "third open"),
		ref("Personal/groceries.md", "groceries", 0, false, "oat milk"),
	}
	m := newTest(t)
	m = m.SetTasks(refs, testToday())

	if got, want := headers(m), []string{"OVERDUE", "DUE SOON", "Personal / groceries", "Work / standup"}; !reflect.DeepEqual(got, want) {
		t.Errorf("headers = %v, want %v", got, want)
	}

	// Overdue: send invoice (due 09-27, before today 09-29).
	var overdueGroup []string
	for _, it := range m.items {
		if it.kind == kindTask && it.group == groupOverdue {
			overdueGroup = append(overdueGroup, it.ref.Task.Text)
		}
	}
	if want := []string{"send invoice @2026-09-27"}; !reflect.DeepEqual(overdueGroup, want) {
		t.Errorf("overdue = %v, want %v", overdueGroup, want)
	}

	// "fix login bug" is due 2026-10-01, within the 7-day due-soon window
	// from 2026-09-29, so it should be grouped under Due soon rather than
	// its note. Re-run with a task actually inside the window to confirm.
}

func TestDueSoonWindow(t *testing.T) {
	refs := []index.TaskRef{
		ref("a.md", "A", 0, false, "in window @2026-10-04"),   // today+5, within 7
		ref("a.md", "A", 1, false, "at boundary @2026-10-06"), // today+7, inclusive boundary
		ref("a.md", "A", 2, false, "past window @2026-10-10"), // today+11, outside
		ref("a.md", "A", 3, false, "today itself @2026-09-29"),
		ref("a.md", "A", 4, false, "yesterday @2026-09-28"), // overdue
	}
	m := newTest(t)
	m = m.SetTasks(refs, testToday())

	group := map[string]groupKind{}
	for _, it := range m.items {
		if it.kind == kindTask {
			group[it.ref.Task.Text] = it.group
		}
	}
	cases := []struct {
		text string
		want groupKind
	}{
		{"in window @2026-10-04", groupDueSoon},
		{"at boundary @2026-10-06", groupDueSoon},
		{"past window @2026-10-10", groupNote},
		{"today itself @2026-09-29", groupDueSoon},
		{"yesterday @2026-09-28", groupOverdue},
	}
	for _, c := range cases {
		if got := group[c.text]; got != c.want {
			t.Errorf("group(%q) = %v, want %v", c.text, got, c.want)
		}
	}
}

func TestSortingWithinDueGroups(t *testing.T) {
	refs := []index.TaskRef{
		// Two tasks with the same overdue due date: sorted by path then line.
		ref("b.md", "B", 5, false, "b task @2026-09-01"),
		ref("a.md", "A", 2, false, "a task @2026-09-01"),
		ref("a.md", "A", 0, false, "earlier due @2026-08-01"),
	}
	m := newTest(t)
	m = m.SetTasks(refs, testToday())

	var got []string
	for _, it := range m.items {
		if it.kind == kindTask {
			got = append(got, it.ref.Task.Text)
		}
	}
	want := []string{"earlier due @2026-08-01", "a task @2026-09-01", "b task @2026-09-01"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("overdue order = %v, want %v", got, want)
	}
}

func TestNotesAlphabetical(t *testing.T) {
	refs := []index.TaskRef{
		ref("z.md", "zeta", 0, false, "z task"),
		ref("a.md", "alpha", 0, false, "a task"),
		ref("Folder/m.md", "mid", 0, false, "m task"),
	}
	m := newTest(t)
	m = m.SetTasks(refs, testToday())

	got := headers(m)
	want := []string{"alpha", "Folder / mid", "zeta"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("note headers = %v, want %v", got, want)
	}
}

func TestNoteDoneTotalCount(t *testing.T) {
	refs := []index.TaskRef{
		ref("Work/standup.md", "standup", 0, false, "open one"),
		ref("Work/standup.md", "standup", 1, true, "done one"),
		ref("Work/standup.md", "standup", 2, true, "done two"),
	}
	m := newTest(t)
	m = m.SetTasks(refs, testToday())

	var got item
	for _, it := range m.items {
		if it.kind == kindHeader && it.group == groupNote {
			got = it
		}
	}
	if got.done != 2 || got.total != 3 {
		t.Errorf("done/total = %d/%d, want 2/3", got.done, got.total)
	}
}

func TestShowDoneToggle(t *testing.T) {
	refs := []index.TaskRef{
		ref("a.md", "A", 0, false, "open"),
		ref("a.md", "A", 1, true, "done"),
	}
	m := newTest(t)
	m = m.SetTasks(refs, testToday())

	if got := tasksIn(m); len(got) != 1 {
		t.Fatalf("with show_done=false, tasks = %v, want 1 open task", got)
	}

	m = m.SetShowDone(true)
	if got := tasksIn(m); len(got) != 2 {
		t.Fatalf("with show_done=true, tasks = %v, want 2 tasks", got)
	}

	m = m.SetShowDone(false)
	if got := tasksIn(m); len(got) != 1 {
		t.Fatalf("after disabling show_done, tasks = %v, want 1 open task", got)
	}
}

func TestTitleCountsOpenOnly(t *testing.T) {
	refs := []index.TaskRef{
		ref("a.md", "A", 0, false, "open 1"),
		ref("a.md", "A", 1, false, "open 2"),
		ref("a.md", "A", 2, true, "done"),
	}
	m := newTest(t)
	m = m.SetTasks(refs, testToday())
	if got, want := m.Title(), "Tasks · 2 open"; got != want {
		t.Errorf("Title() = %q, want %q", got, want)
	}

	m = m.SetShowDone(true)
	if got, want := m.Title(), "Tasks · 2 open"; got != want {
		t.Errorf("Title() after show_done = %q, want %q (should stay open-only)", got, want)
	}
}

func TestSelectionStabilityAcrossSetTasks(t *testing.T) {
	refs := []index.TaskRef{
		ref("a.md", "A", 0, false, "first"),
		ref("b.md", "B", 0, false, "second"),
		ref("c.md", "C", 0, false, "third"),
	}
	m := newTest(t)
	m = m.SetTasks(refs, testToday())
	m.move(1) // move onto "second" (b.md)
	want := selectedID(m)
	if !strings.Contains(want, "second") {
		t.Fatalf("expected selection on 'second', got id %q", want)
	}

	// Rebuild with the same refs in a different order plus a new one: the
	// selection should stay on the same (Path, Raw) task.
	refs2 := []index.TaskRef{
		ref("c.md", "C", 0, false, "third"),
		ref("a.md", "A", 0, false, "first"),
		ref("b.md", "B", 0, false, "second"),
		ref("d.md", "D", 0, false, "fourth"),
	}
	m = m.SetTasks(refs2, testToday())
	if got := selectedID(m); got != want {
		t.Errorf("selection after SetTasks = %q, want %q", got, want)
	}

	// Now rebuild without "second": selection must move elsewhere without
	// panicking, and land on a real task.
	refs3 := []index.TaskRef{
		ref("a.md", "A", 0, false, "first"),
		ref("c.md", "C", 0, false, "third"),
	}
	m = m.SetTasks(refs3, testToday())
	if _, ok := m.selected(); !ok {
		t.Error("expected a selection to remain after the selected task vanished")
	}
}

func send(m Model, k tea.KeyPressMsg) (Model, tea.Msg) {
	var cmd tea.Cmd
	m, cmd = m.Update(k)
	if cmd == nil {
		return m, nil
	}
	return m, cmd()
}

func key(s string) tea.KeyPressMsg {
	switch s {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "space":
		return tea.KeyPressMsg{Code: tea.KeySpace}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	}
	return tea.KeyPressMsg{Code: rune(s[0]), Text: s}
}

func TestKeyMessages(t *testing.T) {
	refs := []index.TaskRef{
		ref("a.md", "A", 3, false, "first task"),
		ref("b.md", "B", 5, false, "second task"),
	}
	tests := []struct {
		name string
		key  string
		want tea.Msg
	}{
		{"toggle", "space", msgs.ToggleTaskMsg{Path: "a.md", Line: 3, Text: "- [ ] first task"}},
		{"open", "enter", msgs.OpenNoteMsg{Path: "a.md", Line: 3}},
		{"focus sidebar", "tab", msgs.FocusSidebarMsg{}},
		{"back", "esc", BackMsg{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newTest(t)
			m = m.SetTasks(refs, testToday())
			_, got := send(m, key(tt.key))
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("key %q = %#v, want %#v", tt.key, got, tt.want)
			}
		})
	}
}

func TestMoveKeysSkipHeaders(t *testing.T) {
	refs := []index.TaskRef{
		ref("a.md", "A", 0, false, "first"),
		ref("b.md", "B", 0, false, "second"),
	}
	m := newTest(t)
	m = m.SetTasks(refs, testToday())

	m, _ = send(m, key("j"))
	if got := selectedID(m); !strings.Contains(got, "second") {
		t.Errorf("after j, selection = %q, want to contain 'second'", got)
	}
	m, _ = send(m, key("k"))
	if got := selectedID(m); !strings.Contains(got, "first") {
		t.Errorf("after k, selection = %q, want to contain 'first'", got)
	}
	m, _ = send(m, key("G"))
	if got := selectedID(m); !strings.Contains(got, "second") {
		t.Errorf("after G, selection = %q, want to contain 'second'", got)
	}
	m, _ = send(m, key("g"))
	if got := selectedID(m); !strings.Contains(got, "first") {
		t.Errorf("after g, selection = %q, want to contain 'first'", got)
	}
}

func TestEmptyState(t *testing.T) {
	m := newTest(t)
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "No open tasks") {
		t.Errorf("empty view = %q, want it to contain 'No open tasks'", view)
	}
	lines := strings.Split(view, "\n")
	if len(lines) != m.height {
		t.Errorf("empty view has %d lines, want %d", len(lines), m.height)
	}
}

func TestViewDimensions(t *testing.T) {
	refs := []index.TaskRef{
		ref("Work/admin.md", "admin", 0, false, "send invoice @2026-09-27"),
		ref("Work/standup.md", "standup", 0, false, "follow up with #design"),
		ref("Work/standup.md", "standup", 1, false, "fix login bug"),
		ref("Personal/groceries.md", "groceries", 0, false, "oat milk"),
	}
	m := New(testStyles(t), 7, false)
	m = m.SetSize(44, 12)
	m = m.SetTasks(refs, testToday())

	view := m.View()
	lines := strings.Split(view, "\n")
	if len(lines) != 12 {
		t.Fatalf("View() has %d lines, want 12", len(lines))
	}
	for i, l := range lines {
		if w := ansi.StringWidth(l); w != 44 {
			t.Errorf("line %d width = %d, want 44 (line: %q)", i, w, ansi.Strip(l))
		}
	}
}

func TestRenderSnapshot(t *testing.T) {
	refs := []index.TaskRef{
		ref("Work/admin.md", "admin", 0, false, "send invoice @2026-09-27"),
		ref("Work/standup.md", "standup", 0, false, "follow up with #design"),
		ref("Work/standup.md", "standup", 1, false, "fix login bug @2026-10-01"),
		ref("Personal/groceries.md", "groceries", 0, false, "oat milk"),
	}
	m := New(testStyles(t), 7, false)
	m = m.SetSize(44, 10)
	m = m.SetTasks(refs, testToday())

	got := ansi.Strip(m.View())
	lines := strings.Split(got, "\n")
	if len(lines) != 10 {
		t.Fatalf("got %d lines, want 10:\n%s", len(lines), got)
	}
	if !strings.Contains(lines[0], "OVERDUE") {
		t.Errorf("line 0 = %q, want OVERDUE header", lines[0])
	}
	if !strings.Contains(lines[1], "send invoice") || !strings.Contains(lines[1], "@09-27") || !strings.Contains(lines[1], "Work/admin") {
		t.Errorf("line 1 = %q, want the overdue task row", lines[1])
	}
}

func TestLongTextTruncatesWithEllipsis(t *testing.T) {
	refs := []index.TaskRef{
		ref("a.md", "A", 0, false, strings.Repeat("word ", 30)),
	}
	m := New(testStyles(t), 7, false)
	m = m.SetSize(20, 5)
	m = m.SetTasks(refs, testToday())
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "…") {
		t.Errorf("expected truncation ellipsis in view:\n%s", view)
	}
	for _, l := range strings.Split(view, "\n") {
		if w := ansi.StringWidth(l); w != 20 {
			t.Errorf("line width = %d, want 20 (line: %q)", w, l)
		}
	}
}

func TestDoneRowStrikethroughHiddenByDefault(t *testing.T) {
	refs := []index.TaskRef{
		ref("a.md", "A", 0, true, "finished task"),
	}
	m := newTest(t)
	m = m.SetTasks(refs, testToday())
	view := ansi.Strip(m.View())
	if strings.Contains(view, "finished task") {
		t.Errorf("done task should be hidden by default:\n%s", view)
	}

	m = m.SetShowDone(true)
	view = ansi.Strip(m.View())
	if !strings.Contains(view, "finished task") {
		t.Errorf("done task should show with show_done=true:\n%s", view)
	}
	if !strings.Contains(view, glyphDone) {
		t.Errorf("expected done glyph in view:\n%s", view)
	}
}

// TestDateOnlyLocalCalendarDate confirms dateOnly reads "today" as a local
// calendar date even when it is passed in another location (e.g. UTC),
// matching how tasks.DueDate always parses due dates in time.Local.
func TestDateOnlyLocalCalendarDate(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skip("timezone database not available:", err)
	}
	orig := time.Local
	time.Local = loc
	defer func() { time.Local = orig }()

	// 2026-09-29 02:00 UTC is 2026-09-28 22:00 EDT (America/New_York,
	// UTC-4 in September): a UTC timestamp just after local midnight,
	// which is still the previous local calendar day.
	utcToday := time.Date(2026, 9, 29, 2, 0, 0, 0, time.UTC)
	got := dateOnly(utcToday)
	want := time.Date(2026, 9, 28, 0, 0, 0, 0, loc)
	if !got.Equal(want) {
		t.Errorf("dateOnly(%v) = %v, want %v", utcToday, got, want)
	}

	// End-to-end: a task due "today" (local) must land in Due soon, not
	// Overdue or Note, even though "today" arrives in UTC.
	refs := []index.TaskRef{
		ref("a.md", "A", 0, false, "due local today @2026-09-28"),
	}
	m := newTest(t)
	m = m.SetTasks(refs, utcToday)
	var got2 groupKind
	for _, it := range m.items {
		if it.kind == kindTask {
			got2 = it.group
		}
	}
	if got2 != groupDueSoon {
		t.Errorf("group = %v, want groupDueSoon (task due local 'today')", got2)
	}
}

// TestSelIDClearedWhenSelectionVanishes checks that once no task remains
// selectable, the stored selection id is cleared rather than left pointing
// at a task that no longer exists.
func TestSelIDClearedWhenSelectionVanishes(t *testing.T) {
	refs := []index.TaskRef{
		ref("a.md", "A", 0, false, "only task"),
	}
	m := newTest(t)
	m = m.SetTasks(refs, testToday())
	if selectedID(m) == "" {
		t.Fatal("expected an initial selection")
	}

	m = m.SetTasks(nil, testToday())
	if _, ok := m.selected(); ok {
		t.Fatal("expected no selection once the task list is empty")
	}
	if m.selID != "" {
		t.Errorf("selID = %q, want empty after the selection vanished", m.selID)
	}

	// A task reappearing afterwards must not be treated as "still
	// selected" just because selID happens to be empty already.
	m = m.SetTasks(refs, testToday())
	if got := selectedID(m); got == "" {
		t.Error("expected a fresh selection once a task reappears")
	}
}

func TestDisplayPathGuardsShortPaths(t *testing.T) {
	tests := []struct{ in, want string }{
		{"Work/admin.md", "Work/admin"},
		{"Work/admin.MD", "Work/admin"},
		{".md", ".md"},
		{"md", "md"},
		{"", ""},
		{"a.md", "a"},
	}
	for _, tt := range tests {
		if got := displayPath(tt.in); got != tt.want {
			t.Errorf("displayPath(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
