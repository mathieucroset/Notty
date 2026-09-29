package vim

import (
	"strings"
	"unicode"

	"github.com/mathieucroset/notty/internal/buffer"
)

// register holds yanked or deleted text. Linewise text ends with "\n".
type register struct {
	text     string
	linewise bool
}

// setRegister stores text in the unnamed register and, for "+, also puts it
// on the system clipboard.
func (m *Machine) setRegister(reg, text string, lw bool) {
	m.unnamed = register{text: text, linewise: lw}
	if reg == "+" {
		s := text
		m.eff.Clipboard = &s
	}
}

// Register returns the unnamed register's text and whether it is linewise.
func (m *Machine) Register() (string, bool) { return m.unnamed.text, m.unnamed.linewise }

// beginChange opens the undo group of a change and remembers the cursor so
// that undoing the change puts the cursor back where it was.
func (m *Machine) beginChange(b *buffer.Buffer) {
	b.BeginGroup()
	m.chgCursor = b.Cursor()
	m.chgVersion = b.Version()
}

// endChange closes the undo group opened by beginChange.
func (m *Machine) endChange(b *buffer.Buffer) {
	b.EndGroup()
	if b.Version() != m.chgVersion {
		m.undoCur[b.Version()] = m.chgCursor
	}
}

// undo undoes n steps, restoring the cursor from before each change.
func (m *Machine) undo(b *buffer.Buffer, n int) {
	for ; n > 0; n-- {
		v := b.Version()
		p, ok := b.Undo()
		if !ok {
			m.eff.Message = "Already at oldest change"
			break
		}
		if c, found := m.undoCur[v]; found {
			delete(m.undoCur, v)
			m.redoCur[b.Version()] = c
			p = c
		}
		b.SetCursor(p)
	}
	m.clampNormal(b)
}

// redo redoes n steps; the cursor goes to the start of the change.
func (m *Machine) redo(b *buffer.Buffer, n int) {
	for ; n > 0; n-- {
		v := b.Version()
		if _, ok := b.Redo(); !ok {
			m.eff.Message = "Already at newest change"
			break
		}
		if c, found := m.redoCur[v]; found {
			delete(m.redoCur, v)
			m.undoCur[b.Version()] = c
		}
	}
	m.clampNormal(b)
}

// lines returns the range of whole lines l1..l2 (in either order).
func lines(b *buffer.Buffer, l1, l2 int) buffer.Range {
	if l2 < l1 {
		l1, l2 = l2, l1
	}
	return buffer.Range{Start: pos(l1, 0), End: lineEnd(b, l2)}
}

// indentCols returns the number of leading blank graphemes on line l.
func indentCols(b *buffer.Buffer, l int) int { return firstNonBlankOrEnd(b, l) }

// opRange computes the text an operator command acts on.
func (m *Machine) opRange(b *buffer.Buffer, c cmd) (r buffer.Range, lw, ok bool) {
	from := b.Cursor()
	last := b.LineCount() - 1
	if c.name == c.op {
		return lines(b, from.Line, min(last, from.Line+c.count()-1)), true, true
	}
	if isTextObject(c.name) {
		return m.textObject(b, c, from)
	}
	if c.op == "c" && (c.name == "w" || c.name == "W") {
		// cw on a word changes to its end, like ce.
		t := newText(b)
		if charClass(t.at(from), false) != clsBlank {
			end := t.endWord(from, c.count(), c.name == "W", true)
			return buffer.Range{Start: from, End: pos(end.Line, end.Col+1)}, false, true
		}
	}
	mr := m.evalMotion(b, c, from, c.op)
	if !mr.ok {
		return r, false, false
	}
	s, e := from, mr.pos
	if e.Less(s) {
		s, e = e, s
	}
	switch mr.kind {
	case linewise:
		return lines(b, s.Line, e.Line), true, true
	case inclusive:
		e.Col++
		return buffer.Range{Start: s, End: e}, false, true
	}
	r, lw = adjustExclusive(b, s, e)
	return r, lw, true
}

