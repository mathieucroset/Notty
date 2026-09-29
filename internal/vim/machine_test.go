package vim

import (
	"strings"
	"testing"
	"time"

	"github.com/mathieucroset/notty/internal/buffer"
)

func TestLargeCountsAreFast(t *testing.T) {
	many := strings.Repeat("word\n", 10000)
	tests := []struct {
		name, in, keys string
		check          func(b *buffer.Buffer) bool
	}{
		{"9999ia", "|x", "9999ia<esc>", func(b *buffer.Buffer) bool {
			return b.String() == strings.Repeat("a", 9999)+"x"
		}},
		{"9999ia then dot", "|x", "9999ia<esc>.", func(b *buffer.Buffer) bool {
			return b.LineLen(0) == 2*9999+1
		}},
		{"9999J", "|" + many, "9999J", func(b *buffer.Buffer) bool {
			return strings.HasPrefix(b.Line(0), "word word") && b.LineCount() == 2
		}},
		{"9999x", "|" + strings.Repeat("a", 20000), "9999x", func(b *buffer.Buffer) bool {
			return b.LineLen(0) == 20000-9999
		}},
		{"9999p", "|ab", "yl9999p", func(b *buffer.Buffer) bool { return b.LineLen(0) == 10001 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start := time.Now()
			_, b, _ := run(t, tt.in, tt.keys)
			elapsed := time.Since(start)
			if !tt.check(b) {
				t.Errorf("wrong result: %d lines, line 0 len %d", b.LineCount(), b.LineLen(0))
			}
			if elapsed > 50*time.Millisecond*raceSlowdown {
				t.Errorf("took %v", elapsed)
			}
		})
	}
}

func TestInsertDeleteWord(t *testing.T) {
	runEditCases(t, []editCase{
		{"c-w deletes word", "foo bar|", "A<c-w>", "foo |"},
		{"c-w deletes word and blanks", "foo bar  |", "A<c-w>", "foo |"},
		{"c-w punct", "foo.bar|", "A<c-w>", "foo.|"},
		{"c-w at col 0 joins", "ab\n|cd", "i<c-w>", "ab|cd"},
		{"c-u deletes to line start", "  ab|cd", "i<c-u>", "|cd"},
	})
}

// groupClosed checks that no undo group is left open on b: a fresh edit
// must undo on its own.
func groupClosed(t *testing.T, b *buffer.Buffer, want string) {
	t.Helper()
	before := b.String()
	b.Insert(pos(0, 0), "Z")
	b.Undo()
	if got := b.String(); got != before {
		t.Errorf("an undo group is still open: undo left %q, want %q", got, before)
	}
	if want != "" {
		b.Undo()
		if got := b.String(); got != want {
			t.Errorf("after undoing the session: %q, want %q", got, want)
		}
	}
}

func TestSwitchBufferClosesSession(t *testing.T) {
	t.Run("insert", func(t *testing.T) {
		m := New()
		b1, b2 := newBuf("|ab"), newBuf("|xy")
		feed(m, b1, "ifoo")
		feed(m, b2, "x") // normal-mode x on the new buffer
		if m.Mode() != Normal || b2.String() != "y" {
			t.Errorf("mode %v b2 %q", m.Mode(), b2.String())
		}
		if got := b1.String(); got != "fooab" {
			t.Errorf("b1 = %q", got)
		}
		groupClosed(t, b1, "ab")
	})
	t.Run("visual", func(t *testing.T) {
		m := New()
		b1, b2 := newBuf("|ab"), newBuf("|xy")
		feed(m, b1, "vl")
		if _, ok := m.Selection(b2); ok {
			t.Error("selection reported for another buffer")
		}
		feed(m, b2, "l")
		if m.Mode() != Normal {
			t.Errorf("mode %v", m.Mode())
		}
	})
	t.Run("command line", func(t *testing.T) {
		m := New()
		b1, b2 := newBuf("|ab"), newBuf("|xy")
		feed(m, b1, ":wq")
		eff := feed(m, b2, "<cr>")
		if eff.Save || eff.Quit || m.CommandLine() != "" {
			t.Errorf("command line leaked: %+v %q", eff, m.CommandLine())
		}
	})
	t.Run("paste on another buffer", func(t *testing.T) {
		m := New()
		b1, b2 := newBuf("|ab"), newBuf("|xy")
		feed(m, b1, "iq<c-v>")
		m.PasteClipboard(b2, "P", false)
		if m.Mode() != Normal || b2.String() != "xPy" {
			t.Errorf("mode %v b2 %q", m.Mode(), b2.String())
		}
		groupClosed(t, b1, "ab")
	})
	t.Run("plain", func(t *testing.T) {
		p := NewPlain()
		b1, b2 := newBuf("|ab"), newBuf("|xy")
		feed(p, b1, "foo<s-left>")
		feed(p, b1, "bar")
		feed(p, b2, "q")
		groupClosed(t, b1, "fooab")
		feed(p, b2, "<c-z>")
		if got := b2.String(); got != "xy" {
			t.Errorf("b2 after undo %q", got)
		}
		feed(p, b1, "<s-right>")
		if _, ok := p.Selection(b2); ok {
			t.Error("selection reported for another buffer")
		}
	})
}

func TestReset(t *testing.T) {
	for _, keys := range []string{"ifoo", "vl", ":w", "2d", `"+`} {
		m := New()
		b := newBuf("|ab")
		feed(m, b, keys)
		m.Reset(b)
		if m.Mode() != Normal || m.Pending() != "" || m.CommandLine() != "" {
			t.Errorf("%q: mode %v pending %q cmdline %q", keys, m.Mode(), m.Pending(), m.CommandLine())
		}
		groupClosed(t, b, "")
	}
	// Reset after SetText clamps the cursor for normal mode.
	m := New()
	b := newBuf("abc|")
	feed(m, b, "A")
	b.SetText("xy")
	m.Reset(b)
	if got := show(b); got != "x|y" {
		t.Errorf("after SetText + Reset: %q", got)
	}

	p := NewPlain()
	b = newBuf("|ab")
	feed(p, b, "xy<s-left>")
	p.Reset(b)
	if _, ok := p.Selection(b); ok {
		t.Error("plain Reset kept the selection")
	}
	groupClosed(t, b, "")
	b2 := newBuf("|q")
	feed(p, b2, "zz")
	p.Reset(b2)
	groupClosed(t, b2, "q")
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

func TestPasteClipboardUpdatesDesiredColumn(t *testing.T) {
	m, b, _ := run(t, "|a\nabcdefgh", `"+p`)
	m.PasteClipboard(b, "XYZ", false)
	feed(m, b, "j")
	if got := show(b); got != "aXYZ\nabc|defgh" {
		t.Errorf("vim: %q", got)
	}
	p, b, _ := runPlain(t, "|a\nabcdefgh", "<c-v>")
	p.PasteClipboard(b, "XYZ", false)
	feed(p, b, "<down>")
	if got := show(b); got != "XYZa\nabc|defgh" {
		t.Errorf("plain: %q", got)
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
