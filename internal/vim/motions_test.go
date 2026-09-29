package vim

import (
	"testing"

	"github.com/mathieucroset/notty/internal/buffer"
)

func TestMotions(t *testing.T) {
	runEditCases(t, []editCase{
		// h j k l
		{"l", "|abc", "l", "a|bc"},
		{"3l", "|abcdef", "3l", "abc|def"},
		{"l stops at last char", "a|bc", "5l", "ab|c"},
		{"h", "ab|c", "h", "a|bc"},
		{"h at col 0", "|abc", "h", "|abc"},
		{"2h", "abc|d", "2h", "a|bcd"},
		{"arrows", "|abc\ndef", "<right><down><left>", "abc\n|def"},
		{"j", "|abc\ndef", "j", "abc\n|def"},
		{"j clamps col", "ab|cdef\nxy", "j", "abcdef\nx|y"},
		{"5j clamps to last line", "|a\nb\nc", "5j", "a\nb\n|c"},
		{"j on last line", "a\n|b", "j", "a\n|b"},
		{"k", "abc\nd|ef", "k", "a|bc\ndef"},
		{"3k", "a\nb\nc\n|d", "3k", "|a\nb\nc\nd"},
		{"sticky col j j", "abc|def\nx\nabcdefg", "jj", "abcdef\nx\nabc|defg"},
		{"sticky col k k", "abcdefg\nx\nabc|def", "kk", "abc|defg\nx\nabcdef"},
		{"sticky col through empty", "ab|c\n\nabc", "jj", "abc\n\nab|c"},
		{"$ sticks to eol", "a|b\nabcdef\nxyz", "$jj", "ab\nabcdef\nxy|z"},
		{"$ then j then k", "|ab\nabcdef", "$jk", "a|b\nabcdef"},
		{"l resets sticky", "abcdef\nabcdef", "$hj", "abcdef\nabcd|ef"},
		// line positions
		{"0", "abc|def", "0", "|abcdef"},
		{"home", "abc|def", "<home>", "|abcdef"},
		{"^", "   ab|c", "^", "   |abc"},
		{"^ on blank line", "  |  ", "^", "   | "},
		{"$", "|abc", "$", "ab|c"},
		{"end", "|abc", "<end>", "ab|c"},
		{"2$", "|abc\ndefg", "2$", "abc\ndef|g"},
		{"$ on empty", "|", "$", "|"},
		{"g_", "|abc  ", "g_", "ab|c  "},
		{"+", "|abc\n  def", "+", "abc\n  |def"},
		{"-", "  abc\nd|ef", "-", "  |abc\ndef"},
		{"cr moves down", "|a\n  b", "<cr>", "a\n  |b"},
		// gg G
		{"gg", "abc\n  d|ef", "gg", "|abc\n  def"},
		{"gg first nonblank", "  abc\nd|ef", "gg", "  |abc\ndef"},
		{"G", "|abc\n  def", "G", "abc\n  |def"},
		{"3G", "|a\nb\nc\nd", "3G", "a\nb\n|c\nd"},
		{"2gg", "a\nb\n|c", "2gg", "a\n|b\nc"},
		{"99G clamps", "|a\nb", "99G", "a\n|b"},
		// word motions
		{"w", "|foo bar", "w", "foo |bar"},
		{"w punct", "|foo.bar", "w", "foo|.bar"},
		{"w from punct", "foo|.bar", "w", "foo.|bar"},
		{"W skips punct", "|foo.bar baz", "W", "foo.bar |baz"},
		{"3w", "|a b c d", "3w", "a b c |d"},
		{"w across lines", "fo|o\n  bar", "w", "foo\n  |bar"},
		{"w stops at empty line", "|foo\n\nbar", "w", "foo\n|\nbar"},
		{"w on last word goes to end", "foo |bar", "w", "foo ba|r"},
		{"w multiple spaces", "|a   b", "w", "a   |b"},
		{"w unicode letters", "|héllo wörld", "w", "héllo |wörld"},
		{"w underscore is keyword", "|foo_bar baz", "w", "foo_bar |baz"},
		{"b", "foo ba|r", "b", "foo |bar"},
		{"b to previous word", "foo |bar", "b", "|foo bar"},
		{"b across lines", "foo\n|bar", "b", "|foo\nbar"},
		{"b stops at empty line", "foo\n\n|bar", "b", "foo\n|\nbar"},
		{"2b", "a b |c", "2b", "|a b c"},
		{"B", "foo.bar ba|z", "B", "foo.bar |baz"},
		{"B skips punct", "foo.bar |baz", "B", "|foo.bar baz"},
		{"b at start", "|foo", "b", "|foo"},
		{"e", "|foo bar", "e", "fo|o bar"},
		{"e from end of word", "fo|o bar", "e", "foo ba|r"},
		{"2e", "|foo bar baz", "2e", "foo ba|r baz"},
		{"e across lines", "fo|o\n  bar", "e", "foo\n  ba|r"},
		{"e skips empty lines", "fo|o\n\nbar", "e", "foo\n\nba|r"},
		{"e punct", "|foo.bar", "e", "fo|o.bar"},
		{"E", "|foo.bar baz", "E", "foo.ba|r baz"},
		{"e at buffer end", "fo|o", "e", "fo|o"},
		{"ge", "foo ba|r", "ge", "fo|o bar"},
		{"gE", "a.b c|d", "gE", "a.|b cd"},
		// f t
		{"fx", "|abcxdef", "fx", "abc|xdef"},
		{"2fx", "|axbxc", "2fx", "axb|xc"},
		{"fx not found", "|abc", "fz", "|abc"},
		{"Fx", "axbc|d", "Fx", "a|xbcd"},
		{"tx", "|abcxd", "tx", "ab|cxd"},
		{"Tx", "axbc|d", "Tx", "ax|bcd"},
		{"f;", "|a,b,c", "f,;", "a,b|,c"},
		{"f;,", "|a,b,c,", "f,;;,", "a,b|,c,"},
		{"t; skips adjacent", "|a,b,c", "t,;", "a,|b,c"},
		{"F;", "a,b,|c", "F,;", "a|,b,c"},
		{"f space", "|ab cd", "f<space>", "ab| cd"},
		// paragraphs
		{"}", "|a\nb\n\nc", "}", "a\nb\n|\nc"},
		{"2}", "|a\n\nb\n\nc", "2}", "a\n\nb\n|\nc"},
		{"} to end", "a\n\n|b\nc", "}", "a\n\nb\n|c"},
		{"{", "a\n\nb\n|c", "{", "a\n|\nb\nc"},
		{"{ to start", "a\n|b", "{", "|a\nb"},
		{"} from blank", "a\n|\n\nb\nc\n\nd", "}", "a\n\n\nb\nc\n|\nd"},
		// %
		{"% forward", "|(a(b)c)", "%", "(a(b)c|)"},
		{"% backward", "(a(b)c|)", "%", "|(a(b)c)"},
		{"% finds bracket after cursor", "|x = [1, 2]", "%", "x = [1, 2|]"},
		{"% across lines", "|{\n  a\n}", "%", "{\n  a\n|}"},
		{"% no bracket", "|abc", "%", "|abc"},
		{"50%", "|1\n2\n3\n4", "50%", "1\n|2\n3\n4"},
		// counts
		{"count multiplies with nothing", "|a b c d e", "2w", "a b |c d e"},
		{"10l", "|abcdefghijklm", "10l", "abcdefghij|klm"},
		{"0 after count is count digit", "|abcdefghijklm", "10l0", "|abcdefghijklm"},
		// grapheme columns
		{"l over emoji", "|a👍b", "ll", "a👍|b"},
		{"j keeps grapheme col", "a👍|b\nxyz", "j", "a👍b\nxy|z"},
	})
}

