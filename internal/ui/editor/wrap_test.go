package editor

import (
	"slices"
	"strings"
	"testing"

	"github.com/mathieucroset/notty/internal/buffer"
)

// rowsText renders the wrapped rows of s joined by "|".
func rowsText(s string, width int) string {
	gs := buffer.Graphemes(s)
	starts := wrapGraphemes(gs, width)
	var parts []string
	for r := range starts {
		parts = append(parts, strings.Join(gs[starts[r]:rowEnd(starts, r, len(gs))], ""))
	}
	return strings.Join(parts, "|")
}

func TestWrapGraphemes(t *testing.T) {
	tests := []struct {
		name, in string
		width    int
		want     string
	}{
		{"empty", "", 10, ""},
		{"fits", "hello", 10, "hello"},
		{"exact", "hello", 5, "hello"},
		{"word wrap", "hello world foo", 11, "hello world |foo"},
		{"word wrap keeps words", "aaa bbb ccc ddd", 8, "aaa bbb |ccc ddd"},
		{"hanging space", "hello world", 5, "hello |world"},
		{"hard break", "abcdefghij", 4, "abcd|efgh|ij"},
		{"long word after short", "a bcdefghij", 4, "a |bcde|fghi|j"},
		{"cjk", "日本語のテキスト", 6, "日本語|のテキ|スト"},
		{"cjk odd width", "日本語", 5, "日本|語"},
		{"emoji", "ab😀cd😀", 4, "ab😀|cd😀"},
		{"emoji zwj", "👩‍👩‍👧x", 2, "👩‍👩‍👧|x"},
		{"tab", "\tab", 4, "\t|ab"},
		{"tab is a break", "a\tbcdef", 8, "a\t|bcdef"},
		{"trailing hanging space adds a row", "abcd ", 4, "abcd |"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := rowsText(tt.in, tt.width); got != tt.want {
				t.Errorf("rows(%q, %d) = %q, want %q", tt.in, tt.width, got, tt.want)
			}
		})
	}
}

func TestCellX(t *testing.T) {
	tests := []struct {
		name, in string
		width    int
		col      int
		wantRow  int
		wantX    int
	}{
		{"start", "hello world", 20, 0, 0, 0},
		{"mid", "hello world", 20, 3, 0, 3},
		{"eol", "hello", 20, 5, 0, 5},
		{"second row", "hello world", 6, 7, 1, 1},
		{"eol full row", "abcd", 4, 4, 0, 4},
		{"cjk", "日本語", 10, 2, 0, 4},
		{"cjk wrapped", "日本語", 4, 2, 1, 0},
		{"tab", "\tx", 10, 1, 0, 4},
		{"tab after char", "a\tx", 10, 2, 0, 4},
		{"hanging space", "hello world", 5, 5, 0, 5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gs := buffer.Graphemes(tt.in)
			starts := wrapGraphemes(gs, tt.width)
			if r := rowOf(starts, tt.col); r != tt.wantRow {
				t.Errorf("row = %d, want %d", r, tt.wantRow)
			}
			if x := cellX(gs, starts, tt.col, tt.width); x != tt.wantX {
				t.Errorf("x = %d, want %d", x, tt.wantX)
			}
		})
	}
}

// TestRoundTrip checks that every grapheme maps to a cell that maps back to
// the same grapheme.
func TestRoundTrip(t *testing.T) {
	lines := []string{
		"the quick brown fox jumps over the lazy dog",
		"日本語のテキストと english mixed 😀 text",
		"\tindented\twith\ttabs and words",
		"👩‍👩‍👧 family é combining",
	}
	for _, line := range lines {
		for _, width := range []int{3, 5, 8, 13, 40} {
			gs := buffer.Graphemes(line)
			starts := wrapGraphemes(gs, width)
			for col := range gs {
				r := rowOf(starts, col)
				x := cellX(gs, starts, col, width)
				if x > width {
					t.Errorf("%q w=%d col %d: x=%d beyond the text area", line, width, col, x)
				}
				if back := colAtCell(gs, starts, r, x, width); back != col {
					t.Errorf("%q w=%d: col %d -> (%d,%d) -> col %d", line, width, col, r, x, back)
				}
			}
		}
	}
}

func TestWrapNavigator(t *testing.T) {
	b := buffer.New("0123456789abcdefghij\nshort\n日本語日本語")
	l := &layout{width: 8}
	tests := []struct {
		name string
		from buffer.Pos
		n    int
		want buffer.Pos
	}{
		{"down within line", buffer.Pos{Line: 0, Col: 2}, 1, buffer.Pos{Line: 0, Col: 10}},
		{"down to last row", buffer.Pos{Line: 0, Col: 2}, 2, buffer.Pos{Line: 0, Col: 18}},
		{"down across lines", buffer.Pos{Line: 0, Col: 18}, 1, buffer.Pos{Line: 1, Col: 2}},
		{"down clamps column", buffer.Pos{Line: 0, Col: 15}, 2, buffer.Pos{Line: 1, Col: 4}},
		{"down into cjk", buffer.Pos{Line: 1, Col: 3}, 1, buffer.Pos{Line: 2, Col: 1}},
		{"down at end stays", buffer.Pos{Line: 2, Col: 5}, 5, buffer.Pos{Line: 2, Col: 5}},
		{"up within line", buffer.Pos{Line: 0, Col: 12}, 1, buffer.Pos{Line: 0, Col: 4}},
		{"up across lines", buffer.Pos{Line: 1, Col: 1}, 1, buffer.Pos{Line: 0, Col: 17}},
		{"up at top stays", buffer.Pos{Line: 0, Col: 3}, 2, buffer.Pos{Line: 0, Col: 3}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got buffer.Pos
			if tt.n >= 0 && !strings.HasPrefix(tt.name, "up") {
				got = l.Down(b, tt.from, tt.n)
			} else {
				got = l.Up(b, tt.from, tt.n)
			}
			if got != tt.want {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

// fixedRows is a rowCounter where every line i has rows[i] rows.
func fixedRows(rows []int) rowCounter { return func(l int) int { return rows[l] } }

func TestScrollMargin(t *testing.T) {
	ones := slices.Repeat([]int{1}, 100)
	tests := []struct {
		name   string
		top    anchor
		cur    anchor
		height int
		rows   []int
		want   anchor
	}{
		{"cursor visible, no scroll", anchor{0, 0}, anchor{5, 0}, 20, ones, anchor{0, 0}},
		{"near top of doc stays", anchor{0, 0}, anchor{1, 0}, 20, ones, anchor{0, 0}},
		{"down into bottom margin", anchor{0, 0}, anchor{17, 0}, 20, ones, anchor{1, 0}},
		{"far jump down", anchor{0, 0}, anchor{80, 0}, 20, ones, anchor{64, 0}},
		{"up into top margin", anchor{50, 0}, anchor{52, 0}, 20, ones, anchor{49, 0}},
		{"above top", anchor{50, 0}, anchor{10, 0}, 20, ones, anchor{7, 0}},
		{"end of doc has no blank margin", anchor{0, 0}, anchor{99, 0}, 20, ones, anchor{80, 0}},
		{"wrapped lines", anchor{0, 0}, anchor{4, 1}, 10, []int{3, 3, 3, 3, 3, 3}, anchor{2, 1}},
		{"tiny height", anchor{0, 0}, anchor{10, 0}, 1, ones, anchor{10, 0}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := scroll(tt.top, tt.cur, tt.height, len(tt.rows), fixedRows(tt.rows))
			if got != tt.want {
				t.Errorf("scroll = %+v, want %+v", got, tt.want)
			}
		})
	}
}
