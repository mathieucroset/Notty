package vim

import "testing"

func TestInsertDeleteWord(t *testing.T) {
	runEditCases(t, []editCase{
		{"c-w deletes word", "foo bar|", "A<c-w>", "foo |"},
		{"c-w deletes word and blanks", "foo bar  |", "A<c-w>", "foo |"},
		{"c-w punct", "foo.bar|", "A<c-w>", "foo.|"},
		{"c-w at col 0 joins", "ab\n|cd", "i<c-w>", "ab|cd"},
		{"c-u deletes to line start", "  ab|cd", "i<c-u>", "|cd"},
	})
}

func TestMultiGraphemeKeys(t *testing.T) {
	m := New()
	b := newBuf("|x foo")
	m.Handle(b, Key{Text: "/foo"})
	if m.CommandLine() != "/foo" {
		t.Fatalf("CommandLine = %q", m.CommandLine())
	}
	feed(m, b, "<cr>")
	if got := show(b); got != "x |foo" {
		t.Errorf("got %q", got)
	}
	m = New()
	b = newBuf("|x")
	m.Handle(b, Key{Text: "i<space>"})
	if got := show(b); got != "<space>|x" {
		t.Errorf("literal text after i: %q", got)
	}
}

func TestTypedTextThatLooksLikeAKeyName(t *testing.T) {
	m, b, _ := run(t, "|x", "i")
	m.Handle(b, Key{Text: "<esc>"})
	if got := show(b); got != "<esc>|x" || m.Mode() != Insert {
		t.Errorf("insert: %q mode %v", got, m.Mode())
	}
	m, b, _ = run(t, "|x", ":")
	m.Handle(b, Key{Text: "<cr>"})
	if m.CommandLine() != ":<cr>" {
		t.Errorf("cmdline %q", m.CommandLine())
	}
	p := NewPlain()
	b = newBuf("|x")
	p.Handle(b, Key{Text: "<bs>"})
	if got := show(b); got != "<bs>|x" {
		t.Errorf("plain: %q", got)
	}
}

func TestPasteClipboardLeavesVisual(t *testing.T) {
	m, b, _ := run(t, "|ab", "v")
	m.PasteClipboard(b, "X", false)
	if m.Mode() != Normal {
		t.Errorf("mode = %v", m.Mode())
	}
	if got := show(b); got != "a|Xb" {
		t.Errorf("got %q", got)
	}
}

func TestPasteClipboardUndo(t *testing.T) {
	m, b, _ := run(t, "a|b", `"+p`)
	m.PasteClipboard(b, "XY\n", false)
	if got := b.String(); got != "ab\nXY" {
		t.Fatalf("text %q", got)
	}
	feed(m, b, "u")
	if got := show(b); got != "a|b" {
		t.Errorf("after undo %q", got)
	}
}

func TestExternalCursorMoveResetsDesiredColumn(t *testing.T) {
	m, b, _ := run(t, "abc|def\nx\nabcdefg", "$")
	b.SetCursor(pos(0, 1)) // e.g. a mouse click
	feed(m, b, "jj")
	if got := show(b); got != "abcdef\nx\na|bcdefg" {
		t.Errorf("got %q", got)
	}
}

func TestDotAfterNonInsertChangeDoesNotLeakRecording(t *testing.T) {
	// x records a change; replaying an earlier insert must not overwrite it.
	runEditCases(t, []editCase{
		{"A then x then dot", "|ab\ncd", "A!<esc>x.", "|a\ncd"},
		{"x then A then dot", "|ab\ncd", "xA!<esc>j.", "b!\ncd|!"},
	})
}
