package vim

import (
	"github.com/mathieucroset/notty/internal/buffer"
)

// motionKind is how an operator treats the text a motion moves over.
type motionKind int

const (
	exclusive motionKind = iota // up to, not including, the target
	inclusive                   // including the character at the target
	linewise                    // whole lines from the start to the target line
)

// motionRes is the result of evaluating a motion.
type motionRes struct {
	pos      buffer.Pos
	kind     motionKind
	ok       bool
	keepWant bool // vertical motion: keep the desired column
	wantEOL  bool // $: stick to the end of line
}

// findState remembers the last f/F/t/T for ; and ,.
type findState struct {
	ch        string
	fwd, till bool
}

// moveCursor applies a motion to the cursor.
func (m *Machine) moveCursor(b *buffer.Buffer, c cmd) {
	r := m.evalMotion(b, c, b.Cursor(), "")
	if !r.ok {
		return
	}
	b.SetCursor(r.pos)
	switch {
	case r.wantEOL:
		m.curswant = wantEOL
	case !r.keepWant:
		m.curswant = cursorCell(b)
	}
}

// vertical returns the position on line l at the desired column.
func (m *Machine) vertical(b *buffer.Buffer, l int) motionRes {
	col := normalCol(b, l, colAtCell(b.Line(l), m.curswant))
	return motionRes{pos: pos(l, col), kind: linewise, ok: true, keepWant: true}
}

// evalMotion computes the target of motion c from p. op is the pending
// operator ("" for a plain cursor move), which changes a few motions the way
// vim does (l may reach the end of line, w stops at the end of line).
func (m *Machine) evalMotion(b *buffer.Buffer, c cmd, p buffer.Pos, op string) motionRes {
	t := newText(b)
	n := c.count()
	last := t.last()
	ok := func(q buffer.Pos, k motionKind) motionRes { return motionRes{pos: q, kind: k, ok: true} }
	switch c.name {
	case "h", "<left>", "<bs>":
		if p.Col == 0 {
			return motionRes{}
		}
		return ok(pos(p.Line, max(0, p.Col-n)), exclusive)
	case "l", "<right>":
		limit := t.len(p.Line) - 1
		if op != "" {
			limit = t.len(p.Line)
		}
		if p.Col >= limit {
			return motionRes{}
		}
		return ok(pos(p.Line, min(p.Col+n, limit)), exclusive)
	case "j", "<down>":
		if p.Line >= last {
			return motionRes{}
		}
		return m.vertical(b, min(last, p.Line+n))
	case "k", "<up>":
		if p.Line == 0 {
			return motionRes{}
		}
		return m.vertical(b, max(0, p.Line-n))
	case "gj", "gk":
		if m.nav == nil {
			return m.evalMotion(b, cmd{count1: c.count1, count2: c.count2, name: c.name[1:]}, p, op)
		}
		var q buffer.Pos
		if c.name == "gj" {
			q = m.nav.Down(b, p, n)
		} else {
			q = m.nav.Up(b, p, n)
		}
		return ok(b.Clamp(q), exclusive)
	case "0", "<home>":
		return ok(pos(p.Line, 0), exclusive)
	case "^":
		return ok(pos(p.Line, firstNonBlank(b, p.Line)), exclusive)
	case "$", "<end>":
		l := min(last, p.Line+n-1)
		r := ok(pos(l, max(0, t.len(l)-1)), inclusive)
		r.wantEOL = true
		return r
	case "g_":
		l := min(last, p.Line+n-1)
		col := t.len(l) - 1
		for col > 0 && charClass(t.at(pos(l, col)), false) == clsBlank {
			col--
		}
		return ok(pos(l, max(0, col)), inclusive)
	case "+", "<cr>", "-", "_":
		l := p.Line + n
		switch c.name {
		case "-":
			l = p.Line - n
		case "_":
			l = p.Line + n - 1
		}
		if l < 0 || l > last {
			if (c.name == "-" && p.Line == 0) || (c.name != "-" && p.Line == last && c.name != "_") {
				return motionRes{}
			}
			l = max(0, min(l, last))
		}
		return ok(pos(l, firstNonBlank(b, l)), linewise)
	case "G", "gg":
		l := last
		if c.name == "gg" {
			l = 0
		}
		if c.hasCount() {
			l = min(last, n-1)
		}
		return ok(pos(l, firstNonBlank(b, l)), linewise)
	case "w", "W":
		return ok(t.fwdWord(p, n, c.name == "W", op != ""), exclusive)
	case "b", "B":
		q := t.bckWord(p, n, c.name == "B")
		return motionRes{pos: q, kind: exclusive, ok: q != p}
	case "e", "E":
		q := t.endWord(p, n, c.name == "E", false)
		return motionRes{pos: q, kind: inclusive, ok: q != p || op != ""}
	case "ge", "gE":
		q := t.bckEndWord(p, n, c.name == "gE")
		return motionRes{pos: q, kind: inclusive, ok: q != p}
	case "f", "F", "t", "T":
		m.lastFind = findState{ch: c.arg, fwd: c.name == "f" || c.name == "t", till: c.name == "t" || c.name == "T"}
		return m.findMotion(t, p, n, m.lastFind, false)
	case ";", ",":
		if m.lastFind.ch == "" {
			return motionRes{}
		}
		f := m.lastFind
		if c.name == "," {
			f.fwd = !f.fwd
		}
		return m.findMotion(t, p, n, f, true)
	case "}", "{":
		dir := 1
		if c.name == "{" {
			dir = -1
		}
		q, incl := t.paragraph(p, n, dir)
		k := exclusive
		if incl {
			k = inclusive
		}
		return motionRes{pos: q, kind: k, ok: q != p}
	case "%":
		if c.hasCount() {
			if n > 100 {
				return motionRes{}
			}
			l := min(last, (n*(last+1)+99)/100-1)
			return ok(pos(l, firstNonBlank(b, l)), linewise)
		}
		q, found := t.matchPair(p)
		return motionRes{pos: q, kind: inclusive, ok: found}
	case "n", "N":
		return m.searchMotion(b, p, c.name == "N", n)
	}
	return motionRes{}
}

