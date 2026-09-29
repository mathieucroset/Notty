package vim

import (
	"testing"
)

// opCase checks text, cursor, mode and the unnamed register.
type opCase struct {
	name, in, keys, want string
	reg                  string // "" = don't check
	mode                 Mode
}

func runOpCases(t *testing.T, cases []opCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, b, _ := run(t, tc.in, tc.keys)
			if got := show(b); got != tc.want {
				t.Errorf("%q + %q:\n got %q\nwant %q", tc.in, tc.keys, got, tc.want)
			}
			if m.Mode() != tc.mode {
				t.Errorf("mode = %v, want %v", m.Mode(), tc.mode)
			}
			if tc.reg != "" {
				text, lw := m.Register()
				got := text
				if lw {
					got = "L:" + text
				}
				if got != tc.reg {
					t.Errorf("register = %q, want %q", got, tc.reg)
				}
			}
		})
	}
}

func TestOperators(t *testing.T) {
	runOpCases(t, []opCase{
		// d + motion
		{"dw", "|foo bar", "dw", "|bar", "foo ", Normal},
		{"dw last word stays on line", "foo |bar\nbaz", "dw", "foo| \nbaz", "bar", Normal},
		{"dw at end of buffer", "foo |bar", "dw", "foo| ", "bar", Normal},
		{"d2w", "|a b c", "d2w", "|c", "a b ", Normal},
		{"2dw", "|a b c", "2dw", "|c", "a b ", Normal},
		{"2d3w = 6 words", "|a b c d e f g", "2d3w", "|g", "a b c d e f ", Normal},
		{"3dw", "|a b c d", "3dw", "|d", "", Normal},
		{"dw on punct", "|foo.bar", "dw", "|.bar", "foo", Normal},
		{"dW", "|foo.bar baz", "dW", "|baz", "foo.bar ", Normal},
		{"de", "|foo bar", "de", "| bar", "foo", Normal},
		{"de from end", "fo|o bar", "de", "f|o", "o bar", Normal},
		{"db", "foo |bar", "db", "|bar", "foo ", Normal},
		{"d$", "a|bcd", "d$", "|a", "bcd", Normal},
		{"D", "a|bcd", "D", "|a", "bcd", Normal},
		{"d0", "ab|cd", "d0", "|cd", "ab", Normal},
		{"d^", "  ab|cd", "d^", "  |cd", "ab", Normal},
		{"dl", "a|bc", "dl", "a|c", "b", Normal},
		{"dh", "ab|c", "dh", "a|c", "b", Normal},
		{"dj linewise", "a\n|b\nc\nd", "dj", "a\n|d", "L:b\nc\n", Normal},
		{"dk linewise", "a\nb\n|c\nd", "dk", "a\n|d", "L:b\nc\n", Normal},
		{"dj on last line fails", "a\n|b", "dj", "a\n|b", "", Normal},
		{"dG", "a\n|b\nc", "dG", "|a", "L:b\nc\n", Normal},
		{"dgg", "a\nb\n|c\nd", "dgg", "|d", "L:a\nb\nc\n", Normal},
		{"dG all", "|a\nb", "dG", "|", "L:a\nb\n", Normal},
		{"dt)", "f(|abc)", "dt)", "f(|)", "abc", Normal},
		{"df,", "|a, b", "df,", "| b", "a,", Normal},
		{"dF", "abc|d", "dFa", "|d", "abc", Normal},
		{"dT", "abc|d", "dTa", "a|d", "bc", Normal},
		{"d} from para start linewise", "|a\nb\n\nc", "d}", "|\nc", "L:a\nb\n", Normal},
		{"d} mid-line charwise", "x|yz\nb\n\nc", "d}", "|x\n\nc", "yz\nb", Normal},
		{"d} to end of buffer", "a\n\n|b\nc", "d}", "a\n|", "L:b\nc\n", Normal},
		{"d{", "a\n\nb\n|c", "d{", "a\n|c", "", Normal},
		{"d%", "|(a b) c", "d%", "| c", "(a b)", Normal},
		{"dd", "a\n|b\nc", "dd", "a\n|c", "L:b\n", Normal},
		{"dd last line", "a\n|b", "dd", "|a", "L:b\n", Normal},
		{"dd only line", "|abc", "dd", "|", "L:abc\n", Normal},
		{"3dd", "|a\nb\nc\nd", "3dd", "|d", "L:a\nb\nc\n", Normal},
		{"d3d", "|a\nb\nc\nd", "d3d", "|d", "L:a\nb\nc\n", Normal},
		{"5dd clamps", "a\n|b\nc", "5dd", "|a", "L:b\nc\n", Normal},
		{"dd cursor first nonblank", "|a\n  b", "dd", "  |b", "", Normal},
		{"d2j", "|a\nb\nc\nd", "d2j", "|d", "", Normal},
		// c + motion
		{"cw like ce", "|foo bar", "cwx<esc>", "|x bar", "foo", Normal},
		{"cw keeps insert", "|foo bar", "cw", "| bar", "foo", Insert},
		{"cw on single char word", "|a b", "cwx<esc>", "|x b", "a", Normal},
		{"cw on last char of word", "fo|o bar", "cwX<esc>", "fo|X bar", "o", Normal},
		{"c2w", "|a b c", "c2wX<esc>", "|X c", "a b", Normal},
		{"cw on blank", "a|  b", "cw", "a|b", "  ", Insert},
		{"cW", "|a.b c", "cWX<esc>", "|X c", "a.b", Normal},
		{"ce", "|foo bar", "ceX<esc>", "|X bar", "foo", Normal},
		{"c$", "a|bc", "c$X<esc>", "a|X", "bc", Normal},
		{"C", "a|bc", "CX<esc>", "a|X", "bc", Normal},
		{"cc keeps indent", "  a|b\nc", "ccX<esc>", "  |X\nc", "L:  ab\n", Normal},
		{"S", "  a|b", "SX<esc>", "  |X", "L:  ab\n", Normal},
		{"2cc", "|a\nb\nc", "2ccX<esc>", "|X\nc", "L:a\nb\n", Normal},
		{"cj", "|a\nb\nc", "cjX<esc>", "|X\nc", "", Normal},
		{"ct", "|abc)", "ct)X<esc>", "|X)", "abc", Normal},
		{"s", "a|bc", "sX<esc>", "a|Xc", "b", Normal},
		{"3s", "a|bcde", "3sX<esc>", "a|Xe", "bcd", Normal},
		{"s on empty line", "|", "sX<esc>", "|X", "", Normal},
		// y
		{"yw", "|foo bar", "yw", "|foo bar", "foo ", Normal},
		{"yiw-like yb moves", "foo ba|r", "yb", "foo |bar", "ba", Normal},
		{"yy", "a\n|b\nc", "yy", "a\n|b\nc", "L:b\n", Normal},
		{"Y", "|ab", "Y", "|ab", "L:ab\n", Normal},
		{"2yy", "|a\nb\nc", "2yy", "|a\nb\nc", "L:a\nb\n", Normal},
		{"yk moves up", "a\n|b", "yk", "|a\nb", "L:a\nb\n", Normal},
		{"y$", "a|bc", "y$", "a|bc", "bc", Normal},
		{"ye", "|foo bar", "ye", "|foo bar", "foo", Normal},
		{"yt", "|abc)", "yt)", "|abc)", "abc", Normal},
		// x X
		{"x", "a|bc", "x", "a|c", "b", Normal},
		{"3x", "a|bcde", "3x", "a|e", "bcd", Normal},
		{"x at end moves left", "ab|c", "x", "a|b", "c", Normal},
		{"5x clamps to line", "a|bc\nd", "5x", "|a\nd", "bc", Normal},
		{"x on empty line", "|\na", "x", "|\na", "", Normal},
		{"del key", "a|bc", "<del>", "a|c", "b", Normal},
		{"X", "ab|c", "X", "a|c", "b", Normal},
		{"2X", "abc|d", "2X", "a|d", "bc", Normal},
		{"X at col 0", "|ab", "X", "|ab", "", Normal},
		// r
		{"r", "a|bc", "rx", "a|xc", "", Normal},
		{"3r", "|abcd", "3rx", "xx|xd", "", Normal},
		{"r too many", "a|bc", "5rx", "a|bc", "", Normal},
		{"r cr", "a|bc", "r<cr>", "a\n|c", "", Normal},
		{"r space", "a|bc", "r<space>", "a| c", "", Normal},
		{"r esc cancels", "a|bc", "r<esc>x", "a|c", "", Normal},
		// J
		{"J", "|a\nb", "J", "a| b", "", Normal},
		{"J strips indent", "|a\n    b", "J", "a| b", "", Normal},
		{"J on indented line", "  |a\n    b c", "J", "  a| b c", "", Normal},
		{"3J", "|a\nb\nc\nd", "3J", "a b| c\nd", "", Normal},
		{"J blank next", "|a\n   ", "J", "|a", "", Normal},
		{"J trailing space", "|a \nb", "J", "a |b", "", Normal},
		{"J paren", "|f(\n)", "J", "f(|)", "", Normal},
		{"J last line", "a\n|b", "J", "a\n|b", "", Normal},
		// > <
		{">>", "|a\nb", ">>", "  |a\nb", "", Normal},
		{">> tab line", "\t|a", ">>", "\t\t|a", "", Normal},
		{"<<", "    |a", "<<", "  |a", "", Normal},
		{"<< tab", "\t\t|a", "<<", "\t|a", "", Normal},
		{"<< one space", " |a", "<<", "|a", "", Normal},
		{"<< nothing", "|a", "<<", "|a", "", Normal},
		{"3>>", "|a\nb\nc\nd", "3>>", "  |a\n  b\n  c\nd", "", Normal},
		{">j", "|a\nb\nc", ">j", "  |a\n  b\nc", "", Normal},
		{">> skips empty", "|a\n\nb", "3>>", "  |a\n\n  b", "", Normal},
		{">ip-like >}", "|a\nb\n\nc", ">}", "  |a\n  b\n\nc", "", Normal},
		// ~
		{"~", "|abC", "~~~", "AB|c", "", Normal},
		{"3~", "|abcd", "3~", "ABC|d", "", Normal},
	})
}

