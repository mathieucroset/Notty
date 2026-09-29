package app

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/mathieucroset/notty/internal/ui/msgs"
)

func TestCtrlGCyclesNoteViews(t *testing.T) {
	m := openNote(t, testOptions(t), "ideas.md")
	want := []struct {
		view  NoteView
		title string
	}{
		{ViewSplit, "split"},
		{ViewPreview, "preview"},
		{ViewEditor, ""},
	}
	for _, w := range want {
		run(t, m, keyMsg("ctrl+g"))
		if m.NoteView() != w.view {
			t.Fatalf("view = %d, want %d", m.NoteView(), w.view)
		}
		top := strings.Split(screen(m), "\n")[0]
		if w.title != "" && !strings.Contains(top, w.title) {
			t.Errorf("pane title %q does not say %q", top, w.title)
		}
		assertSize(t, m, 120, 30)
	}
}

func TestSplitViewLayout(t *testing.T) {
	m := openNote(t, testOptions(t), "ideas.md")
	run(t, m, keyMsg("ctrl+g"))
	l := ComputeLayout(120, 30, true)
	ew, pw := splitWidths(l.Content.W)
	if ew+1+pw != l.Content.W || ew != 44 {
		t.Fatalf("split widths %d + 1 + %d for %d", ew, pw, l.Content.W)
	}
	assertSize(t, m, 120, 30)
	lines := strings.Split(screen(m), "\n")
	divX := l.Content.X + ew
	for y := l.Content.Y; y < l.Content.Y+l.Content.H; y++ {
		if got := ansi.Cut(lines[y], divX, divX+1); got != "│" {
			t.Fatalf("row %d: column %d is %q, want the divider", y, divX, got)
		}
	}
	// Editor on the left (raw markdown), rendered preview on the right.
	left := ansi.Cut(lines[1], l.Content.X, divX)
	right := ansi.Cut(strings.Join(lines, "\n"), 0, 120)
	if !strings.Contains(left, "# Ideas") {
		t.Errorf("editor side row 1 = %q", left)
	}
	var previewText strings.Builder
	for _, line := range lines[l.Content.Y : l.Content.Y+l.Content.H] {
		previewText.WriteString(ansi.Cut(line, divX+1, divX+1+pw) + "\n")
	}
	if !strings.Contains(previewText.String(), "A note app in the terminal.") {
		t.Errorf("preview side does not show the note:\n%s\n(full screen:\n%s)", previewText.String(), right)
	}
	// Focus and cursor stay in the editor.
	if c := m.View().Cursor; c == nil || c.X != l.Content.X+1 || c.Y != 1 {
		t.Errorf("cursor = %+v", c)
	}
	if got := m.keyContext().String(); got != "Editor · normal" {
		t.Errorf("split view context = %s", got)
	}
}

func TestSplitPreviewFollowsCursor(t *testing.T) {
	opts := testOptions(t)
	var b strings.Builder
	for i := range 40 {
		fmt.Fprintf(&b, "## Section %d\n\nParagraph number %d.\n\n", i, i)
	}
	writeFile(t, opts.Vault, "long.md", b.String())
	m := openNote(t, opts, "long.md")
	run(t, m, keyMsg("ctrl+g"))
	if !strings.Contains(screen(m), "Paragraph number 0.") {
		t.Fatalf("split preview does not start at the top:\n%s", screen(m))
	}
	run(t, m, keyMsg("G"))
	s := screen(m)
	if !strings.Contains(s, "Paragraph number 39.") || strings.Contains(s, "Paragraph number 0.") {
		t.Errorf("split preview did not follow the cursor to the end:\n%s", s)
	}
}

func TestPreviewRendersUnsavedBuffer(t *testing.T) {
	opts := testOptions(t)
	m := openNote(t, opts, "ideas.md")
	insertText(t, m, "Unsaved words here\n\n")
	run(t, m, keyMsg("ctrl+g"))
	run(t, m, keyMsg("ctrl+g"))
	if m.NoteView() != ViewPreview {
		t.Fatal("not in preview view")
	}
	if !strings.Contains(screen(m), "Unsaved words here") {
		t.Errorf("preview does not show the buffer:\n%s", screen(m))
	}
	if strings.Contains(readFile(t, opts.Vault, "ideas.md"), "Unsaved") {
		t.Error("the preview view saved the buffer")
	}
	// Typing in split view updates the preview side.
	run(t, m, keyMsg("ctrl+g"))
	run(t, m, keyMsg("ctrl+g"))
	insertText(t, m, "Fresh edit\n\n")
	if !strings.Contains(screen(m), "Fresh edit") {
		t.Errorf("split preview missed an edit:\n%s", screen(m))
	}
}

func TestPreviewViewTakesKeys(t *testing.T) {
	opts := testOptions(t)
	m := openNote(t, opts, "ideas.md")
	run(t, m, keyMsg("ctrl+g"))
	run(t, m, keyMsg("ctrl+g"))
	if got := m.keyContext().String(); got != "Preview" {
		t.Fatalf("context = %s", got)
	}
	if c := m.View().Cursor; c != nil {
		t.Errorf("preview view shows the editor cursor: %+v", c)
	}
	before := m.editor.Content()
	pressKeys(t, m, "x", "i", "d", "d")
	if m.editor.Content() != before {
		t.Error("keys in the preview view reached the editor")
	}
	run(t, m, keyMsg("tab"))
	if m.Focus() != FocusSidebar {
		t.Error("tab in the preview view did not focus the sidebar")
	}
}

func TestPreviewViewExactSizes(t *testing.T) {
	for _, size := range [][2]int{{120, 30}, {70, 20}, {160, 45}} {
		m := start(t, testOptions(t), size[0], size[1])
		run(t, m, msgs.OpenNoteMsg{Path: "Work/Standup notes.md", Line: -1})
		for range 3 {
			run(t, m, keyMsg("ctrl+g"))
			assertSize(t, m, size[0], size[1])
		}
		// Resizing in split view keeps the exact size.
		run(t, m, keyMsg("ctrl+g"))
		run(t, m, tea.WindowSizeMsg{Width: size[0] - 7, Height: size[1] - 3})
		assertSize(t, m, size[0]-7, size[1]-3)
	}
}

func TestReadyTicks(t *testing.T) {
	m := New(testOptions(t))
	run(t, m, tea.WindowSizeMsg{Width: 120, Height: 30})
	var ticks []readyTickMsg
	for _, msg := range execCmd(m.Init()) {
		if r, ok := msg.(readyTickMsg); ok {
			ticks = append(ticks, r)
		}
	}
	if len(ticks) != 1 || ticks[0].gen != 0 {
		t.Fatalf("Init ready ticks = %+v", ticks)
	}
	// Every tea.Exec starts a new generation and re-arms the tick.
	out := run(t, m, configEditedMsg{})
	found := false
	for _, msg := range out {
		if r, ok := msg.(readyTickMsg); ok && r.gen == 1 {
			found = true
		}
	}
	if !found || m.kittyGen != 1 {
		t.Errorf("no re-armed ready tick after an exec: %v", out)
	}
}