// findMotion runs f/F/t/T. repeat is set for ; and , (a repeated t that
// would not move skips the adjacent match, as in vim).
func (m *Machine) findMotion(t *text, p buffer.Pos, n int, f findState, repeat bool) motionRes {
	gs := t.g(p.Line)
	dir := 1
	if !f.fwd {
		dir = -1
	}
	col := p.Col
	stop := !(repeat && f.till && n == 1)
	for ; n > 0; n-- {
		for {
			col += dir
			if col < 0 || col >= len(gs) {
				return motionRes{}
			}
			if gs[col] == f.ch && stop {
				break
			}
			stop = true
		}
	}
	if f.till {
		col -= dir
	}
	k := inclusive
	if !f.fwd {
		k = exclusive
	}
	return motionRes{pos: pos(p.Line, col), kind: k, ok: true}
}

// skipClass moves p over characters of class cc in direction dir. It returns
// true if it hit the edge of the buffer.
func (t *text) skipClass(p *buffer.Pos, cc int, big bool, dir int) bool {
	for t.cls(*p, big) == cc {
		var r int
		if dir > 0 {
			r = t.inc(p)
		} else {
			r = t.dec(p)
		}
		if r == -1 {
			return true
		}
	}
	return false
}

// fwdWord is vim's fwd_word: n words forward. With eol (an operator is
// pending) the last word stops at the end of its line instead of moving to
// the next line.
func (t *text) fwdWord(p buffer.Pos, n int, big, eol bool) buffer.Pos {
	for n > 0 {
		n--
		sclass := t.cls(p, big)
		lastLine := p.Line == t.last()
		i := t.inc(&p)
		if i == -1 || (i >= 1 && lastLine) {
			return p
		}
		if i >= 1 && eol && n == 0 {
			return p
		}
		if sclass != clsBlank {
			for t.cls(p, big) == sclass {
				i = t.inc(&p)
				if i == -1 || (i >= 1 && eol && n == 0) {
					return p
				}
			}
		}
		for t.cls(p, big) == clsBlank {
			if p.Col == 0 && t.emptyLine(p) {
				break
			}
			i = t.inc(&p)
			if i == -1 || (i >= 1 && eol && n == 0) {
				return p
			}
		}
	}
	return p
}

