package editor

// scrollMargin is the number of rows kept between the cursor and the top or
// bottom edge when scrolling (spec §5).
const scrollMargin = 3

// anchor is a screen row identified by its buffer line and the wrapped row
// within that line. The viewport is anchored at its top row, so scrolling
// never needs the layout of the whole document.
type anchor struct{ line, row int }

func (a anchor) less(b anchor) bool {
	return a.line < b.line || (a.line == b.line && a.row < b.row)
}

// rowCounter returns the number of screen rows of a line.
type rowCounter func(line int) int

// back walks n rows up from a, stopping at the first row of the document.
func back(a anchor, n int, rows rowCounter) anchor {
	for ; n > 0; n-- {
		switch {
		case a.row > 0:
			a.row--
		case a.line > 0:
			a.line--
			a.row = rows(a.line) - 1
		default:
			return a
		}
	}
	return a
}

// distance returns the number of rows from a down to b (b not before a),
// giving up with limit+1 once it exceeds limit.
func distance(a, b anchor, limit int, rows rowCounter) int {
	if a.line == b.line {
		return b.row - a.row
	}
	d := rows(a.line) - a.row
	for l := a.line + 1; l < b.line; l++ {
		if d > limit {
			return limit + 1
		}
		d += rows(l)
	}
	return d + b.row
}

// rowsBelow counts the rows after a to the end of the document, up to limit.
func rowsBelow(a anchor, limit, lineCount int, rows rowCounter) int {
	n := rows(a.line) - 1 - a.row
	for l := a.line + 1; l < lineCount && n < limit; l++ {
		n += rows(l)
	}
	return min(n, limit)
}

// clampAnchor keeps a inside the document and its line.
func clampAnchor(a anchor, lineCount int, rows rowCounter) anchor {
	a.line = max(0, min(a.line, lineCount-1))
	a.row = max(0, min(a.row, rows(a.line)-1))
	return a
}

// scroll returns the top anchor that keeps the cursor row cur visible with
// scrollMargin rows around it when possible, moving as little as possible.
func scroll(top, cur anchor, height, lineCount int, rows rowCounter) anchor {
	if height <= 0 {
		return cur
	}
	top = clampAnchor(top, lineCount, rows)
	margin := min(scrollMargin, (height-1)/2)
	if cur.less(top) {
		return back(cur, margin, rows)
	}
	d := distance(top, cur, height, rows)
	if d < margin {
		return back(cur, margin, rows)
	}
	below := min(margin, rowsBelow(cur, margin, lineCount, rows))
	if d > height-1-below {
		return back(cur, height-1-below, rows)
	}
	return top
}
