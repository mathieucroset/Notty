package preview

import (
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/mathieucroset/notty/internal/imgrender"
	"github.com/mathieucroset/notty/internal/ui/msgs"
)

func key(s string) tea.KeyPressMsg {
	switch s {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "space":
		return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	case "ctrl+d":
		return tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl}
	case "ctrl+u":
		return tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl}
	}
	return tea.KeyPressMsg{Code: rune(s[0]), Text: s}
}

// press sends keys (each key a single rune or a named key) and returns the
// messages the last one emitted.
func press(m Model, keys ...string) (Model, []tea.Msg) {
	var out []tea.Msg
	for _, k := range keys {
		var cmd tea.Cmd
		m, cmd = m.Update(key(k))
		m, _, out = run(m, cmd)
	}
	return m, out
}

func longNote(n int) string {
	var b strings.Builder
	for i := range n {
		if i > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString("paragraph " + strconv.Itoa(i))
	}
	return b.String()
}

func TestScrollKeys(t *testing.T) {
	m := newTest(t, imgrender.ProtoOff, t.TempDir()).SetSize(40, 10)
	m, _ = setContent(t, m, "n.md", longNote(20)) // 20 rows + 19 separators
	total := m.totalLines()
	if total != 39 {
		t.Fatalf("total lines %d, want 39", total)
	}
	steps := []struct {
		key  string
		want int
	}{
		{"k", 0}, {"j", 1}, {"j", 2}, {"k", 1},
		{"ctrl+d", 6}, {"ctrl+u", 1}, {"ctrl+u", 0},
		{"G", total - 10}, {"j", total - 10}, {"ctrl+d", total - 10},
	}
	for _, s := range steps {
		m, _ = press(m, s.key)
		if m.offset != s.want {
			t.Fatalf("after %q: offset %d, want %d", s.key, m.offset, s.want)
		}
	}
	m, _ = press(m, "g", "g")
	if m.offset != 0 {
		t.Fatalf("gg: offset %d, want 0", m.offset)
	}
	if lineOf(m.View(), "paragraph 0") != 0 {
		t.Fatal("top of the note not shown after gg")
	}
	// A lone g followed by another key does nothing.
	m, _ = press(m, "j", "g", "k")
	if m.offset != 0 {
		t.Fatalf("g k: offset %d, want 0", m.offset)
	}
}

const taskNote = "# Tasks\n" + // 0
	"\n" + // 1
	"- [ ] write **spec**\n" + // 2
	"- [x] ship it\n" + // 3
	"\n" + // 4
	"```\n" + // 5
	"- [ ] not a task\n" + // 6
	"```\n" + // 7
	"\n" + // 8
	"Some text.\n" + // 9
	"\n" + // 10
	"1. [ ] ship it\n" + // 11
	"   - [ ] nested `code` task" // 12

func TestTaskNavigationAndToggle(t *testing.T) {
	m := newTest(t, imgrender.ProtoOff, t.TempDir())
	m, _ = setContent(t, m, "work/n.md", taskNote)
	wantLines := []int{2, 3, 11, 12}
	wantText := []string{"write spec", "ship it", "ship it", "nested code task"}
	if len(m.doc.tasks) != len(wantLines) {
		t.Fatalf("%d tasks, want %d", len(m.doc.tasks), len(wantLines))
	}
	rendered := strings.Split(ansi.Strip(m.View()), "\n")
	seen := map[int]bool{}
	for i, line := range wantLines {
		m, _ = press(m, "]", "t")
		if m.taskIdx != i {
			t.Fatalf("]t #%d: taskIdx %d", i, m.taskIdx)
		}
		row := m.doc.taskRows[i]
		if seen[row] {
			t.Fatalf("task %d mapped to row %d twice", i, row)
		}
		seen[row] = true
		if !strings.Contains(normalize(rendered[row]), wantText[i]) {
			t.Fatalf("task %d marker row %d = %q, want it to contain %q", i, row, rendered[row], wantText[i])
		}
		v := strings.Split(ansi.Strip(m.View()), "\n")
		if !strings.HasPrefix(v[row-m.offset], "▸") {
			t.Fatalf("no marker on row %d: %q", row, v[row-m.offset])
		}
		var out []tea.Msg
		m, out = press(m, "space")
		want := msgs.ToggleTaskMsg{Path: "work/n.md", Line: line, Text: strings.Split(taskNote, "\n")[line]}
		if len(out) != 1 || out[0] != want {
			t.Fatalf("space on task %d emitted %#v, want %#v", i, out, want)
		}
	}
	m, _ = press(m, "]", "t")
	if m.taskIdx != 3 {
		t.Fatalf("]t past the end: taskIdx %d, want 3", m.taskIdx)
	}
	m, _ = press(m, "[", "t", "[", "t")
	if m.taskIdx != 1 {
		t.Fatalf("[t [t: taskIdx %d, want 1", m.taskIdx)
	}
	// The highlight survives a re-render of the toggled content.
	m, _ = setContent(t, m, "work/n.md", strings.Replace(taskNote, "- [x] ship", "- [ ] ship", 1))
	if m.taskIdx != 1 {
		t.Fatalf("after re-render taskIdx %d, want 1", m.taskIdx)
	}
}