// endWord is vim's end_word: to the end of the n-th word. With stop (cw),
// a cursor already at the end of a word stays there.
func (t *text) endWord(p buffer.Pos, n int, big, stop bool) buffer.Pos {
	for n > 0 {
		n--
		sclass := t.cls(p, big)
		if t.inc(&p) == -1 {
			return p
		}
		if c := t.cls(p, big); c == sclass && sclass != clsBlank {
			if t.skipClass(&p, sclass, big, 1) {
				return p
			}
		} else if !stop || sclass == clsBlank {
			for t.cls(p, big) == clsBlank {
				if t.inc(&p) == -1 {
					return p
				}
			}
			if t.skipClass(&p, t.cls(p, big), big, 1) {
				return p
			}
		}
		t.dec(&p)
		stop = false
	}
	return p
}

// bckWord is vim's bck_word: to the start of the n-th previous word; empty
// lines count as words.
func (t *text) bckWord(p buffer.Pos, n int, big bool) buffer.Pos {
outer:
	for n > 0 {
		n--
		if t.dec(&p) == -1 {
			return p
		}
		for t.cls(p, big) == clsBlank {
			if p.Col == 0 && t.emptyLine(p) {
				continue outer
			}
			if t.dec(&p) == -1 {
				return p
			}
		}
		if t.skipClass(&p, t.cls(p, big), big, -1) {
			return p
		}
		t.inc(&p)
	}
	return p
}

// bckEndWord is vim's bckend_word (ge): to the end of the n-th previous word.
func (t *text) bckEndWord(p buffer.Pos, n int, big bool) buffer.Pos {
	for n > 0 {
		n--
		sclass := t.cls(p, big)
		if t.dec(&p) == -1 {
			return p
		}
		if sclass != clsBlank {
			for t.cls(p, big) == sclass {
				if t.dec(&p) == -1 {
					return p
				}
			}
		}
		for t.cls(p, big) == clsBlank {
			if p.Col == 0 && t.emptyLine(p) {
				break
			}
			if t.dec(&p) == -1 {
				return p
			}
		}
	}
	return p
}

// paragraph implements { and }: move to the n-th empty line that follows
// (dir 1) or precedes (dir -1) a non-empty one. Running into the last line
// lands on its last character, which is inclusive for operators.
func (t *text) paragraph(p buffer.Pos, n, dir int) (buffer.Pos, bool) {
	curr := p.Line
	for ; n > 0; n-- {
		didSkip := false
		for first := true; ; first = false {
			if t.len(curr) != 0 {
				didSkip = true
			}
			if !first && didSkip && t.len(curr) == 0 {
				break
			}
			next := curr + dir
			if next < 0 || next > t.last() {
				break
			}
			curr = next
		}
	}
	if curr == t.last() && dir > 0 && t.len(curr) > 0 {
		return pos(curr, t.len(curr)-1), true
	}
	return pos(curr, 0), false
}

var pairOf = map[string]struct {
	other string
	dir   int
}{
	"(": {")", 1}, ")": {"(", -1},
	"[": {"]", 1}, "]": {"[", -1},
	"{": {"}", 1}, "}": {"{", -1},
}

// anglePair is matched by the i< / a< text objects only (not by %).
var anglePair = map[string]struct {
	other string
	dir   int
}{"<": {">", 1}, ">": {"<", -1}}

// matchPair implements %: find the first bracket at or after the cursor on
// the line and jump to its match (which may be on another line).
func (t *text) matchPair(p buffer.Pos) (buffer.Pos, bool) {
	gs := t.g(p.Line)
	col := p.Col
	for col < len(gs) {
		if _, isPair := pairOf[gs[col]]; isPair {
			break
		}
		col++
	}
	if col >= len(gs) {
		return p, false
	}
	return t.findMatch(pos(p.Line, col))
}

// findMatch returns the bracket matching the one at q, counting nesting.
func (t *text) findMatch(q buffer.Pos) (buffer.Pos, bool) {
	open := t.at(q)
	pr, ok := pairOf[open]
	if !ok {
		pr, ok = anglePair[open]
	}
	if !ok {
		return q, false
	}
	depth := 0
	for {
		var r int
		if pr.dir > 0 {
			r = t.inc(&q)
		} else {
			r = t.dec(&q)
		}
		if r == -1 {
			return q, false
		}
		switch t.at(q) {
		case open:
			depth++
		case pr.other:
			if depth == 0 {
				return q, true
			}
			depth--
		}
	}
}