// fakeNav moves by 10-column screen rows on one line.
type fakeNav struct{}

func (fakeNav) Down(b *buffer.Buffer, p buffer.Pos, n int) buffer.Pos {
	return buffer.Pos{Line: p.Line, Col: p.Col + 10*n}
}

func (fakeNav) Up(b *buffer.Buffer, p buffer.Pos, n int) buffer.Pos {
	return buffer.Pos{Line: p.Line, Col: max(0, p.Col-10*n)}
}

func TestWrapNavigator(t *testing.T) {
	long := "0123456789abcdefghij"
	tests := []struct {
		name, in, keys, want string
		nav                  bool
	}{
		{"gj without nav is j", "|" + long + "\nx", "gj", long + "\n|x", false},
		{"gk without nav is k", long + "\n|x", "gk", "|" + long + "\nx", false},
		{"gj with nav", "|" + long, "gj", "0123456789|abcdefghij", true},
		{"gk with nav", "0123456789a|bcdefghij", "gk", "0|123456789abcdefghij", true},
		{"2gj with nav clamps", "|" + long, "2gj", "0123456789abcdefghi|j", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := New()
			if tt.nav {
				m.SetWrapNavigator(fakeNav{})
			}
			b := newBuf(tt.in)
			feed(m, b, tt.keys)
			if got := show(b); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPending(t *testing.T) {
	m := New()
	b := newBuf("|abc")
	feed(m, b, "2")
	if got := m.Pending(); got != "2" {
		t.Errorf("Pending() = %q, want %q", got, "2")
	}
	feed(m, b, "f")
	if got := m.Pending(); got != "2f" {
		t.Errorf("Pending() = %q, want %q", got, "2f")
	}
	feed(m, b, "<esc>")
	if got := m.Pending(); got != "" {
		t.Errorf("Pending() after esc = %q", got)
	}
	feed(m, b, "l")
	if got := show(b); got != "a|bc" {
		t.Errorf("after esc, l moved to %q", got)
	}
}

func TestInsertMode(t *testing.T) {
	tests := []struct {
		name, in, keys, want string
		mode                 Mode
	}{
		{"i", "ab|c", "iX", "abX|c", Insert},
		{"i esc moves left", "ab|c", "iX<esc>", "ab|Xc", Normal},
		{"a", "a|bc", "aX<esc>", "ab|Xc", Normal},
		{"a on empty line", "|", "aX<esc>", "|X", Normal},
		{"I", "  ab|c", "IX<esc>", "  |Xabc", Normal},
		{"A", "a|bc", "AX<esc>", "abc|X", Normal},
		{"o keeps indent", "  a|b\nc", "ox<esc>", "  ab\n  |x\nc", Normal},
		{"O keeps indent", "x\n  a|b", "Oy<esc>", "x\n  |y\n  ab", Normal},
		{"O on first line", "a|b", "Oy<esc>", "|y\nab", Normal},
		{"o on last line", "a|b", "o", "ab\n|", Insert},
		{"esc at col 0", "|ab", "i<esc>", "|ab", Normal},
		{"c-c leaves insert", "ab|c", "iX<c-c>", "ab|Xc", Normal},
		{"backspace", "abc|d", "i<bs><esc>", "a|bd", Normal},
		{"backspace joins lines", "ab\n|cd", "i<bs>", "ab|cd", Insert},
		{"backspace at buffer start", "|ab", "i<bs>", "|ab", Insert},
		{"delete", "a|bc", "i<del>", "a|c", Insert},
		{"delete joins next line", "ab\ncd", "A<del>", "ab|cd", Insert},
		{"enter splits", "ab|cd", "i<cr>", "ab\n|cd", Insert},
		{"enter keeps indent", "  ab|cd", "i<cr>", "  ab\n  |cd", Insert},
		{"enter in indent", " | ab", "i<cr>", " \n| ab", Insert},
		{"arrows in insert", "abc\nd", "A<left><left><down>", "abc\nd|", Insert},
		{"up keeps col", "abcd\nx\nabcd", "GA<up><up>", "abcd|\nx\nabcd", Insert},
		{"home end", "a|bc", "i<end>X<home>Y", "Y|abcX", Insert},
		{"space inserts", "|ab", "i<space>", " |ab", Insert},
		{"3i repeats text", "|x", "3iab<esc>", "ababa|bx", Normal},
		{"2A repeats", "|x", "2A-<esc>", "x-|-", Normal},
		{"unicode", "|x", "ié👍<esc>", "é|👍x", Normal},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, b, _ := run(t, tt.in, tt.keys)
			if got := show(b); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
			if m.Mode() != tt.mode {
				t.Errorf("mode = %v, want %v", m.Mode(), tt.mode)
			}
		})
	}
}

func TestInsertPasteText(t *testing.T) {
	m := New()
	b := newBuf("|x")
	feed(m, b, "i")
	m.Handle(b, Key{Text: "hello world"})
	if got := show(b); got != "hello world|x" {
		t.Errorf("got %q", got)
	}
}

func TestNormalMultiRuneKeyEntersInsert(t *testing.T) {
	m := New()
	b := newBuf("|x")
	m.Handle(b, Key{Text: "ifoo bar"})
	if got := show(b); got != "foo bar|x" || m.Mode() != Insert {
		t.Errorf("got %q mode %v", got, m.Mode())
	}
}

func TestInsertSessionIsOneUndoStep(t *testing.T) {
	m := New()
	b := newBuf("|x")
	feed(m, b, "iab<cr>cd<esc>")
	if got := b.String(); got != "ab\ncdx" {
		t.Fatalf("text = %q", got)
	}
	b.Undo()
	if got := b.String(); got != "x" {
		t.Errorf("after one undo = %q, want %q", got, "x")
	}
}

func TestModeName(t *testing.T) {
	m := New()
	b := newBuf("|x")
	if m.ModeName() != "NORMAL" {
		t.Errorf("ModeName = %q", m.ModeName())
	}
	feed(m, b, "i")
	if m.ModeName() != "INSERT" {
		t.Errorf("ModeName = %q", m.ModeName())
	}
}