func TestTaskMappingWrappedAndDuplicateTasks(t *testing.T) {
	m := newTest(t, imgrender.ProtoOff, t.TempDir()).SetSize(24, 20)
	content := "- [ ] same\n- [ ] a [linked](http://x.y) task whose text wraps over several rows\n- [ ] same"
	m, _ = setContent(t, m, "n.md", content)
	v := strings.Split(ansi.Strip(m.View()), "\n")
	rows := m.doc.taskRows
	if len(rows) != 3 || rows[0] >= rows[1] || rows[1] >= rows[2] {
		t.Fatalf("task rows %v not strictly increasing", rows)
	}
	for i, want := range []string{"☐ same", "☐ a linked", "☐ same"} {
		if !strings.Contains(v[rows[i]], want) {
			t.Fatalf("task %d row %d = %q, want %q", i, rows[i], v[rows[i]], want)
		}
	}
}

func TestSpaceIgnoredWhileRenderIsStale(t *testing.T) {
	m := newTest(t, imgrender.ProtoOff, t.TempDir())
	m, _ = setContent(t, m, "n.md", "- [ ] one\n- [ ] two")
	m, _ = press(m, "]", "t")
	edited := "- [ ] zero\n- [ ] one\n- [ ] two"
	m, cmd := m.SetContent("n.md", edited)
	if _, out := press(m, "space"); len(out) != 0 {
		t.Fatalf("space on a stale render emitted %v", out)
	}
	m, _, _ = run(m, cmd)
	_, out := press(m, "space")
	want := msgs.ToggleTaskMsg{Path: "n.md", Line: 0, Text: "- [ ] zero"}
	if len(out) != 1 || out[0] != want {
		t.Fatalf("space after the re-render emitted %v, want %v", out, want)
	}
	// A pending resize does not block the toggle.
	m = m.SetSize(50, 20)
	if _, out := press(m, "space"); len(out) != 1 {
		t.Fatalf("space during a pending resize emitted %v", out)
	}
}

func TestSpaceWithoutTaskDoesNothing(t *testing.T) {
	m := newTest(t, imgrender.ProtoOff, t.TempDir())
	m, _ = setContent(t, m, "n.md", "no tasks")
	if _, out := press(m, "space"); len(out) != 0 {
		t.Fatalf("space emitted %v", out)
	}
	if _, out := press(m, "]", "t", "space"); len(out) != 0 {
		t.Fatalf("]t space emitted %v", out)
	}
}

func TestTaskNavigationScrollsIntoView(t *testing.T) {
	m := newTest(t, imgrender.ProtoOff, t.TempDir()).SetSize(40, 6)
	m, _ = setContent(t, m, "n.md", longNote(10)+"\n\n- [ ] far away")
	m, _ = press(m, "]", "t")
	row := m.doc.taskRows[0]
	if row < m.offset || row >= m.offset+6 {
		t.Fatalf("task row %d not visible (offset %d)", row, m.offset)
	}
}

