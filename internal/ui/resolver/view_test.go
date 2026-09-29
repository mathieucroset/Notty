package resolver

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/mathieucroset/notty/internal/gitsync"
)

// checkSize fails unless the view is exactly w×h cells.
func checkSize(t *testing.T, m Model, w, h int) {
	t.Helper()
	rows := strings.Split(m.View(), "\n")
	if len(rows) != h {
		t.Fatalf("view has %d rows, want %d", len(rows), h)
	}
	for i, r := range rows {
		if got := ansi.StringWidth(r); got != w {
			t.Errorf("row %d is %d cells wide, want %d: %q", i, got, w, ansi.Strip(r))
		}
	}
}

// plain returns the view without ANSI codes.
func plain(m Model) string { return ansi.Strip(m.View()) }

func allKinds() []File {
	return []File{
		textFile("notes/meeting.md", base2, ours2, theirs2),
		textFile("a/very/long/path/that/does/not/fit/in/the/list/n.md", noBase, "a\tb\x1b[31m\n", "c\n"),
		{Path: "att/x.bin", Kind: Binary, Ours: []byte{0}, Theirs: []byte{1}},
		{Path: "n.md", Kind: ModifyDelete, Ours: []byte("x\n"), Deleted: "theirs"},
		{Path: "Work/n.md", Kind: PathConflict, PathKind: gitsync.RenameRename, Original: "n.md", OursPath: "Work/n.md", TheirsPath: "Home/n.md"},
		{Path: "done.md", Kind: Text, Resolved: true},
	}
}

func TestViewExactSize(t *testing.T) {
	sizes := [][2]int{{120, 40}, {80, 24}, {100, 30}, {99, 30}, {60, 12}, {39, 10}, {20, 3}, {10, 2}, {5, 1}}
	for _, sz := range sizes {
		w, h := sz[0], sz[1]
		m := newModel(t, w, h, allKinds()...)
		for i := range len(m.items) {
			m.sel = i
			checkSize(t, m, w, h)
		}
		m = m.SetError("notes/meeting.md", "a long error message that will not fit in the file list at all")
		m.sel = 0
		checkSize(t, m, w, h)
		m, _ = press(t, m, "e")
		checkSize(t, m, w, h)
	}
}

func TestViewContent120(t *testing.T) {
	m := newModel(t, 120, 40, allKinds()...)
	v := plain(m)
	for _, want := range []string{
		"Resolve conflicts", "1 of 6 files resolved", "Files",
		"notes/meeting.md", "✓", "●", "Yours", "Theirs", "Result",
		"]c/[c", "esc close",
	} {
		if !strings.Contains(v, want) {
			t.Errorf("view lacks %q", want)
		}
	}
	if strings.Contains(m.View(), "\x1b[31m") {
		t.Error("file content escape sequence reached the terminal")
	}
}

func TestNarrowTabCycling(t *testing.T) {
	m := newModel(t, 80, 24, textFile("n.md", base1, ours1, theirs1))
	title := func(m Model) string {
		rows := strings.Split(plain(m), "\n")
		return rows[2] // title, file header, column title
	}
	shown := func(m Model) string {
		return strings.Fields(strings.SplitN(title(m), "│", 2)[1])[0]
	}
	if got := shown(m); got != "Result" {
		t.Fatalf("narrow view starts on %q, want Result", got)
	}
	if strings.Count(title(m), "│") != 1 {
		t.Fatalf("narrow view shows more than one column: %q", title(m))
	}
	for _, s := range []struct{ col, line string }{{"Yours", "X"}, {"Theirs", "Y"}, {"Result", unresolvedPlaceholder}} {
		m, _ = press(t, m, "tab")
		if got := shown(m); got != s.col {
			t.Fatalf("after tab showing %q, want %s", got, s.col)
		}
		if !strings.Contains(plain(m), "▌ "+s.line) {
			t.Errorf("%s column does not show %q", s.col, s.line)
		}
		checkSize(t, m, 80, 24)
	}
	// Wide terminals ignore tab.
	w := newModel(t, 120, 40, textFile("n.md", base1, ours1, theirs1))
	if w2, _ := press(t, w, "tab"); w2.narrow != w.narrow {
		t.Error("tab changed the column on a wide terminal")
	}
}

func TestErrorShownInView(t *testing.T) {
	m := newModel(t, 120, 40, textFile("a.md", base1, ours1, theirs1), textFile("b.md", base1, ours1, theirs1))
	m = m.SetError("b.md", "write failed: disk full")
	if !strings.Contains(plain(m), "✗ write failed") {
		t.Error("error not shown under the file in the list")
	}
	m, _ = press(t, m, "j")
	if !strings.Contains(plain(m), "✗ write failed: disk full") {
		t.Error("error not shown in the file's pane")
	}
	m = m.SetError("b.md", "")
	if strings.Contains(plain(m), "write failed") {
		t.Error("empty SetError did not clear the error")
	}
}

func TestTruncateLeftAndSanitize(t *testing.T) {
	if got := truncateLeft("abcdef", 4); got != "…def" {
		t.Errorf("truncateLeft = %q", got)
	}
	if got := truncateLeft("abc", 4); got != "abc" {
		t.Errorf("truncateLeft = %q", got)
	}
	if got := sanitize("a\tb\x1b[0m\r"); got != "a    b·[0m" {
		t.Errorf("sanitize = %q", got)
	}
}
