package sidebar

import (
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/mathieucroset/notty/internal/ui/msgs"
	"github.com/mathieucroset/notty/internal/ui/theme"
	"github.com/mathieucroset/notty/internal/vault"
)

func testStyles(t *testing.T) theme.Styles {
	t.Helper()
	p, ok := theme.Get("catppuccin-mocha")
	if !ok {
		t.Fatal("palette missing")
	}
	return theme.NewStyles(p)
}

func dir(name, p string, children ...*vault.Node) *vault.Node {
	return &vault.Node{Name: name, Path: p, IsDir: true, Children: children}
}

func note(name, p string) *vault.Node {
	return &vault.Node{Name: name, Path: p, IsNote: true}
}

func file(name, p string) *vault.Node {
	return &vault.Node{Name: name, Path: p}
}

// testTree:
//
//	Personal/journal.md
//	Work/Sub/deep.md
//	Work/Standup notes.md
//	Work/ideas.md
//	diagram.png
//	readme.md
func testTree() *vault.Node {
	return dir("vault", "",
		dir("Personal", "Personal", note("journal.md", "Personal/journal.md")),
		dir("Work", "Work",
			dir("Sub", "Work/Sub", note("deep.md", "Work/Sub/deep.md")),
			note("Standup notes.md", "Work/Standup notes.md"),
			note("ideas.md", "Work/ideas.md"),
		),
		file("diagram.png", "diagram.png"),
		note("readme.md", "readme.md"),
	)
}

func newTest(t *testing.T) Model {
	t.Helper()
	m := New(testStyles(t))
	m.SetSize(26, 30)
	m.SetTree(testTree())
	return m
}

func TestSelectedFolder(t *testing.T) {
	m := newTest(t)
	m.Select("Work/Sub/deep.md")
	if got := m.SelectedFolder(); got != "Work/Sub" {
		t.Errorf("SelectedFolder on a note = %q, want Work/Sub", got)
	}
	m.Select("Work")
	if got := m.SelectedFolder(); got != "Work" {
		t.Errorf("SelectedFolder on a folder = %q, want Work", got)
	}
	m.Select("readme.md")
	if got := m.SelectedFolder(); got != "" {
		t.Errorf("SelectedFolder at the root = %q, want \"\"", got)
	}
}

// nodePaths lists the tree rows currently visible, in order.
func nodePaths(m Model) []string {
	var out []string
	for _, it := range m.items {
		if it.kind == kindNode {
			out = append(out, it.path)
		}
	}
	return out
}

// selectedID returns the identity of the selected row, or "".
func selectedID(m Model) string {
	if m.cursor < 0 {
		return ""
	}
	return m.items[m.cursor].id()
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
	case "backspace":
		return tea.KeyPressMsg{Code: tea.KeyBackspace}
	}
	return tea.KeyPressMsg{Code: rune(s[0]), Text: s}
}

func send(m Model, keys ...string) (Model, tea.Msg) {
	var last tea.Msg
	for _, k := range keys {
		var cmd tea.Cmd
		m, cmd = m.Update(key(k))
		last = nil
		if cmd != nil {
			last = cmd()
		}
	}
	return m, last
}