func TestImageNavigationAndViewer(t *testing.T) {
	vault := t.TempDir()
	writePNG(t, filepath.Join(vault, "notes", "a.png"), 32, 32)
	writePNG(t, filepath.Join(vault, "attachments", "b.png"), 32, 32)
	m := newTest(t, imgrender.ProtoHalfBlocks, vault)
	content := "![a](a.png)\n\n![web](https://x.org/w.png)\n\ntext ![b](/attachments/b.png)"
	m, _ = setContent(t, m, "notes/n.md", content)
	paths := []string{filepath.Join(vault, "notes", "a.png"), filepath.Join(vault, "attachments", "b.png")}

	m, _ = press(m, "]", "i")
	if m.imgIdx != 0 {
		t.Fatalf("]i: imgIdx %d, want 0", m.imgIdx)
	}
	v := strings.Split(ansi.Strip(m.View()), "\n")
	top := m.doc.imgTop[0] - m.offset
	if !strings.HasPrefix(v[top], "▌") {
		t.Fatalf("no image bar on the highlighted image: %q", v[top])
	}
	_, out := press(m, "enter")
	want := msgs.OpenImageViewerMsg{Paths: paths, Index: 0}
	if len(out) != 1 || !reflect.DeepEqual(out[0], want) {
		t.Fatalf("enter emitted %#v, want %#v", out, want)
	}

	// The external image can be highlighted but not opened.
	m, _ = press(m, "]", "i")
	if _, out := press(m, "enter"); len(out) != 0 {
		t.Fatalf("enter on an external image emitted %v", out)
	}
	m, _ = press(m, "]", "i", "]", "i")
	if m.imgIdx != 2 {
		t.Fatalf("imgIdx %d, want 2", m.imgIdx)
	}
	_, out = press(m, "enter")
	want = msgs.OpenImageViewerMsg{Paths: paths, Index: 1}
	if len(out) != 1 || !reflect.DeepEqual(out[0], want) {
		t.Fatalf("enter emitted %#v, want %#v", out, want)
	}
	m, _ = press(m, "[", "i", "[", "i", "[", "i")
	if m.imgIdx != 0 {
		t.Fatalf("[i x3: imgIdx %d, want 0", m.imgIdx)
	}
	// Selecting a task clears the image highlight.
	m, _ = setContent(t, m, "notes/n.md", content+"\n\n- [ ] t")
	m, _ = press(m, "]", "t")
	if m.imgIdx != -1 {
		t.Fatalf("imgIdx %d after ]t, want -1", m.imgIdx)
	}
}

func TestFocusAndHelpKeys(t *testing.T) {
	m := newTest(t, imgrender.ProtoOff, t.TempDir())
	if _, out := press(m, "tab"); len(out) != 1 || out[0] != (msgs.FocusSidebarMsg{}) {
		t.Fatalf("tab emitted %v", out)
	}
	if _, out := press(m, "?"); len(out) != 1 || out[0] != (msgs.OpenHelpMsg{}) {
		t.Fatalf("? emitted %v", out)
	}
}

func TestSplitModeIgnoresKeysAndFollowsLine(t *testing.T) {
	m := newTest(t, imgrender.ProtoOff, t.TempDir()).SetSize(40, 6).SetMode(ModeSplit)
	content := longNote(20)
	m, _ = setContent(t, m, "n.md", content)
	if m2, out := press(m, "j", "tab", "?"); len(out) != 0 || m2.offset != 0 {
		t.Fatalf("split mode reacted to keys: offset %d, out %v", m2.offset, out)
	}
	// "paragraph 15" is source line 30.
	m = m.FollowLine(30)
	if l := lineOf(m.View(), "paragraph 15"); l < 0 {
		t.Fatalf("FollowLine(30) does not show paragraph 15:\n%s", ansi.Strip(m.View()))
	}
	off := m.offset
	m = m.FollowLine(31) // blank line after it: already visible, no jump
	if m.offset != off {
		t.Fatalf("FollowLine on a visible segment scrolled from %d to %d", off, m.offset)
	}
	m = m.FollowLine(0)
	if lineOf(m.View(), "paragraph 0") < 0 {
		t.Fatal("FollowLine(0) does not show the top")
	}
}