func TestPut(t *testing.T) {
	runOpCases(t, []opCase{
		{"yyp", "|a\nb", "yyp", "a\n|a\nb", "L:a\n", Normal},
		{"yyP", "a\n|b", "yyP", "a\n|b\nb", "L:b\n", Normal},
		{"yyP first line", "|a", "yyP", "|a\na", "", Normal},
		{"yy3p", "|a", "yy3p", "a\n|a\na\na", "", Normal},
		{"ddp swaps lines", "|a\nb", "ddp", "b\n|a", "", Normal},
		{"linewise p indented", "|  a\nb", "yyjp", "  a\nb\n  |a", "", Normal},
		{"xp swaps chars", "|ab", "xp", "b|a", "a", Normal},
		{"yw P", "|foo bar", "ywP", "foo| foo bar", "", Normal},
		{"p charwise", "|ab", "ylp", "a|ab", "", Normal},
		{"2p charwise", "|ab", "yl2p", "aa|ab", "", Normal},
		{"p on empty line", "a\n|\nz", "kyljp", "a\n|a\nz", "", Normal},
		{"p nothing", "|a", "p", "|a", "", Normal},
		{"dwP restores", "|foo bar", "dwP", "foo| bar", "foo ", Normal},
	})
}

func TestUndoRedo(t *testing.T) {
	runOpCases(t, []opCase{
		{"undo dd", "a\n|b\nc", "ddu", "a\n|b\nc", "", Normal},
		{"undo dd last line", "a\n|b", "ddu", "a\n|b", "", Normal},
		{"undo dw", "x |foo bar", "dwu", "x |foo bar", "", Normal},
		{"undo insert session", "a|b", "ixyz<esc>u", "a|b", "", Normal},
		{"undo A", "|ab", "A;<esc>u", "|ab", "", Normal},
		{"redo", "a\n|b\nc", "ddu<c-r>", "a\n|c", "", Normal},
		{"undo twice", "|abc", "xxuu", "|abc", "", Normal},
		{"2u", "|abc", "xx2u", "|abc", "", Normal},
		{"undo cw session one step", "|foo bar", "cwxyz<esc>u", "|foo bar", "", Normal},
		{"undo 3dd", "|a\nb\nc\nd", "3ddu", "|a\nb\nc\nd", "", Normal},
		{"undo J", "|a\nb", "Ju", "|a\nb", "", Normal},
		{"undo o", "|a", "ob<esc>u", "|a", "", Normal},
		{"undo nothing", "|a", "u", "|a", "", Normal},
		{"redo nothing", "|a", "<c-r>", "|a", "", Normal},
		{"undo redo undo", "|abc", "xu<c-r>u", "|abc", "", Normal},
	})
}

