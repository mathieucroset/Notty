package buffer

import (
	"testing"
)

func P(line, col int) Pos { return Pos{Line: line, Col: col} }

func R(l1, c1, l2, c2 int) Range { return Range{Start: P(l1, c1), End: P(l2, c2)} }

func TestNewStringRoundTrip(t *testing.T) {
	tests := []struct {
		name      string
		in        string
		want      string
		wantLines int
	}{
		{"empty", "", "", 1},
		{"single line no newline", "hello", "hello", 1},
		{"single line with newline", "hello\n", "hello\n", 1},
		{"only newline", "\n", "\n", 1},
		{"multi no trailing", "a\nb\nc", "a\nb\nc", 3},
		{"multi trailing", "a\nb\nc\n", "a\nb\nc\n", 3},
		{"trailing blank line", "a\n\n", "a\n\n", 2},
		{"crlf normalizes", "a\r\nb\r\n", "a\nb\n", 2},
		{"crlf no trailing", "a\r\nb", "a\nb", 2},
		{"lone cr kept", "a\rb", "a\rb", 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := New(tt.in)
			if got := b.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
			if got := b.LineCount(); got != tt.wantLines {
				t.Errorf("LineCount() = %d, want %d", got, tt.wantLines)
			}
		})
	}
}

func TestLineAccessors(t *testing.T) {
	b := New("abc\n" + thumbsUp + eAcute + "\n" + cjk + "\n")
	tests := []struct {
		i         int
		wantLine  string
		wantLen   int
		wantWidth int
	}{
		{0, "abc", 3, 3},
		{1, thumbsUp + eAcute, 2, 3},
		{2, cjk, 2, 4},
		{-1, "", 0, 0},
		{3, "", 0, 0},
	}
	for _, tt := range tests {
		if got := b.Line(tt.i); got != tt.wantLine {
			t.Errorf("Line(%d) = %q, want %q", tt.i, got, tt.wantLine)
		}
		if got := b.LineLen(tt.i); got != tt.wantLen {
			t.Errorf("LineLen(%d) = %d, want %d", tt.i, got, tt.wantLen)
		}
		if got := DisplayWidth(b.Line(tt.i)); got != tt.wantWidth {
			t.Errorf("DisplayWidth(Line(%d)) = %d, want %d", tt.i, got, tt.wantWidth)
		}
	}
}

