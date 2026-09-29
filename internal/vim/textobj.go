package vim

import (
	"strings"

	"github.com/mathieucroset/notty/internal/buffer"
)

// textObjectKeys are the keys after i / a that name a text object.
const textObjectKeys = "wWp\"'`()b[]{}B<>"

// isTextObject reports whether name is a text object such as "iw" or "a(".
func isTextObject(name string) bool {
	return len(name) == 2 && (name[0] == 'i' || name[0] == 'a') &&
		strings.IndexByte(textObjectKeys, name[1]) >= 0
}

// parseTextObject parses (i | a) key at toks[i].
func parseTextObject(toks []string, i int, c cmd) (cmd, parseStatus) {
	if i+1 >= len(toks) {
		return c, parseMore
	}
	name := toks[i] + toks[i+1]
	if !isTextObject(name) {
		return c, parseBad
	}
	c.name = name
	return c, parseOK
}

// blockPairs maps the text object key to its bracket pair.
var blockPairs = map[byte][2]string{
	'(': {"(", ")"}, ')': {"(", ")"}, 'b': {"(", ")"},
	'[': {"[", "]"}, ']': {"[", "]"},
	'{': {"{", "}"}, '}': {"{", "}"}, 'B': {"{", "}"},
	'<': {"<", ">"}, '>': {"<", ">"},
}

// textObject computes the range of a text object at from. The result is
// linewise for paragraphs (and for bracket contents that are whole lines,
// following vim's exclusive-motion rules).
func (m *Machine) textObject(b *buffer.Buffer, c cmd, from buffer.Pos) (buffer.Range, bool, bool) {
	t := newText(b)
	around := c.name[0] == 'a'
	key := c.name[1]
	n := c.count()
	var r buffer.Range
	var ok bool
	switch key {
	case 'w', 'W':
		r, ok = t.wordObject(from, n, key == 'W', around)
	case '"', '\'', '`':
		r, ok = t.quoteObject(from, string(key), around)
	case 'p':
		l1, l2, found := t.paragraphObject(from, n, around)
		if !found {
			return r, false, false
		}
		return lines(b, l1, l2), true, true
	default:
		pr := blockPairs[key]
		r, ok = t.blockObject(from, pr[0], pr[1], n, around)
	}
	if !ok {
		return r, false, false
	}
	r, lw := adjustExclusive(b, r.Start, r.End)
	return r, lw, true
}

// wordObject implements iw / aw (and iW / aW) within the cursor line.
func (t *text) wordObject(p buffer.Pos, n int, big, around bool) (buffer.Range, bool) {
	gs := t.g(p.Line)
	if len(gs) == 0 {
		return buffer.Range{}, false
	}
	cls := func(i int) int { return charClass(gs[i], big) }
	col := min(p.Col, len(gs)-1)
	start, end := col, col+1
	for start > 0 && cls(start-1) == cls(col) {
		start--
	}
	for end < len(gs) && cls(end) == cls(col) {
		end++
	}
	skipRun := func() {
		if end < len(gs) {
			c := cls(end)
			for end < len(gs) && cls(end) == c {
				end++
			}
		}
	}
	if around {
		if cls(col) == clsBlank {
			skipRun() // the following word
		} else if end < len(gs) && cls(end) == clsBlank {
			skipRun() // trailing blanks
		} else {
			for start > 0 && cls(start-1) == clsBlank {
				start--
			}
		}
	}
	for ; n > 1 && end < len(gs); n-- {
		if around {
			if cls(end) == clsBlank {
				skipRun()
				skipRun()
			} else {
				skipRun()
				if end < len(gs) && cls(end) == clsBlank {
					skipRun()
				}
			}
		} else {
			skipRun()
		}
	}
	return buffer.Range{Start: pos(p.Line, start), End: pos(p.Line, end)}, true
}

