package editor

import (
	"github.com/mathieucroset/notty/internal/buffer"
	"github.com/mathieucroset/notty/internal/vim"
)

// Soft-wrap layout. A buffer line (or its display form) is a sequence of
// grapheme clusters. It is split into screen rows no wider than the wrap
// width, breaking after the last space that fits when possible. Tabs expand
// to vim.TabWidth stops measured from the start of each screen row.
//
// The text area is one cell wider than the wrap width: that reserved column
// holds the cursor at the end of a full row and a space that "hangs" off the
// end of a row instead of starting the next one.

// cellWidth is the number of cells grapheme g takes when it starts at cell x
// of a row wrapped at width.
func cellWidth(g string, x, width int) int {
	if g == "\t" {
		w := vim.TabWidth - x%vim.TabWidth
		if x+w > width+1 {
			w = max(1, width+1-x)
		}
		return w
	}
	return buffer.DisplayWidth(g)
}

// wrapGraphemes returns the index of the first grapheme of each screen row.
// The result always starts with 0; an empty line has one (empty) row. A row
// ending in a hanging space is followed by a new row even at the end of the
// line, so the end-of-line position always has a cell of its own.
func wrapGraphemes(gs []string, width int) []int {
	width = max(1, width)
	starts := []int{0}
	x, rowStart, lastBreak := 0, 0, -1
	for i := 0; i < len(gs); i++ {
		w := cellWidth(gs[i], x, width)
		if x+w > width && i > rowStart {
			if gs[i] == " " && x < width+1 {
				// Hang the space in the reserved column.
				rowStart, x, lastBreak = i+1, 0, -1
				starts = append(starts, rowStart)
				continue
			}
			brk := i
			if lastBreak > rowStart {
				brk = lastBreak
			}
			starts = append(starts, brk)
			rowStart, lastBreak = brk, -1
			x = 0
			for j := brk; j < i; j++ {
				x += cellWidth(gs[j], x, width)
			}
			i-- // measure gs[i] again on the new row
			continue
		}
		x += w
		if gs[i] == " " || gs[i] == "\t" {
			lastBreak = i + 1
		}
	}
	return starts
}

// rowOf returns the row holding grapheme index col. The end-of-line index
// (len) belongs to the last row.
func rowOf(starts []int, col int) int {
	r := 0
	for i, s := range starts {
		if s <= col {
			r = i
		}
	}
	return r
}

// rowEnd returns the index just past the last grapheme of row r.
func rowEnd(starts []int, r, n int) int {
	if r+1 < len(starts) {
		return starts[r+1]
	}
	return n
}

// cellX returns the cell at which grapheme index col starts within its row.
func cellX(gs []string, starts []int, col, width int) int {
	r := rowOf(starts, col)
	x := 0
	for j := starts[r]; j < col && j < len(gs); j++ {
		x += cellWidth(gs[j], x, width)
	}
	return x
}

// colAtCell returns the grapheme of row r that covers cell x, clamped to the
// last grapheme of the row (a normal-mode cursor position). An empty row
// returns its start.
func colAtCell(gs []string, starts []int, r, x, width int) int {
	start, end := starts[r], rowEnd(starts, r, len(gs))
	c := 0
	for j := start; j < end; j++ {
		c += cellWidth(gs[j], c, width)
		if x < c {
			return j
		}
	}
	if end > start {
		return end - 1
	}
	if start > 0 && start == len(gs) {
		return start - 1 // empty row after a hanging space
	}
	return start
}

// layout is the soft-wrap geometry shared between the model and the wrap
// navigator registered on the vim Machine.
type layout struct {
	width int // wrap width in cells (text area width minus the reserved column)
}

// rawRows wraps buffer line i as typed (no display substitutions).
func (l *layout) rawRows(b *buffer.Buffer, i int) ([]string, []int) {
	gs := buffer.Graphemes(b.Line(i))
	return gs, wrapGraphemes(gs, l.width)
}

// Down implements vim.WrapNavigator: it moves n screen rows down, keeping
// the cell column.
func (l *layout) Down(b *buffer.Buffer, p buffer.Pos, n int) buffer.Pos {
	return l.move(b, p, n)
}

// Up implements vim.WrapNavigator: it moves n screen rows up, keeping the
// cell column.
func (l *layout) Up(b *buffer.Buffer, p buffer.Pos, n int) buffer.Pos {
	return l.move(b, p, -n)
}

func (l *layout) move(b *buffer.Buffer, p buffer.Pos, n int) buffer.Pos {
	p = b.Clamp(p)
	line := p.Line
	gs, starts := l.rawRows(b, line)
	r := rowOf(starts, p.Col)
	x := cellX(gs, starts, p.Col, l.width)
	for ; n > 0; n-- {
		if r+1 < len(starts) {
			r++
		} else if line+1 < b.LineCount() {
			line++
			gs, starts = l.rawRows(b, line)
			r = 0
		} else {
			break
		}
	}
	for ; n < 0; n++ {
		if r > 0 {
			r--
		} else if line > 0 {
			line--
			gs, starts = l.rawRows(b, line)
			r = len(starts) - 1
		} else {
			break
		}
	}
	return buffer.Pos{Line: line, Col: colAtCell(gs, starts, r, x, l.width)}
}
