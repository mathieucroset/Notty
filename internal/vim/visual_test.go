package vim

import (
	"testing"

	"github.com/mathieucroset/notty/internal/buffer"
)

func TestVisualOps(t *testing.T) {
	runOpCases(t, []opCase{
		{"vd", "a|bcd", "vld", "a|d", "bc", Normal},
		{"vx", "a|bcd", "vlx", "a|d", "bc", Normal},
		{"v backwards", "abc|d", "vhhd", "|a", "bcd", Normal},
		{"v across lines", "a|bc\nde", "vjd", "|a", "bc\nde", Normal},
		{"v$d", "a|bc\nd", "v$d", "|a\nd", "bc", Normal},
		{"v on empty line takes newline", "a\n|\nb", "vd", "a\n|b", "\n", Normal},
		{"vy", "a|bcd", "vly", "a|bcd", "bc", Normal},
		{"vy backwards moves to start", "abc|d", "vhy", "ab|cd", "cd", Normal},
		{"vc", "a|bcd", "vlcX<esc>", "a|Xd", "bc", Normal},
		{"vc insert mode", "a|bcd", "vlc", "a|d", "bc", Insert},
		{"vw extends", "|foo bar baz", "vwd", "|ar baz", "foo b", Normal},
		{"ve", "|foo bar", "ved", "| bar", "foo", Normal},
		{"v count motion", "|abcdef", "v3ld", "|ef", "abcd", Normal},
		{"vo swaps", "ab|cd", "vlohd", "|a", "bcd", Normal},
		{"v esc", "a|bc", "vl<esc>", "ab|c", "", Normal},
		{"v c-c", "a|bc", "vl<c-c>", "ab|c", "", Normal},
		{"v v exits", "a|bc", "vlv", "ab|c", "", Normal},
		{"Vd", "a\n|b\nc", "Vd", "a\n|c", "L:b\n", Normal},
		{"Vjd", "a\n|b\nc\nd", "Vjd", "a\n|d", "L:b\nc\n", Normal},
		{"Vkd", "a\nb\n|c\nd", "Vkd", "a\n|d", "L:b\nc\n", Normal},
		{"Vy", "a\n|b\nc", "Vjy", "a\n|b\nc", "L:b\nc\n", Normal},
		{"Vc", "  a|b\nc", "VcX<esc>", "  |X\nc", "L:  ab\n", Normal},
		{"V>", "|a\nb\nc", "Vj>", "  |a\n  b\nc", "", Normal},
		{"V2>", "|a", "V2>", "    |a", "", Normal},
		{"V<", "    |a\n  b", "Vj<", "  |a\nb", "", Normal},
		{"v> is linewise", "a|b\nc", "v>", "  |ab\nc", "", Normal},
		{"VJ", "|a\n  b\nc", "VjJ", "a| b\nc", "", Normal},
		{"VJ three lines", "|a\nb\nc", "VjjJ", "a b| c", "", Normal},
		{"vJ single line joins two", "|a\nb", "vJ", "a| b", "", Normal},
		{"v to V", "a|b\nc", "vVd", "|c", "L:ab\n", Normal},
		{"V to v", "a|bc", "Vvld", "|a", "bc", Normal},
		{"vX linewise", "a|b\nc", "vX", "|c", "L:ab\n", Normal},
		{"vD linewise", "a|b\nc", "vD", "|c", "L:ab\n", Normal},
		{"vY linewise", "a|b\nc", "vY", "a|b\nc", "L:ab\n", Normal},
		{"v~", "|abC", "v$~", "|ABc", "", Normal},
		{"vU", "|abc", "vlU", "|ABc", "", Normal},
		{"vu", "|ABC", "vlu", "|abC", "", Normal},
		{"viw", "foo b|ar baz", "viwd", "foo | baz", "bar", Normal},
		{"vaw", "foo b|ar baz", "vawd", "foo |baz", "bar ", Normal},
		{"vi(", "f(a|b)", "vi(d", "f(|)", "ab", Normal},
		{"vip becomes linewise", "a\n|b\n\nc", "vipd", "|\nc", "L:a\nb\n", Normal},
		{"visual gg", "a\nb\n|c", "vggd", "|", "", Normal},
		{"p multiline charwise", "|a\nb", "vjy$p", "a|a\nb\nb", "a\nb", Normal},
		{"visual undo is one step", "a|bcd", "vlldu", "a|bcd", "", Normal},
		{"vr replaces each char", "a|bcd", "vllrx", "a|xxx", "", Normal},
		{"vr across lines keeps breaks", "a|b\ncd", "vjrx", "a|x\nxx", "", Normal},
		{"Vr replaces whole lines", "a|b\ncd\nef", "Vjr-", "|--\n--\nef", "", Normal},
		{"vr unicode", "|日本", "vlr*", "|**", "", Normal},
		{"vr undo", "a|bcd", "vllrxu", "a|bcd", "", Normal},
		{"vr dot repeat", "|abcdef", "vlrxll.", "xx|xxef", "", Normal},
		{"vr esc cancels", "a|bcd", "vlr<esc>", "ab|cd", "", Visual},
		{"v c-w ignored", "a|bcd", "vl<c-w>h", "ab|cd", "", Visual},
		{"v c-w keeps selection", "a|bcd", "vl<c-w>ld", "a|d", "bc", Normal},
	})
}