// quoteObject implements i" / a" (and ' `) within the cursor line. When the
// cursor is not inside quotes, the first quoted string after it is used.
func (t *text) quoteObject(p buffer.Pos, q string, around bool) (buffer.Range, bool) {
	gs := t.g(p.Line)
	var qs []int
	for i, g := range gs {
		if g == q && (i == 0 || gs[i-1] != `\`) {
			qs = append(qs, i)
		}
	}
	open, closing := -1, -1
	idx, before := -1, 0
	for i, c := range qs {
		if c == p.Col {
			idx = i
		}
		if c < p.Col {
			before++
		}
	}
	switch {
	case idx >= 0 && idx%2 == 0 && idx+1 < len(qs):
		open, closing = qs[idx], qs[idx+1]
	case idx >= 0 && idx%2 == 1:
		open, closing = qs[idx-1], qs[idx]
	case idx < 0 && before%2 == 1 && before < len(qs):
		open, closing = qs[before-1], qs[before]
	case idx < 0 && before%2 == 0 && before+1 < len(qs):
		open, closing = qs[before], qs[before+1]
	}
	if open < 0 {
		return buffer.Range{}, false
	}
	if !around {
		return buffer.Range{Start: pos(p.Line, open+1), End: pos(p.Line, closing)}, true
	}
	start, end := open, closing+1
	if end < len(gs) && charClass(gs[end], false) == clsBlank {
		for end < len(gs) && charClass(gs[end], false) == clsBlank {
			end++
		}
	} else {
		for start > 0 && charClass(gs[start-1], false) == clsBlank {
			start--
		}
	}
	return buffer.Range{Start: pos(p.Line, start), End: pos(p.Line, end)}, true
}

// findUnmatchedOpen searches backward from q (exclusive) for an open bracket
// that is not closed before q.
func (t *text) findUnmatchedOpen(q buffer.Pos, open, closing string) (buffer.Pos, bool) {
	depth := 0
	for t.dec(&q) != -1 {
		switch t.at(q) {
		case closing:
			depth++
		case open:
			if depth == 0 {
				return q, true
			}
			depth--
		}
	}
	return q, false
}

// blockObject implements i( / a( and the other bracket objects; the
// brackets may be on different lines. n selects the n-th enclosing block.
func (t *text) blockObject(p buffer.Pos, open, closing string, n int, around bool) (buffer.Range, bool) {
	var o buffer.Pos
	var found bool
	switch t.at(p) {
	case open:
		o, found = p, true
	case closing:
		o, found = t.findMatch(p)
	default:
		o, found = t.findUnmatchedOpen(p, open, closing)
	}
	for ; found && n > 1; n-- {
		o, found = t.findUnmatchedOpen(o, open, closing)
	}
	if !found {
		return buffer.Range{}, false
	}
	c, found := t.findMatch(o)
	if !found {
		return buffer.Range{}, false
	}
	if around {
		return buffer.Range{Start: o, End: pos(c.Line, c.Col+1)}, true
	}
	s := o
	if t.inc(&s) >= 1 && s.Col != 0 {
		t.inc(&s) // the open bracket ends its line: start on the next line
	}
	e := c
	if e.Line > s.Line && e.Col <= indentColsText(t, e.Line) {
		e = pos(e.Line, 0) // only indentation before the close bracket
	}
	if e.Less(s) {
		e = s
	}
	return buffer.Range{Start: s, End: e}, true
}

func indentColsText(t *text, l int) int {
	gs := t.g(l)
	i := 0
	for i < len(gs) && (gs[i] == " " || gs[i] == "\t") {
		i++
	}
	return i
}

// paragraphObject implements ip / ap: a run of non-blank lines or of blank
// lines. ap adds the following blank lines (or the preceding ones when there
// are none after); on a blank run it adds the following paragraph.
func (t *text) paragraphObject(p buffer.Pos, n int, around bool) (int, int, bool) {
	last := t.last()
	blank := func(l int) bool { return isBlank(t.b.Line(l)) }
	kind := blank(p.Line)
	l1, l2 := p.Line, p.Line
	for l1 > 0 && blank(l1-1) == kind {
		l1--
	}
	for l2 < last && blank(l2+1) == kind {
		l2++
	}
	nextRun := func() bool {
		if l2 >= last {
			return false
		}
		k := blank(l2 + 1)
		for l2 < last && blank(l2+1) == k {
			l2++
		}
		return true
	}
	if around {
		if !kind && (l2 >= last || !blank(l2+1)) {
			for l1 > 0 && blank(l1-1) {
				l1--
			}
		} else {
			nextRun()
		}
	}
	for ; n > 1; n-- {
		if !nextRun() {
			break
		}
		if around {
			nextRun()
		}
	}
	return l1, l2, true
}
