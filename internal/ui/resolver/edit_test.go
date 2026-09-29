package resolver

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/mathieucroset/notty/internal/imgrender"
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
	// The cursor starts on the current conflict: result line 1.
	if c.X < m.rightX() || c.Y != 3+1 {
		t.Errorf("cursor at %d,%d, want x >= %d and y 4", c.X, c.Y, m.rightX())
	}
	// It sits on the cell showing that line's text ("A").
	if row := strings.Split(plain(m), "\n")[c.Y]; ansi.Cut(row, c.X, c.X+1) != "A" {
		t.Errorf("cursor cell in %q is %q, want %q", row, ansi.Cut(row, c.X, c.X+1), "A")
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

func TestEditPlainEscKeepsEdits(t *testing.T) {
	m := newModel(t, 120, 40, textFile("n.md", base1, ours1, theirs1))
	m, _ = press(t, m, "e")
	m = typeText(t, m, "Q")
	m, _ = press(t, m, "esc")
	if m.KeyContext() != keys.Resolver {
		t.Fatal("esc in plain mode did not leave the editor")
	}
	tx := m.items[0].text
	if !tx.edited || tx.editedText != "a\nQX\nc\n" {
		t.Fatalf("edited=%v text=%q, want the edit as the result", tx.edited, tx.editedText)
	}
	_, out := press(t, m, "enter")
	if got := only[ResolveTextMsg](out); len(got) != 1 || string(got[0].Content) != "a\nQX\nc\n" {
		t.Fatalf("esc then enter produced %v, want the edited content", out)
	}
}

func TestEditEscWithoutChangesLeavesFileUnresolved(t *testing.T) {
	m := newModel(t, 120, 40, textFile("n.md", base2, ours2, theirs2))
	m, _ = press(t, m, "e", "esc")
	if m.KeyContext() != keys.Resolver {
		t.Fatal("esc did not leave the editor")
	}
	tx := m.items[0].text
	if tx.edited || tx.resolvedCount() != 0 {
		t.Fatalf("edited=%v resolved=%d, want nothing changed", tx.edited, tx.resolvedCount())
	}
	if _, out := press(t, m, "enter"); len(only[ResolveTextMsg](out)) != 0 {
		t.Fatal("an unchanged edit made the file resolvable")
	}
	// ctrl+s always accepts, even unchanged (the pre-filled result).
	m, _ = press(t, m, "e", "ctrl+s")
	if !m.items[0].text.edited || m.items[0].text.editedText != ours2 {
		t.Fatalf("ctrl+s without changes: edited=%v text=%q", m.items[0].text.edited, m.items[0].text.editedText)
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
	if !m.items[0].text.edited || m.items[0].text.editedText != "a\nQX\nc\n" {
		t.Fatalf("edit not kept: %q", m.items[0].text.editedText)
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

func TestCommandLineCursorUsesCellWidth(t *testing.T) {
	m := newModelVim(t, true, 120, 40, textFile("n.md", base1, ours1, theirs1))
	m, _ = press(t, m, "e", ":", "日")
	c := m.CursorPosition()
	if c == nil || c.X != m.rightX()+3 || c.Y != m.bodyHeight() {
		t.Fatalf("cursor %+v, want x %d y %d", c, m.rightX()+3, m.bodyHeight())
	}
	// The command line is drawn on that row, inside the right pane.
	if row := strings.Split(plain(m), "\n")[c.Y]; !strings.Contains(ansi.Cut(row, m.rightX(), m.w), ":日") {
		t.Errorf("command line not on the cursor row: %q", row)
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
	// :q after a change keeps it as the result.
	m, _ = press(t, m, "e", "d", "d", ":", "q", "enter")
	if m.KeyContext() != keys.Resolver {
		t.Fatal(":q did not leave the editor")
	}
	_, out := press(t, m, "enter")
	if got := only[ResolveTextMsg](out); len(got) != 1 || string(got[0].Content) != "X\nc\n" {
		t.Fatalf(":q then enter produced %v", out)
	}
}

func TestOwnsAndRouting(t *testing.T) {
	if !Owns(editorMsg{inner: msgs.ToastMsg{}}) {
		t.Error("Owns(editorMsg) = false")
	}
	for _, msg := range []tea.Msg{key("o"), tea.PasteMsg{}, msgs.SaveRequestMsg{}, msgs.ToastMsg{}, nil} {
		if Owns(msg) {
			t.Errorf("Owns(%#v) = true", msg)
		}
	}
	m := newModel(t, 120, 40, textFile("n.md", base1, ours1, theirs1))
	m, _ = press(t, m, "e")
	// Messages the resolver does not own are ignored, even while editing:
	// an app-level save request must not accept the edit.
	for _, msg := range []tea.Msg{msgs.SaveRequestMsg{}, msgs.QuitMsg{}, editor.StatusMsg{Text: "x"}} {
		var cmd tea.Cmd
		m, cmd = m.Update(msg)
		if cmd != nil || !m.Editing() {
			t.Fatalf("Update(%#v) acted: cmd=%v editing=%v", msg, cmd != nil, m.Editing())
		}
	}
	// A status message wrapped by the editor reaches the edit footer.
	m, _ = m.Update(editorMsg{inner: editor.StatusMsg{Text: "search hit BOTTOM"}})
	if !strings.Contains(plain(m), "search hit BOTTOM") {
		t.Error("editor status not shown")
	}
}

func TestEditorClipboardComesBackWrapped(t *testing.T) {
	s, p, opts := testOpts(t, true)
	opts.Clipboard = fakeClipboard{text: "CLIP"}
	caps := imgrender.Caps{Inline: imgrender.ProtoHalfBlocks}
	m := New([]File{textFile("n.md", base1, ours1, theirs1)}, s, p, caps, opts).SetSize(120, 40)
	m, _ = press(t, m, "e")
	var cmd tea.Cmd
	for _, k := range []string{"\"", "+", "p"} {
		m, cmd = m.Update(key(k))
	}
	got := collect(cmd)
	if len(got) != 1 || !Owns(got[0]) {
		t.Fatalf("clipboard read produced %#v, want one owned message", got)
	}
	m, _ = drain(m, cmd)
	m, _ = press(t, m, "ctrl+s")
	if tx := m.items[0].text; !strings.Contains(tx.editedText, "CLIP") {
		t.Errorf("clipboard text not pasted: %q", tx.editedText)
	}
}

func TestWriteAcceptsSynchronously(t *testing.T) {
	m := newModelVim(t, true, 120, 40, textFile("n.md", base1, ours1, theirs1))
	m, _ = press(t, m, "e", "d", "d", ":", "w")
	// The accept happens inside Update, before any command runs.
	m, _ = m.Update(key("enter"))
	if m.Editing() || !m.items[0].text.edited {
		t.Fatal(":w did not accept synchronously")
	}
	// The next key is a resolver key, not an editor key.
	m, _ = m.Update(key("o"))
	if !strings.Contains(plain(m), "u discards the edit") {
		t.Error("the key after :w went to the editor")
	}
}

// fakeClipboard is an editor.Clipboard holding text only.
type fakeClipboard struct{ text string }

func (f fakeClipboard) ReadImage() ([]byte, error) { return nil, errNoImage }
func (f fakeClipboard) ReadText() (string, error)  { return f.text, nil }
func (f fakeClipboard) WriteText(string) error     { return nil }

var errNoImage = errors.New("no image")

func TestEditPaste(t *testing.T) {
	m := newModel(t, 120, 40, textFile("n.md", base1, ours1, theirs1))
	m, _ = press(t, m, "e")
	m, _ = m.Update(tea.PasteMsg{Content: "P"})
	m, _ = press(t, m, "ctrl+s")
	if got := m.items[0].text.editedText; got != "a\nPX\nc\n" {
		t.Errorf("paste not applied: %q", got)
	}
}