func TestUndoCursorAcrossLevels(t *testing.T) {
	m := New()
	b := newBuf("one\ntw|o\nthree")
	feed(m, b, "dd") // cursor lands on "three"
	feed(m, b, "$x") // cursor on the last char of "three"
	feed(m, b, "u")
	if got := show(b); got != "one\nthre|e" {
		t.Errorf("first undo: %q", got)
	}
	feed(m, b, "u")
	if got := show(b); got != "one\ntw|o\nthree" {
		t.Errorf("second undo: %q", got)
	}
	// Redo then undo again keeps the restored cursor.
	feed(m, b, "<c-r>u")
	if got := show(b); got != "one\ntw|o\nthree" {
		t.Errorf("undo after redo: %q", got)
	}
}

func TestUndoCursorTwoBuffers(t *testing.T) {
	m := New()
	b1 := newBuf("a\nb|b\nc")
	b2 := newBuf("x|yz\nw")
	feed(m, b1, "dd")
	feed(m, b2, "lx")
	feed(m, b1, "u")
	if got := show(b1); got != "a\nb|b\nc" {
		t.Errorf("b1 undo: %q", got)
	}
	feed(m, b2, "u")
	if got := show(b2); got != "xy|z\nw" {
		t.Errorf("b2 undo: %q", got)
	}
}