// adjustExclusive applies vim's rules for exclusive motions (":h
// exclusive"): an end in column 0 of a later line moves to the end of the
// previous line, and if the start is in the indentation as well the motion
// becomes linewise.
func adjustExclusive(b *buffer.Buffer, s, e buffer.Pos) (buffer.Range, bool) {
	if e.Col == 0 && e.Line > s.Line {
		e = lineEnd(b, e.Line-1)
		if s.Col <= indentCols(b, s.Line) {
			return lines(b, s.Line, e.Line), true
		}
	}
	return buffer.Range{Start: s, End: e}, false
}

// execOperator runs op + motion / text object / doubled op.
func (m *Machine) execOperator(b *buffer.Buffer, c cmd) {
	r, lw, ok := m.opRange(b, c)
	if !ok {
		return
	}
	m.applyOp(b, c.op, c.reg, r, lw, 1)
}

// applyOp applies an operator to a range. shift is the number of indent
// levels for > and <.
func (m *Machine) applyOp(b *buffer.Buffer, op, reg string, r buffer.Range, lw bool, shift int) {
	r = r.Normalized()
	switch op {
	case "y":
		m.setRegister(reg, rangeText(b, r, lw), lw)
		cur := b.Cursor()
		if !lw {
			b.SetCursor(r.Start)
		} else if r.Start.Line < cur.Line {
			b.SetCursor(pos(r.Start.Line, cur.Col))
		}
	case "d":
		if !lw && r.End.Line > r.Start.Line && r.Start.Col <= indentCols(b, r.Start.Line) &&
			isBlank(b.TextIn(buffer.Range{Start: r.End, End: lineEnd(b, r.End.Line)})) {
			// A multi-line delete of whole lines' worth of text is linewise.
			r, lw = lines(b, r.Start.Line, r.End.Line), true
		}
		text := rangeText(b, r, lw)
		if text != "" || lw {
			m.setRegister(reg, text, lw)
		}
		if lw {
			deleteLines(b, r.Start.Line, r.End.Line)
			l := min(r.Start.Line, b.LineCount()-1)
			b.SetCursor(pos(l, firstNonBlank(b, l)))
		} else {
			b.Delete(r)
			b.SetCursor(r.Start)
		}
	case "c":
		text := rangeText(b, r, lw)
		if text != "" || lw {
			m.setRegister(reg, text, lw)
		}
		if lw {
			indent := leadingWS(b.Line(r.Start.Line))
			b.Replace(r, indent)
			m.startInsert(b, pos(r.Start.Line, graphemeLen(indent)), 1)
		} else {
			b.Delete(r)
			m.startInsert(b, r.Start, 1)
		}
	case ">", "<":
		for l := r.Start.Line; l <= r.End.Line; l++ {
			if op == ">" {
				indentLine(b, l, shift, true)
			} else {
				outdentLine(b, l, shift)
			}
		}
		b.SetCursor(pos(r.Start.Line, firstNonBlank(b, r.Start.Line)))
	}
}

// rangeText returns the register text for a range (linewise text ends with
// a newline).
func rangeText(b *buffer.Buffer, r buffer.Range, lw bool) string {
	if !lw {
		return b.TextIn(r)
	}
	var sb strings.Builder
	for l := r.Start.Line; l <= r.End.Line; l++ {
		sb.WriteString(b.Line(l))
		sb.WriteByte('\n')
	}
	return sb.String()
}

// deleteLines removes lines l1..l2 entirely (the buffer keeps at least one
// empty line).
func deleteLines(b *buffer.Buffer, l1, l2 int) {
	last := b.LineCount() - 1
	switch {
	case l2 < last:
		b.Delete(buffer.Range{Start: pos(l1, 0), End: pos(l2+1, 0)})
	case l1 > 0:
		b.Delete(buffer.Range{Start: lineEnd(b, l1-1), End: lineEnd(b, l2)})
	default:
		b.Delete(buffer.Range{Start: pos(0, 0), End: lineEnd(b, l2)})
	}
}

// indentUnit is one level of indentation for a line: a tab if the line is
// tab-indented, else two spaces.
func indentUnit(line string) string {
	if strings.HasPrefix(line, "\t") {
		return "\t"
	}
	return "  "
}