func TestFlattenCollapsedFolders(t *testing.T) {
	tests := []struct {
		name     string
		expanded []string
		want     []string
	}{
		{"all collapsed", nil, []string{"Personal", "Work", "diagram.png", "readme.md"}},
		{"work expanded", []string{"Work"}, []string{"Personal", "Work", "Work/Sub", "Work/Standup notes.md", "Work/ideas.md", "diagram.png", "readme.md"}},
		{"nested expanded, parent collapsed", []string{"Work/Sub"}, []string{"Personal", "Work", "diagram.png", "readme.md"}},
		{"all expanded", []string{"Work", "Work/Sub", "Personal"}, []string{"Personal", "Personal/journal.md", "Work", "Work/Sub", "Work/Sub/deep.md", "Work/Standup notes.md", "Work/ideas.md", "diagram.png", "readme.md"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newTest(t)
			m.SetExpanded(tt.expanded)
			if got := nodePaths(m); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("rows = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCursorBoundsAndHeaderSkipping(t *testing.T) {
	m := newTest(t)
	m.SetPins([]string{"readme.md"})
	m.SetCounts(3, 0, 1)

	if got := selectedID(m); got != "pin:readme.md" {
		t.Fatalf("initial selection = %q, want the first pin", got)
	}
	m, _ = send(m, "k")
	if got := selectedID(m); got != "pin:readme.md" {
		t.Errorf("k at top moved to %q", got)
	}
	m, _ = send(m, "j")
	if got := selectedID(m); got != "node:Personal" {
		t.Errorf("j from the pin = %q, want node:Personal (skipping the header)", got)
	}
	m, _ = send(m, "up")
	if got := selectedID(m); got != "pin:readme.md" {
		t.Errorf("up = %q, want pin:readme.md", got)
	}
	for i := 0; i < 50; i++ {
		m, _ = send(m, "j")
	}
	if got := selectedID(m); got != "entry:trash" {
		t.Errorf("after many j = %q, want entry:trash", got)
	}
	m, _ = send(m, "k")
	if got := selectedID(m); got != "entry:tasks" {
		t.Errorf("k from trash = %q, want entry:tasks (conflicts hidden)", got)
	}
	m, _ = send(m, "g")
	if got := selectedID(m); got != "pin:readme.md" {
		t.Errorf("g = %q, want first row", got)
	}
	m, _ = send(m, "G")
	if got := selectedID(m); got != "entry:trash" {
		t.Errorf("G = %q, want last row", got)
	}
}

func TestSelectionSurvivesRebuild(t *testing.T) {
	m := newTest(t)
	m.Select("readme.md")
	m.SetPins([]string{"Work/ideas.md"})
	if got := selectedID(m); got != "node:readme.md" {
		t.Errorf("selection after SetPins = %q, want node:readme.md", got)
	}
}

func TestSelectRevealsNote(t *testing.T) {
	m := newTest(t)
	m.Select("Work/Sub/deep.md")
	if got := selectedID(m); got != "node:Work/Sub/deep.md" {
		t.Fatalf("selection = %q", got)
	}
	if got := m.Expanded(); !reflect.DeepEqual(got, []string{"Work", "Work/Sub"}) {
		t.Errorf("Expanded() = %v, want ancestors expanded", got)
	}
}

func TestSelectKeepsPinSelection(t *testing.T) {
	m := newTest(t)
	m.SetPins([]string{"Work/ideas.md"})
	m.selectID("pin:Work/ideas.md")
	m.Select("Work/ideas.md") // what the app does after opening the pin
	if got := selectedID(m); got != "pin:Work/ideas.md" {
		t.Errorf("selection = %q, want the pin to stay selected", got)
	}
	if got := m.Expanded(); len(got) != 0 {
		t.Errorf("Expanded() = %v, want no folders expanded", got)
	}
}

func TestFilterExpansionIsDisplayOnly(t *testing.T) {
	m := newTest(t)
	m.SetExpanded([]string{"Personal"})
	m.SetFilter(map[string]bool{"Work/Sub/deep.md": true})
	if got := nodePaths(m); !reflect.DeepEqual(got, []string{"Work", "Work/Sub", "Work/Sub/deep.md"}) {
		t.Errorf("filtered rows = %v", got)
	}
	if got := m.Expanded(); !reflect.DeepEqual(got, []string{"Personal"}) {
		t.Errorf("Expanded() during filter = %v, want [Personal]", got)
	}
	m.SetFilter(nil)
	want := []string{"Personal", "Personal/journal.md", "Work", "diagram.png", "readme.md"}
	if got := nodePaths(m); !reflect.DeepEqual(got, want) {
		t.Errorf("rows after clearing = %v, want %v", got, want)
	}
}

func TestFilterCollapseWorks(t *testing.T) {
	m := newTest(t)
	m.SetFilter(map[string]bool{"Work/Sub/deep.md": true})
	m.Select("Work")
	m, _ = send(m, "h")
	if got := nodePaths(m); !reflect.DeepEqual(got, []string{"Work"}) {
		t.Errorf("rows after collapsing a filter-expanded folder = %v", got)
	}
}

func TestUpdateDoesNotShareExpandedState(t *testing.T) {
	m := newTest(t)
	m.Select("Work")
	before := m
	after, _ := send(m, "l")
	if len(before.Expanded()) != 0 {
		t.Errorf("earlier Model value changed: Expanded() = %v", before.Expanded())
	}
	if got := after.Expanded(); !reflect.DeepEqual(got, []string{"Work"}) {
		t.Errorf("after l: Expanded() = %v", got)
	}
}

func TestFilterVisibility(t *testing.T) {
	m := newTest(t)
	m.SetFilter(map[string]bool{"Work/Sub/deep.md": true, "readme.md": true})
	want := []string{"Work", "Work/Sub", "Work/Sub/deep.md", "readme.md"}
	if got := nodePaths(m); !reflect.DeepEqual(got, want) {
		t.Errorf("filtered rows = %v, want %v", got, want)
	}
	m.SetFilter(nil)
	want = []string{"Personal", "Work", "diagram.png", "readme.md"}
	if got := nodePaths(m); !reflect.DeepEqual(got, want) {
		t.Errorf("unfiltered rows = %v, want %v", got, want)
	}
}

func TestSectionsAndEntries(t *testing.T) {
	tests := []struct {
		name            string
		pins            []string
		tags            []msgs.TagCount
		conflicts       int
		wantContains    []string
		wantNotContains []string
	}{
		{"minimal", nil, nil, 0, []string{"N O T E S", "Tasks (2)", "Trash (5)"}, []string{"P I N N E D", "T A G S", "Conflicts"}},
		{"everything", []string{"readme.md"}, []msgs.TagCount{{Tag: "design", Count: 3}}, 2, []string{"P I N N E D", "N O T E S", "T A G S", "#design 3", "⚠ Conflicts (2)"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newTest(t)
			m.SetPins(tt.pins)
			m.SetTags(tt.tags)
			m.SetCounts(2, tt.conflicts, 5)
			got := ansi.Strip(m.View())
			for _, s := range tt.wantContains {
				if !strings.Contains(got, s) {
					t.Errorf("view missing %q:\n%s", s, got)
				}
			}
			for _, s := range tt.wantNotContains {
				if strings.Contains(got, s) {
					t.Errorf("view should not contain %q:\n%s", s, got)
				}
			}
		})
	}
}

func TestHLCollapseExpand(t *testing.T) {
	m := newTest(t)
	m.Select("Work")
	m, _ = send(m, "l")
	if got := m.Expanded(); !reflect.DeepEqual(got, []string{"Work"}) {
		t.Fatalf("after l: Expanded() = %v", got)
	}
	m, _ = send(m, "j", "j") // Work/Sub, Work/Standup notes.md
	if got := selectedID(m); got != "node:Work/Standup notes.md" {
		t.Fatalf("selection = %q", got)
	}
	m, _ = send(m, "h")
	if got := selectedID(m); got != "node:Work" {
		t.Errorf("h on a note = %q, want its parent folder", got)
	}
	m, _ = send(m, "h")
	if got := m.Expanded(); len(got) != 0 {
		t.Errorf("h on an expanded folder: Expanded() = %v", got)
	}
	m, msg := send(m, "enter")
	if msg != nil {
		t.Errorf("enter on a folder emitted %#v", msg)
	}
	if got := m.Expanded(); !reflect.DeepEqual(got, []string{"Work"}) {
		t.Errorf("enter on a folder: Expanded() = %v", got)
	}
}

func TestKeyMessages(t *testing.T) {
	tests := []struct {
		name string
		sel  string // path to select, or an id like "entry:tasks"
		key  string
		want tea.Msg
	}{
		{"open note", "Work/ideas.md", "enter", msgs.OpenNoteMsg{Path: "Work/ideas.md", Line: -1}},
		{"open pin", "pin:readme.md", "enter", msgs.OpenNoteMsg{Path: "readme.md", Line: -1}},
		{"open file", "diagram.png", "enter", msgs.OpenFileExternalMsg{Path: "diagram.png"}},
		{"tag", "tag:design", "enter", msgs.FilterTagMsg{Tag: "design"}},
		{"tasks entry", "entry:tasks", "enter", msgs.ActivateEntryMsg{Entry: msgs.EntryTasks}},
		{"conflicts entry", "entry:conflicts", "enter", msgs.ActivateEntryMsg{Entry: msgs.EntryConflicts}},
		{"trash entry", "entry:trash", "enter", msgs.ActivateEntryMsg{Entry: msgs.EntryTrash}},
		{"new note in folder", "Work", "n", msgs.RequestNewNote{Folder: "Work"}},
		{"new note next to note", "Work/ideas.md", "n", msgs.RequestNewNote{Folder: "Work"}},
		{"new note at root", "readme.md", "n", msgs.RequestNewNote{Folder: ""}},
		{"new note from entry", "entry:tasks", "n", msgs.RequestNewNote{Folder: ""}},
		{"new folder", "Work/Sub", "N", msgs.RequestNewFolder{Parent: "Work/Sub"}},
		{"rename", "Work/ideas.md", "r", msgs.RequestRename{Path: "Work/ideas.md"}},
		{"rename folder", "Work", "r", msgs.RequestRename{Path: "Work"}},
		{"move", "Work/ideas.md", "m", msgs.RequestMove{Path: "Work/ideas.md"}},
		{"trash", "Work/ideas.md", "d", msgs.RequestTrash{Path: "Work/ideas.md"}},
		{"trash folder", "Personal", "d", msgs.RequestTrash{Path: "Personal"}},
		{"pin", "Work/ideas.md", "p", msgs.TogglePinMsg{Path: "Work/ideas.md"}},
		{"unpin", "pin:readme.md", "p", msgs.TogglePinMsg{Path: "readme.md"}},
		{"pin folder is no-op", "Work", "p", nil},
		{"history", "Work/ideas.md", "H", msgs.OpenHistoryMsg{Path: "Work/ideas.md"}},
		{"rename entry is no-op", "entry:trash", "r", nil},
		{"clear filter", "readme.md", "esc", msgs.ClearFilterMsg{}},
		{"resolver", "readme.md", "c", msgs.OpenResolverMsg{}},
		{"error log", "readme.md", "!", msgs.OpenErrorLogMsg{}},
		{"help", "readme.md", "?", msgs.OpenHelpMsg{}},
		{"quit", "readme.md", "q", msgs.QuitMsg{}},
		{"focus main", "readme.md", "tab", msgs.FocusMainMsg{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newTest(t)
			m.SetPins([]string{"readme.md"})
			m.SetTags([]msgs.TagCount{{Tag: "design", Count: 3}, {Tag: "todo", Count: 7}})
			m.SetCounts(1, 2, 3)
			m.SetExpanded([]string{"Work"})
			if strings.Contains(tt.sel, ":") {
				m.selectID(tt.sel)
			} else {
				m.Select(tt.sel)
			}
			if got := selectedID(m); got == "" || (!strings.Contains(tt.sel, ":") && !strings.HasSuffix(got, ":"+tt.sel)) || (strings.Contains(tt.sel, ":") && got != tt.sel) {
				t.Fatalf("could not select %q (selected %q)", tt.sel, got)
			}
			_, got := send(m, tt.key)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("key %q on %q = %#v, want %#v", tt.key, tt.sel, got, tt.want)
			}
		})
	}
}

func TestTagPicker(t *testing.T) {
	newPicker := func() Model {
		m := newTest(t)
		m.SetTags([]msgs.TagCount{{Tag: "design", Count: 3}, {Tag: "todo", Count: 7}, {Tag: "quarterly", Count: 1}})
		return m
	}

	m, msg := send(newPicker(), "#")
	if msg != nil || !m.PickerOpen() {
		t.Fatalf("# should open the picker silently (open=%v, msg=%#v)", m.PickerOpen(), msg)
	}
	if !strings.Contains(ansi.Strip(m.View()), "#todo") {
		t.Errorf("picker does not list tags:\n%s", ansi.Strip(m.View()))
	}

	m, msg = send(newPicker(), "#", "t", "o", "enter")
	if want := (msgs.FilterTagMsg{Tag: "todo"}); msg != want {
		t.Errorf("picker enter = %#v, want %#v", msg, want)
	}
	if m.PickerOpen() {
		t.Error("picker still open after enter")
	}

	// q is typed into the query rather than quitting.
	m, msg = send(newPicker(), "#", "q")
	if msg != nil || !m.PickerOpen() {
		t.Errorf("q in the picker: open=%v msg=%#v", m.PickerOpen(), msg)
	}
	_, msg = send(m, "enter")
	if want := (msgs.FilterTagMsg{Tag: "quarterly"}); msg != want {
		t.Errorf("picker enter = %#v, want %#v", msg, want)
	}

	// Moving down picks the second match; backspace edits the query.
	_, msg = send(newPicker(), "#", "x", "backspace", "down", "enter")
	if want := (msgs.FilterTagMsg{Tag: "todo"}); msg != want {
		t.Errorf("picker down+enter = %#v, want %#v", msg, want)
	}

	// esc cancels without clearing the filter.
	m, msg = send(newPicker(), "#", "d", "esc")
	if msg != nil || m.PickerOpen() {
		t.Errorf("esc in the picker: open=%v msg=%#v", m.PickerOpen(), msg)
	}

	// Spaces are ignored (tags never contain them).
	_, msg = send(newPicker(), "#", " ", "t", " ", "o", "enter")
	if want := (msgs.FilterTagMsg{Tag: "todo"}); msg != want {
		t.Errorf("picker with spaces = %#v, want %#v", msg, want)
	}

	// Backspace on an empty query closes the picker.
	m, msg = send(newPicker(), "#", "backspace")
	if msg != nil || m.PickerOpen() {
		t.Errorf("backspace on empty query: open=%v msg=%#v", m.PickerOpen(), msg)
	}

	// No match: enter does nothing but close.
	m, msg = send(newPicker(), "#", "z", "z", "enter")
	if msg != nil || m.PickerOpen() {
		t.Errorf("enter with no match: open=%v msg=%#v", m.PickerOpen(), msg)
	}
}

func TestScrollKeepsCursorVisible(t *testing.T) {
	m := newTest(t)
	m.SetExpanded([]string{"Work", "Work/Sub", "Personal"})
	m.SetCounts(1, 1, 1)
	m.SetSize(26, 5)
	for i := 0; i < 30; i++ {
		m, _ = send(m, "j")
		view := ansi.Strip(m.View())
		if lines := strings.Split(view, "\n"); len(lines) != 5 {
			t.Fatalf("view has %d lines, want 5", len(lines))
		}
		line := m.lineOf[m.cursor]
		if line < m.offset || line >= m.offset+5 {
			t.Fatalf("cursor line %d outside viewport [%d,%d)", line, m.offset, m.offset+5)
		}
	}
	if !strings.Contains(ansi.Strip(m.View()), "Trash (1)") {
		t.Errorf("bottom entry not visible after scrolling:\n%s", ansi.Strip(m.View()))
	}
	m, _ = send(m, "g")
	if !strings.Contains(ansi.Strip(m.View()), "N O T E S") {
		t.Errorf("header not visible after g:\n%s", ansi.Strip(m.View()))
	}
}

func TestDirtyMarker(t *testing.T) {
	m := newTest(t)
	m.SetExpanded([]string{"Work"})
	m.SetDirty("Work/ideas.md")
	view := ansi.Strip(m.View())
	var found bool
	for _, line := range strings.Split(view, "\n") {
		if strings.Contains(line, "ideas") {
			found = strings.Contains(line, "●")
		}
	}
	if !found {
		t.Errorf("dirty marker missing:\n%s", view)
	}
}

func TestEmptyTree(t *testing.T) {
	m := New(testStyles(t))
	m.SetSize(26, 10)
	m.SetTree(dir("vault", ""))
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "No notes yet") {
		t.Errorf("empty tree hint missing:\n%s", view)
	}
	_, msg := send(m, "n")
	if want := (msgs.RequestNewNote{}); msg != want {
		t.Errorf("n on an empty tree = %#v, want %#v", msg, want)
	}
}

func TestRenderSnapshot(t *testing.T) {
	m := New(testStyles(t))
	m.SetSize(28, 20)
	m.SetTree(testTree())
	m.SetPins([]string{"Work/Standup notes.md"})
	m.SetTags([]msgs.TagCount{{Tag: "design", Count: 3}, {Tag: "todo", Count: 7}, {Tag: "work/client", Count: 12}})
	m.SetCounts(12, 2, 2)
	m.SetExpanded([]string{"Work"})
	m.SetDirty("Work/Standup notes.md")
	m.Select("Work/Standup notes.md")
	m.SetFocused(true)

	want := strings.Join([]string{
		"  P I N N E D               ",
		"  ★ Standup notes ●         ",
		"                            ",
		"  N O T E S                 ",
		"  ▸ Personal                ",
		"  ▾ Work                    ",
		"    ▸ Sub                   ",
		"    • Standup notes ●       ",
		"    • ideas                 ",
		"  · diagram.png             ",
		"  • readme                  ",
		"                            ",
		"  T A G S                   ",
		"   #design 3   #todo 7      ",
		"   #work/client 12          ",
		"                            ",
		"  ☐ Tasks (12)              ",
		"  ⚠ Conflicts (2)           ",
		"  ⌫ Trash (2)               ",
		"                            ",
	}, "\n")
	got := ansi.Strip(m.View())
	if got != want {
		t.Errorf("snapshot mismatch\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
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
	m := newTest(t)
	before := m.View()
	m.SetStyles(latteStyles(t))
	after := m.View()
	if before == after {
		t.Error("SetStyles did not change the rendering")
	}
	if ansi.Strip(before) != ansi.Strip(after) {
		t.Error("SetStyles changed the content")
	}
}