func TestClamp(t *testing.T) {
	b := New("hello\n" + cjk + "\nxy")
	tests := []struct {
		name string
		in   Pos
		want Pos
	}{
		{"inside", P(0, 2), P(0, 2)},
		{"end of line allowed", P(0, 5), P(0, 5)},
		{"past eol", P(0, 99), P(0, 5)},
		{"past eol graphemes", P(1, 5), P(1, 2)},
		{"negative col", P(1, -3), P(1, 0)},
		{"negative line", P(-2, 3), P(0, 3)},
		{"past eof", P(10, 1), P(2, 1)},
		{"past eof and eol", P(10, 10), P(2, 2)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := b.Clamp(tt.in); got != tt.want {
				t.Errorf("Clamp(%v) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestSetCursorClamps(t *testing.T) {
	b := New("ab\ncd")
	b.SetCursor(P(9, 9))
	if got := b.Cursor(); got != P(1, 2) {
		t.Errorf("Cursor() = %v, want %v", got, P(1, 2))
	}
	b.SetCursor(P(0, 1))
	if got := b.Cursor(); got != P(0, 1) {
		t.Errorf("Cursor() = %v, want %v", got, P(0, 1))
	}
}

func TestInsert(t *testing.T) {
	tests := []struct {
		name    string
		initial string
		at      Pos
		text    string
		want    string
		wantEnd Pos
	}{
		{"ascii middle", "hello", P(0, 2), "XY", "heXYllo", P(0, 4)},
		{"ascii start", "hello", P(0, 0), ">", ">hello", P(0, 1)},
		{"into empty buffer", "", P(0, 0), "abc", "abc", P(0, 3)},
		{"multi-line", "hello", P(0, 2), "1\n2\n3", "he1\n2\n3llo", P(2, 1)},
		{"newline only splits line", "hello", P(0, 2), "\n", "he\nllo", P(1, 0)},
		{"trailing newline in text", "ab", P(0, 1), "x\n", "ax\nb", P(1, 0)},
		{"at eof", "a\nb", P(1, 1), "c", "a\nbc", P(1, 2)},
		{"at eof keeps trailing newline", "a\nb\n", P(1, 1), "\nc", "a\nb\nc\n", P(2, 1)},
		{"past eof clamps", "a\nb", P(7, 7), "!", "a\nb!", P(1, 2)},
		{"after emoji", "a" + thumbsUp + "b", P(0, 2), "X", "a" + thumbsUp + "Xb", P(0, 3)},
		{"after combining", eAcute + "b", P(0, 1), "X", eAcute + "Xb", P(0, 2)},
		{"cjk", cjk, P(0, 1), "X", "日X本", P(0, 2)},
		{"insert emoji", "ab", P(0, 1), thumbsUp, "a" + thumbsUp + "b", P(0, 2)},
		{"crlf in text normalizes", "ab", P(0, 1), "1\r\n2", "a1\n2b", P(1, 1)},
		{"empty text no-op", "ab", P(0, 1), "", "ab", P(0, 1)},
		{"before lone combining mark rounds up", "\u0301", P(0, 0), "e", "e\u0301", P(0, 1)},
		{"before lone regional indicator rounds up", "\U0001F1EB", P(0, 0), "\U0001F1F7", "\U0001F1F7\U0001F1EB", P(0, 1)},
		{"multi-line ending before lone mark", "\u0301", P(0, 0), "a\ne", "a\ne\u0301", P(1, 1)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := New(tt.initial)
			end := b.Insert(tt.at, tt.text)
			if got := b.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
			if end != tt.wantEnd {
				t.Errorf("end = %v, want %v", end, tt.wantEnd)
			}
		})
	}
}

// Regression: typing a base character before a lone combining mark merges
// the two; the next character typed at the returned position must go after
// the merged cluster, not before the mark.
func TestTypingBeforeCombiningMark(t *testing.T) {
	b := New("\u0301")
	p := b.Insert(P(0, 0), "e")
	p = b.Insert(p, "x")
	if got, want := b.String(), "e\u0301x"; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
	if p != P(0, 2) {
		t.Errorf("end = %v, want %v", p, P(0, 2))
	}
}

func TestDelete(t *testing.T) {
	tests := []struct {
		name        string
		initial     string
		r           Range
		want        string
		wantDeleted string
	}{
		{"within line", "hello", R(0, 1, 0, 3), "hlo", "el"},
		{"whole line content", "hello\nx", R(0, 0, 0, 5), "\nx", "hello"},
		{"join lines", "ab\ncd", R(0, 2, 1, 0), "abcd", "\n"},
		{"across lines", "hello\nmiddle\nworld", R(0, 2, 2, 3), "held", "llo\nmiddle\nwor"},
		{"reversed range normalizes", "hello\nworld", R(1, 2, 0, 3), "helrld", "lo\nwo"},
		{"empty range", "hello", R(0, 2, 0, 2), "hello", ""},
		{"emoji is one col", "a" + thumbsUp + "b", R(0, 1, 0, 2), "ab", thumbsUp},
		{"combining is one col", "x" + eAcute + "y", R(0, 1, 0, 2), "xy", eAcute},
		{"cjk one col each", "a日本b", R(0, 1, 0, 2), "a本b", "日"},
		{"clamped past eof", "ab\ncd\n", R(0, 1, 9, 9), "a\n", "b\ncd"},
		{"everything", "ab\ncd", R(0, 0, 1, 2), "", "ab\ncd"},
		{"end past eof means end of buffer", "ab\ncd", R(1, 1, 3, 0), "ab\nc", "d"},
		{"start past eol, end past eof", "ab\ncd", R(1, 5, 2, 1), "ab\ncd", ""},
		{"start before bof", "ab\ncd", R(-1, 5, 0, 1), "b\ncd", "a"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := New(tt.initial)
			deleted := b.Delete(tt.r)
			if deleted != tt.wantDeleted {
				t.Errorf("deleted = %q, want %q", deleted, tt.wantDeleted)
			}
			if got := b.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
			if b.LineCount() < 1 {
				t.Errorf("LineCount() = %d, want >= 1", b.LineCount())
			}
		})
	}
}

func TestReplace(t *testing.T) {
	tests := []struct {
		name    string
		initial string
		r       Range
		text    string
		want    string
		wantEnd Pos
	}{
		{"same line", "hello world", R(0, 6, 0, 11), "there", "hello there", P(0, 11)},
		{"shrink across lines", "ab\ncd\nef", R(0, 1, 2, 1), "X", "aXf", P(0, 2)},
		{"grow to lines", "abc", R(0, 1, 0, 2), "1\n2", "a1\n2c", P(1, 1)},
		{"empty range is insert", "abc", R(0, 1, 0, 1), "Z", "aZbc", P(0, 2)},
		{"empty text is delete", "abc", R(0, 0, 0, 2), "", "c", P(0, 0)},
		{"emoji", thumbsUp + "x", R(0, 0, 0, 1), "y", "yx", P(0, 1)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := New(tt.initial)
			end := b.Replace(tt.r, tt.text)
			if got := b.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
			if end != tt.wantEnd {
				t.Errorf("end = %v, want %v", end, tt.wantEnd)
			}
		})
	}
}

func TestTextIn(t *testing.T) {
	b := New("hello\n" + thumbsUp + "mid\nworld\n")
	tests := []struct {
		name string
		r    Range
		want string
	}{
		{"within line", R(0, 1, 0, 4), "ell"},
		{"across lines", R(0, 3, 2, 2), "lo\n" + thumbsUp + "mid\nwo"},
		{"reversed", R(2, 2, 0, 3), "lo\n" + thumbsUp + "mid\nwo"},
		{"line break only", R(0, 5, 1, 0), "\n"},
		{"empty", R(1, 1, 1, 1), ""},
		{"clamped", R(2, 3, 50, 50), "ld"},
		{"emoji", R(1, 0, 1, 1), thumbsUp},
		{"start past eol, end past eof", R(2, 9, 4, 1), ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := b.TextIn(tt.r); got != tt.want {
				t.Errorf("TextIn(%v) = %q, want %q", tt.r, got, tt.want)
			}
		})
	}
}