// indentLine adds n indent levels to line l and returns the number of
// graphemes added. With skipEmpty, empty lines are left alone (like >>).
func indentLine(b *buffer.Buffer, l, n int, skipEmpty bool) int {
	line := b.Line(l)
	if skipEmpty && line == "" {
		return 0
	}
	add := strings.Repeat(indentUnit(line), n)
	b.Insert(pos(l, 0), add)
	return len(add)
}

// outdentLine removes up to n indent levels from line l and returns the
// number of graphemes removed.
func outdentLine(b *buffer.Buffer, l, n int) int {
	line := b.Line(l)
	removed := 0
	for ; n > 0; n-- {
		rest := line[removed:]
		switch {
		case strings.HasPrefix(rest, "\t"):
			removed++
		case strings.HasPrefix(rest, "  "):
			removed += 2
		case strings.HasPrefix(rest, " "):
			removed++
		default:
			n = 0
			continue
		}
	}
	if removed > 0 {
		b.Delete(buffer.Range{Start: pos(l, 0), End: pos(l, removed)})
	}
	return removed
}

// put pastes a register after (or before) the cursor count times.
func (m *Machine) put(b *buffer.Buffer, reg register, before bool, count int) {
	if reg.text == "" {
		return
	}
	cur := b.Cursor()
	if reg.linewise {
		body := strings.TrimSuffix(reg.text, "\n")
		body = strings.TrimSuffix(strings.Repeat(body+"\n", count), "\n")
		after := cur.Line
		if before {
			after--
		}
		p := b.InsertLineAfter(after, body)
		b.SetCursor(pos(p.Line, firstNonBlank(b, p.Line)))
		return
	}
	s := strings.Repeat(reg.text, count)
	at := cur
	if !before && b.LineLen(cur.Line) > 0 {
		at.Col++
	}
	end := b.Insert(at, s)
	if strings.Contains(s, "\n") {
		b.SetCursor(at)
	} else {
		b.SetCursor(pos(end.Line, end.Col-1))
	}
}

// joinLines joins count lines starting at l (J): the leading whitespace of
// each joined line is replaced by a single space.
func joinLines(b *buffer.Buffer, l, count int) bool {
	last := b.LineCount() - 1
	if l >= last {
		return false
	}
	count = min(max(count, 2), last-l+1)
	col := 0
	for i := 1; i < count; i++ {
		cur, next := b.Line(l), b.Line(l+1)
		ws := leadingWS(next)
		rest := next[len(ws):]
		sep := " "
		if rest == "" || cur == "" || strings.HasSuffix(cur, " ") || strings.HasSuffix(cur, "\t") ||
			strings.HasPrefix(rest, ")") {
			sep = ""
		}
		col = graphemeLen(cur)
		b.Replace(buffer.Range{Start: pos(l, col), End: pos(l+1, graphemeLen(ws))}, sep)
	}
	b.SetCursor(pos(l, col))
	return true
}

// replaceChars implements r<c>.
func replaceChars(b *buffer.Buffer, arg string, n int) {
	cur := b.Cursor()
	if cur.Col+n > b.LineLen(cur.Line) {
		return
	}
	r := buffer.Range{Start: cur, End: pos(cur.Line, cur.Col+n)}
	if arg == "<cr>" {
		b.Replace(r, "\n")
		b.SetCursor(pos(cur.Line+1, 0))
		return
	}
	ch := tokenChar(arg)
	if ch == "" {
		return
	}
	b.Replace(r, strings.Repeat(ch, n))
	b.SetCursor(pos(cur.Line, cur.Col+n-1))
}

// toggleCase implements ~ on n characters from the cursor.
func toggleCase(b *buffer.Buffer, n int) {
	cur := b.Cursor()
	end := min(cur.Col+n, b.LineLen(cur.Line))
	if end <= cur.Col {
		return
	}
	r := buffer.Range{Start: cur, End: pos(cur.Line, end)}
	b.Replace(r, swapCase(b.TextIn(r)))
	b.SetCursor(pos(cur.Line, end))
}

func swapCase(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case unicode.IsUpper(r):
			return unicode.ToLower(r)
		case unicode.IsLower(r):
			return unicode.ToUpper(r)
		}
		return r
	}, s)
}
