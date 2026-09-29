package resolver

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/mathieucroset/notty/internal/ui/editor"
	"github.com/mathieucroset/notty/internal/ui/keys"
	"github.com/mathieucroset/notty/internal/ui/msgs"
)

// typeText presses each rune of s as a key.
func typeText(t *testing.T, m Model, s string) Model {
	t.Helper()
	for _, r := range s {
		m, _ = press(t, m, string(r))
	}
	return m
}

func TestEditPlainCtrlSAccepts(t *testing.T) {
	m := newModel(t, 120, 40, textFile("n.md", base2, ours2, theirs2))
	if m.CursorPosition() != nil {
		t.Error("cursor shown outside the edit sub-context")
	}
	m, _ = press(t, m, "e")
	if m.KeyContext() != keys.ResolverEdit || !m.Editing() {
		t.Fatal("e did not open the edit sub-context")
	}
	if got := m.ed.Content(); got != ours2 {
		t.Fatalf("editor opened with %q, want the result pre-filled with yours %q", got, ours2)
	}
	if !strings.Contains(plain(m), "pre-filled with yours") {
		t.Error("pre-fill note missing")
	}
	c := m.CursorPosition()
	if c == nil {
		t.Fatal("no cursor while editing")
	}
	lw, sw, _ := m.split()
	// The cursor starts on the current conflict: result line 1.
	if c.X < lw+sw || c.Y != 3+1 {
		t.Errorf("cursor at %d,%d, want x >= %d and y 4", c.X, c.Y, lw+sw)
	}
	// Keys such as o, t, q and j go to the editor, not the resolver.
	m = typeText(t, m, "Zq")
	if m.KeyContext() != keys.ResolverEdit {
		t.Fatal("typing left the edit sub-context")
	}
	m, out := press(t, m, "ctrl+s")
	if len(out) != 0 {
		t.Errorf("accept leaked messages to the app: %v", out)
	}
	if m.KeyContext() != keys.Resolver || m.CursorPosition() != nil {
		t.Fatal("ctrl+s did not return to navigation")
	}
	tx := m.items[0].text
	want := "1\nZqA\n3\nC\n5\n"
	if !tx.edited || tx.editedText != want || tx.resolvedCount() != 2 {
		t.Fatalf("edited=%v text=%q count=%d", tx.edited, tx.editedText, tx.resolvedCount())
	}
	if !strings.Contains(plain(m), "2 of 2 conflicts resolved · edited") {
		t.Errorf("header does not show the edit:\n%s", plain(m))
	}
	// Block choices are refused while the result is edited.
	m, _ = press(t, m, "o")
	if !strings.Contains(plain(m), "u discards the edit") {
		t.Error("no hint for o on an edited result")
	}
	_, out = press(t, m, "enter")
	if got := only[ResolveTextMsg](out); len(got) != 1 || string(got[0].Content) != want {
		t.Fatalf("enter after edit: %v", out)
	}
	// u discards the edit.
	m, _ = press(t, m, "u")
	if m.items[0].text.edited || m.items[0].text.resolvedCount() != 0 {
		t.Error("u did not discard the edit")
	}
}

func TestEditPlainEscKeepsDraft(t *testing.T) {
	m := newModel(t, 120, 40, textFile("n.md", base1, ours1, theirs1))
	m, _ = press(t, m, "e")
	m = typeText(t, m, "Q")
	m, _ = press(t, m, "esc")
	if m.KeyContext() != keys.Resolver {
		t.Fatal("esc in plain mode did not leave the editor")
	}
	tx := m.items[0].text
	if tx.edited || !tx.hasDraft {
		t.Fatalf("edited=%v hasDraft=%v, want a draft only", tx.edited, tx.hasDraft)
	}
	m, _ = press(t, m, "e")
	if got := m.ed.Content(); got != "a\nQX\nc\n" {
		t.Fatalf("draft not resumed: %q", got)
	}
	// Leaving without changes keeps nothing.
	m, _ = press(t, m, "esc", "t")
	if m.items[0].text.hasDraft {
		t.Error("choosing a block should drop the draft")
	}
}

func TestEditVimEscOnlyFromNormal(t *testing.T) {
	m := newModelVim(t, true, 120, 40, textFile("n.md", base1, ours1, theirs1))
	m, _ = press(t, m, "e")
	m, _ = press(t, m, "i")
	m = typeText(t, m, "Q")
	m, _ = press(t, m, "esc")
	if m.KeyContext() != keys.ResolverEdit {
		t.Fatal("esc from insert mode left the editor")
	}
	if m.ed.ModeName() != "NORMAL" {
		t.Fatalf("mode %s, want NORMAL", m.ed.ModeName())
	}
	m, _ = press(t, m, "esc")
	if m.KeyContext() != keys.Resolver {
		t.Fatal("esc from normal mode did not leave the editor")
	}
	if !m.items[0].text.hasDraft || m.items[0].text.edited {
		t.Fatal("draft not kept")
	}
}

func TestEditVimWriteAccepts(t *testing.T) {
	m := newModelVim(t, true, 120, 40, textFile("n.md", base1, ours1, theirs1))
	m, _ = press(t, m, "e", "d", "d")
	m, _ = press(t, m, ":")
	if !strings.Contains(plain(m), ":") || m.CursorPosition() == nil {
		t.Error("command line not drawn with a cursor")
	}
	m, out := press(t, m, "w", "enter")
	if len(out) != 0 {
		t.Errorf(":w leaked messages: %v", out)
	}
	if m.KeyContext() != keys.Resolver || !m.items[0].text.edited {
		t.Fatal(":w did not accept the edit")
	}
	if got := m.items[0].text.editedText; got != "a\nc\n" {
		t.Errorf("edited text %q", got)
	}
}

func TestEditVimQuitLeaves(t *testing.T) {
	m := newModelVim(t, true, 120, 40, textFile("n.md", base1, ours1, theirs1))
	m, _ = press(t, m, "e", ":", "w", "q", "enter")
	if m.KeyContext() != keys.Resolver || !m.items[0].text.edited {
		t.Fatal(":wq did not accept and leave")
	}
	m, _ = press(t, m, "e", ":", "q", "enter")
	if m.KeyContext() != keys.Resolver {
		t.Fatal(":q did not leave the editor")
	}
}

func TestTranslate(t *testing.T) {
	cmd := tea.Batch(
		emit(msgs.SaveRequestMsg{}),
		tea.Sequence(emit(msgs.SaveRequestMsg{}), emit(msgs.QuitMsg{})),
		emit(editor.ChangedMsg{}),
		emit(editor.AutosaveTickMsg{}),
		emit(msgs.OpenNoteMsg{Path: "x.md"}),
		emit(editor.StatusMsg{Text: "hi"}),
		emit(msgs.ToastMsg{Text: "keep"}),
	)
	got := collect(translate(cmd))
	want := []tea.Msg{acceptEditMsg{}, acceptEditMsg{}, leaveEditMsg{}, editStatusMsg{text: "hi"}, msgs.ToastMsg{Text: "keep"}}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("msg %d = %#v, want %#v", i, got[i], want[i])
		}
	}
	if translate(nil) != nil {
		t.Error("translate(nil) should be nil")
	}
}

func TestEditPaste(t *testing.T) {
	m := newModel(t, 120, 40, textFile("n.md", base1, ours1, theirs1))
	m, _ = press(t, m, "e")
	m, _ = m.Update(tea.PasteMsg{Content: "P"})
	m, _ = press(t, m, "ctrl+s")
	if got := m.items[0].text.editedText; got != "a\nPX\nc\n" {
		t.Errorf("paste not applied: %q", got)
	}
}