func TestCursorClampedAfterEdit(t *testing.T) {
	b := New("hello\nworld")
	b.SetCursor(P(1, 5))
	b.Delete(R(0, 3, 1, 5))
	if got := b.Cursor(); got != P(0, 3) {
		t.Errorf("Cursor() = %v, want %v", got, P(0, 3))
	}
}

func TestVersionIncrements(t *testing.T) {
	b := New("abc")
	v0 := b.Version()
	b.Insert(P(0, 0), "x")
	v1 := b.Version()
	if v1 <= v0 {
		t.Fatalf("Version after Insert = %d, want > %d", v1, v0)
	}
	b.Delete(R(0, 0, 0, 1))
	v2 := b.Version()
	if v2 <= v1 {
		t.Fatalf("Version after Delete = %d, want > %d", v2, v1)
	}
	b.Replace(R(0, 0, 0, 1), "z")
	v3 := b.Version()
	if v3 <= v2 {
		t.Fatalf("Version after Replace = %d, want > %d", v3, v2)
	}
	b.Insert(P(0, 0), "")
	b.Delete(R(0, 1, 0, 1))
	b.SetCursor(P(0, 2))
	if got := b.Version(); got != v3 {
		t.Errorf("Version after no-ops = %d, want %d", got, v3)
	}
}

func TestFirstChangedLine(t *testing.T) {
	tests := []struct {
		name  string
		edits func(b *Buffer)
		want  int
	}{
		{"none", func(b *Buffer) {}, -1},
		{"single insert", func(b *Buffer) { b.Insert(P(2, 0), "x") }, 2},
		{"lowest of several", func(b *Buffer) {
			b.Insert(P(3, 0), "x")
			b.Delete(R(1, 0, 1, 1))
			b.Replace(R(2, 0, 2, 1), "y")
		}, 1},
		{"multi-line delete reports start", func(b *Buffer) { b.Delete(R(1, 1, 3, 0)) }, 1},
		{"no-op edit untouched", func(b *Buffer) { b.Insert(P(0, 0), "") }, -1},
		{"set text", func(b *Buffer) { b.SetText("zzz") }, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := New("l0\nl1\nl2\nl3\nl4")
			tt.edits(b)
			if got := b.FirstChangedLine(); got != tt.want {
				t.Errorf("FirstChangedLine() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestResetChanged(t *testing.T) {
	b := New("a\nb\nc")
	b.Insert(P(0, 0), "x")
	b.ResetChanged()
	if got := b.FirstChangedLine(); got != -1 {
		t.Fatalf("after reset = %d, want -1", got)
	}
	b.Insert(P(2, 0), "y")
	if got := b.FirstChangedLine(); got != 2 {
		t.Errorf("after edit = %d, want 2", got)
	}
}

func TestSetText(t *testing.T) {
	tests := []struct {
		name    string
		initial string
		text    string
		cursor  Pos
		want    string
		wantCur Pos
	}{
		{"replaces all", "a\nb\nc\n", "x\ny", P(0, 0), "x\ny", P(0, 0)},
		{"adds trailing newline", "abc", "abc\n", P(0, 1), "abc\n", P(0, 1)},
		{"crlf normalizes", "", "p\r\nq\r\n", P(0, 0), "p\nq\n", P(0, 0)},
		{"cursor clamped", "long line\nsecond\nthird", "hi", P(2, 4), "hi", P(0, 2)},
		{"empty", "abc\n", "", P(0, 2), "", P(0, 0)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := New(tt.initial)
			b.SetCursor(tt.cursor)
			b.SetText(tt.text)
			if got := b.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
			if got := b.Cursor(); got != tt.wantCur {
				t.Errorf("Cursor() = %v, want %v", got, tt.wantCur)
			}
		})
	}
}

func TestSetTextIdenticalIsNoOp(t *testing.T) {
	b := New("same\n")
	v := b.Version()
	b.SetText("same\n")
	if b.Version() != v {
		t.Errorf("Version changed on identical SetText")
	}
	if b.FirstChangedLine() != -1 {
		t.Errorf("FirstChangedLine = %d, want -1", b.FirstChangedLine())
	}
}