func TestUndoMessages(t *testing.T) {
	_, _, eff := run(t, "|a", "u")
	if eff.Message != "Already at oldest change" {
		t.Errorf("Message = %q", eff.Message)
	}
	_, _, eff = run(t, "|a", "<c-r>")
	if eff.Message != "Already at newest change" {
		t.Errorf("Message = %q", eff.Message)
	}
}

func TestOperatorIsOneUndoStep(t *testing.T) {
	for _, keys := range []string{"3dd", "d2w", "cwxyz<esc>", "3J", "3>>", "yy3p", "5x", "3rx"} {
		m, b, _ := run(t, "|a b c\nd e f\ng h i\nj k l", keys)
		_ = m
		before := "a b c\nd e f\ng h i\nj k l"
		if b.String() == before {
			t.Errorf("%q changed nothing", keys)
			continue
		}
		b.Undo()
		if got := b.String(); got != before {
			t.Errorf("%q: after one undo %q", keys, got)
		}
	}
}

func TestClipboardRegister(t *testing.T) {
	tests := []struct {
		name, in, keys, want, clip string
	}{
		{"\"+yy", "|ab\ncd", `"+yy`, "|ab\ncd", "ab\n"},
		{"\"+yw", "|ab cd", `"+yw`, "|ab cd", "ab "},
		{"\"+dd", "|ab\ncd", `"+dd`, "|cd", "ab\n"},
		{"\"+x", "|ab", `"+x`, "|b", "a"},
		{"\"*y$ alias", "|ab", `"*y$`, "|ab", "ab"},
		{"\"+2yy", "|a\nb\nc", `"+2yy`, "|a\nb\nc", "a\nb\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, b, eff := run(t, tt.in, tt.keys)
			if got := show(b); got != tt.want {
				t.Errorf("text = %q, want %q", got, tt.want)
			}
			if eff.Clipboard == nil || *eff.Clipboard != tt.clip {
				t.Errorf("Clipboard = %v, want %q", eff.Clipboard, tt.clip)
			}
			if text, _ := m.Register(); text != tt.clip {
				t.Errorf("unnamed = %q, want %q", text, tt.clip)
			}
		})
	}
	// Plain yank does not touch the clipboard.
	_, _, eff := run(t, "|ab", "yy")
	if eff.Clipboard != nil {
		t.Errorf("yy set Clipboard %q", *eff.Clipboard)
	}
}

func TestClipboardPaste(t *testing.T) {
	tests := []struct {
		name, in, keys, clip, want string
		before                     bool
	}{
		{"\"+p charwise", "|ab", `"+p`, "XY", "aX|Yb", false},
		{"\"+P charwise", "a|b", `"+P`, "XY", "aX|Yb", true},
		{"\"+p linewise", "|ab\ncd", `"+p`, "XY\n", "ab\n|XY\ncd", false},
		{"\"+P linewise", "ab\n|cd", `"+P`, "XY\n", "ab\n|XY\ncd", true},
		{"\"+2p", "|a", `"+2p`, "X", "aX|X", false},
		{"insert c-v", "a|b", "i<c-v>", "XY", "aXY|b", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, b, eff := run(t, tt.in, tt.keys)
			if !eff.NeedClipboard {
				t.Fatalf("NeedClipboard not set")
			}
			if eff.PasteBefore != tt.before {
				t.Errorf("PasteBefore = %v", eff.PasteBefore)
			}
			m.PasteClipboard(b, tt.clip, eff.PasteBefore)
			if got := show(b); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