func TestVisualClipboard(t *testing.T) {
	_, _, eff := run(t, "a|bc", `vl"+y`)
	if eff.Clipboard == nil || *eff.Clipboard != "bc" {
		t.Errorf("Clipboard = %v", eff.Clipboard)
	}
}

func TestSelection(t *testing.T) {
	R := func(l1, c1, l2, c2 int) buffer.Range { return buffer.Range{Start: pos(l1, c1), End: pos(l2, c2)} }
	tests := []struct {
		name, in, keys string
		want           buffer.Range
		ok             bool
	}{
		{"normal has none", "a|bc", "", buffer.Range{}, false},
		{"charwise", "a|bcd", "vl", R(0, 1, 0, 3), true},
		{"charwise backwards", "abc|d", "vh", R(0, 2, 0, 4), true},
		{"multiline", "a|b\ncd", "vj", R(0, 1, 1, 2), true},
		{"linewise", "a|b\ncd\nef", "Vj", R(0, 0, 2, 0), true},
		{"linewise last line", "ab\n|cd", "V", R(1, 0, 2, 0), true},
		{"empty line", "|\nab", "v", R(0, 0, 1, 0), true},
		{"after esc", "a|bc", "vl<esc>", buffer.Range{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, b, _ := run(t, tt.in, tt.keys)
			got, ok := m.Selection(b)
			if ok != tt.ok || got != tt.want {
				t.Errorf("Selection = %v %v, want %v %v", got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestVisualModeNames(t *testing.T) {
	m, b, _ := run(t, "|ab", "v")
	if m.ModeName() != "VISUAL" || m.Mode() != Visual {
		t.Errorf("mode %q", m.ModeName())
	}
	feed(m, b, "V")
	if m.ModeName() != "V-LINE" || m.Mode() != VisualLine {
		t.Errorf("mode %q", m.ModeName())
	}
}

func TestDotRepeat(t *testing.T) {
	runOpCases(t, []opCase{
		{"dw.", "|a b c d", "dw.", "|c d", "", Normal},
		{"dw 3.", "|a b c d e", "dw3.", "|e", "", Normal},
		{"count replaces original", "|a b c d e f g", "2dw.", "|e f g", "", Normal},
		{"count stays for next dot", "|a b c d e f g", "dw2..", "|f g", "", Normal},
		{"x.", "|abcd", "x..", "|d", "", Normal},
		{"3x.", "|abcdefg", "3x.", "|g", "", Normal},
		{"dd.", "|a\nb\nc", "dd.", "|c", "", Normal},
		{"cw text .", "|foo bar baz", "cwxy<esc>w.", "xy x|y baz", "", Normal},
		{"ciw text .", "|foo bar", "ciwZ<esc>w.", "Z |Z", "", Normal},
		{"A; .", "|a\nb", "A;<esc>j.", "a;\nb|;", "", Normal},
		{"i text .", "|ab", "iX<esc>.", "|XXab", "", Normal},
		{"a text .", "|ab", "aX<esc>.", "aX|Xb", "", Normal},
		{"3i .", "|x", "3ia<esc>.", "aaaa|aax", "", Normal},
		{"o text .", "|a", "ob<esc>.", "a\nb\n|b", "", Normal},
		{"p .", "|a", "yyp.", "a\na\n|a", "", Normal},
		{"J .", "|a\nb\nc", "J.", "a b| c", "", Normal},
		{">> .", "|a", ">>.", "    |a", "", Normal},
		{"r .", "|abcd", "rxl.", "x|xcd", "", Normal},
		{"~ .", "|abcd", "~.", "AB|cd", "", Normal},
		{"D .", "a|b\ncd", "Dj.", "a\n|c", "", Normal},
		{"dt .", "|a,b,c", "dt,l.", ",|,c", "", Normal},
		{"visual d .", "|abcdef", "vld.", "|ef", "", Normal},
		{"visual V> .", "|a\nb", "Vj>.", "    |a\n    b", "", Normal},
		{"visual c .", "|abcdef", "vlcX<esc>l.", "X|Xef", "", Normal},
		{"motion does not reset dot", "|a b c d", "dwwx.", "b |d", "", Normal},
		{"yank does not reset dot", "|abcd", "xyl.", "|cd", "", Normal},
		{"undo does not reset dot", "|abcd", "xxu.", "|cd", "", Normal},
		{"dot with nothing", "|ab", ".", "|ab", "", Normal},
		{"dot undo is one step", "|foo bar", "cwxy<esc>w.u", "xy |bar", "", Normal},
		{"insert backspace replayed", "|ab", "iXY<bs><esc>l.", "X|Xab", "", Normal},
	})
}
