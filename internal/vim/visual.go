package vim

import (
	"strings"
	"unicode"

	"github.com/mathieucroset/notty/internal/buffer"
)

// visualEdits are the visual-mode commands that modify the buffer.
var visualEdits = map[string]bool{
	"d": true, "x": true, "<del>": true, "X": true, "D": true,
	"c": true, "s": true, "S": true, "C": true, "R": true,
	">": true, "<": true, "J": true, "~": true, "u": true, "U": true,
}

func init() {
	for k := range visualEdits {
		visualActions[k] = true
	}
	for _, k := range []string{"y", "Y", "o", "O", "v", "V"} {
		visualActions[k] = true
	}
}

// enterVisual starts a visual selection at the cursor.
func (m *Machine) enterVisual(b *buffer.Buffer, mode Mode) {
	m.mode = mode
	m.anchor = b.Cursor()
}

// exitVisual returns to normal mode.
func (m *Machine) exitVisual(b *buffer.Buffer) {
	m.mode = Normal
	m.clampNormal(b)
}

// visualRange returns the selected text: charwise inclusive of the cursor
// character (and of the line break when the selection ends on an empty
// line), or whole lines in visual-line mode.
func (m *Machine) visualRange(b *buffer.Buffer) (buffer.Range, bool) {
	s, e := m.anchor, b.Cursor()
	if e.Less(s) {
		s, e = e, s
	}
	if m.mode == VisualLine {
		return lines(b, s.Line, e.Line), true
	}
	end := e
	if e.Col < b.LineLen(e.Line) {
		end.Col++
	} else if e.Line < b.LineCount()-1 {
		end = pos(e.Line+1, 0)
	}
	return buffer.Range{Start: s, End: end}, false
}

// Selection returns the visual selection for rendering. A linewise
// selection ends at the start of the line after the last selected one.
func (m *Machine) Selection(b *buffer.Buffer) (buffer.Range, bool) {
	if !m.isVisual() {
		return buffer.Range{}, false
	}
	r, lw := m.visualRange(b)
	if lw {
		r.End = pos(r.End.Line+1, 0)
	}
	return r, true
}

// execVisual runs a command in visual or visual-line mode.
func (m *Machine) execVisual(b *buffer.Buffer, c cmd) {
	if isMotion(c.name) {
		m.moveCursor(b, c)
		return
	}
	if isTextObject(c.name) {
		m.selectTextObject(b, c)
		return
	}
	switch c.name {
	case "v", "V":
		mode := Visual
		if c.name == "V" {
			mode = VisualLine
		}
		if m.mode == mode {
			m.exitVisual(b)
		} else {
			m.mode = mode
		}
		return
	case "o", "O":
		cur := b.Cursor()
		b.SetCursor(m.anchor)
		m.anchor = cur
		m.curswant = b.Cursor().Col
		return
	}
	if m.execVisualSpecial(b, c) {
		return
	}
	if visualEdits[c.name] {
		if m.readOnly {
			m.eff.Blocked = true
			return
		}
		m.recordVisual(b, c)
		m.beginChange(b)
		if m.anchor.Less(m.chgCursor) {
			m.chgCursor = m.anchor // undo returns to the start of the selection
		}
		m.visualOp(b, c)
		if m.mode != Insert {
			m.endChange(b)
		}
		return
	}
	m.visualOp(b, c) // yank
}

// visualOp applies an operator command to the selection and leaves visual
// mode.
func (m *Machine) visualOp(b *buffer.Buffer, c cmd) {
	r, lw := m.visualRange(b)
	l1, l2 := r.Start.Line, r.End.Line
	if !lw && r.End.Col == 0 && l2 > l1 {
		l2-- // a charwise selection ending with a line break
	}
	all := lines(b, l1, l2)
	m.mode = Normal
	switch c.name {
	case "y":
		m.applyOp(b, "y", c.reg, r, lw, 1, true)
	case "Y":
		m.applyOp(b, "y", c.reg, all, true, 1, true)
	case "d", "x", "<del>":
		m.applyOp(b, "d", c.reg, r, lw, 1, true)
	case "X", "D":
		m.applyOp(b, "d", c.reg, all, true, 1, true)
	case "c", "s":
		m.applyOp(b, "c", c.reg, r, lw, 1, true)
	case "S", "C", "R":
		m.applyOp(b, "c", c.reg, all, true, 1, true)
	case ">", "<":
		m.applyOp(b, c.name, c.reg, all, true, c.count(), true)
	case "J":
		joinLines(b, l1, max(2, l2-l1+1))
	case "~", "u", "U":
		if lw {
			r = all
		}
		b.Replace(r, mapCase(b.TextIn(r), c.name))
		b.SetCursor(r.Start)
	}
}

// mapCase applies ~ (swap), u (lower) or U (upper) to s.
func mapCase(s, how string) string {
	switch how {
	case "u":
		return strings.ToLower(s)
	case "U":
		return strings.ToUpper(s)
	}
	return strings.Map(func(r rune) rune {
		if unicode.IsUpper(r) {
			return unicode.ToLower(r)
		}
		return unicode.ToUpper(r)
	}, s)
}

// selectTextObject sets the selection to a text object (viw, vip, ...).
func (m *Machine) selectTextObject(b *buffer.Buffer, c cmd) {
	r, lw, ok := m.textObject(b, c, b.Cursor())
	if !ok {
		return
	}
	if lw {
		m.mode = VisualLine
		m.anchor = pos(r.Start.Line, 0)
		b.SetCursor(pos(r.End.Line, normalCol(b, r.End.Line, b.Cursor().Col)))
		return
	}
	if r.End == r.Start {
		return
	}
	m.anchor = r.Start
	end := r.End
	t := newText(b)
	t.dec(&end)
	b.SetCursor(end)
	m.curswant = end.Col
}
