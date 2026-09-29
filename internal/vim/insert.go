package vim

import (
	"strings"

	"github.com/mathieucroset/notty/internal/buffer"
)

// startInsert enters insert mode at p. The caller has opened the undo group
// (exec wraps change commands); leaveInsert closes it. repeat is the count
// of i/a/I/A: the typed text is inserted that many times.
func (m *Machine) startInsert(b *buffer.Buffer, p buffer.Pos, repeat int) {
	b.SetCursor(p)
	m.mode = Insert
	m.insertRepeat = repeat
	m.sessionKeys = nil
	m.curswant = cursorCell(b)
}

// openLine implements o (below) and O (above), keeping the indentation of
// the current line; o also continues a markdown list.
func (m *Machine) openLine(b *buffer.Buffer, l int, above bool) {
	line := b.Line(l)
	prefix := leadingWS(line)
	var p buffer.Pos
	if above {
		p = b.InsertLineAfter(l-1, prefix)
	} else {
		if cont, _ := ContinueList(line); cont != "" {
			prefix = cont
		}
		p = b.InsertLineAfter(l, prefix)
	}
	p.Col = graphemeLen(prefix)
	m.startInsert(b, p, 1)
}

// insertKey handles a key in insert mode.
func (m *Machine) insertKey(b *buffer.Buffer, k Key) {
	tok := specialToken(k)
	if tok == "<esc>" || tok == "<c-c>" {
		m.leaveInsert(b)
		return
	}
	if tok != "<c-v>" {
		m.sessionKeys = append(m.sessionKeys, k)
	}
	m.insertApply(b, k, tok)
}

// insertApply performs one insert-mode key (also used to replay a session).
func (m *Machine) insertApply(b *buffer.Buffer, k Key, tok string) {
	cur := b.Cursor()
	switch tok {
	case "<bs>":
		switch {
		case cur.Col > 0:
			b.Delete(buffer.Range{Start: pos(cur.Line, cur.Col-1), End: cur})
			b.SetCursor(pos(cur.Line, cur.Col-1))
		case cur.Line > 0:
			prev := lineEnd(b, cur.Line-1)
			b.Delete(buffer.Range{Start: prev, End: cur})
			b.SetCursor(prev)
		}
	case "<del>":
		if cur.Col < b.LineLen(cur.Line) {
			b.Delete(buffer.Range{Start: cur, End: pos(cur.Line, cur.Col+1)})
		} else if cur.Line < b.LineCount()-1 {
			b.Delete(buffer.Range{Start: cur, End: pos(cur.Line+1, 0)})
		}
		b.SetCursor(cur)
	case "<cr>":
		m.insertNewline(b)
	case "<left>", "<s-left>":
		if cur.Col > 0 {
			b.SetCursor(pos(cur.Line, cur.Col-1))
		}
	case "<right>", "<s-right>":
		b.SetCursor(pos(cur.Line, cur.Col+1))
	case "<up>", "<s-up>", "<down>", "<s-down>":
		l := cur.Line - 1
		if tok == "<down>" || tok == "<s-down>" {
			l = cur.Line + 1
		}
		if l >= 0 && l < b.LineCount() {
			b.SetCursor(pos(l, colAtCell(b.Line(l), m.curswant)))
		}
		return // keep the desired column
	case "<home>", "<s-home>":
		b.SetCursor(pos(cur.Line, 0))
	case "<end>", "<s-end>":
		b.SetCursor(lineEnd(b, cur.Line))
	case "<space>":
		b.SetCursor(b.Insert(cur, " "))
	case "<c-v>":
		m.eff.NeedClipboard = true
	case "<c-w>", "<c-u>":
		if cur.Col == 0 {
			m.insertApply(b, k, "<bs>")
			return
		}
		start := pos(cur.Line, 0)
		if tok == "<c-w>" {
			// Delete the word (and the blanks after it) before the cursor.
			t := newText(b)
			start = cur
			for start.Col > 0 && t.cls(pos(cur.Line, start.Col-1), false) == clsBlank {
				start.Col--
			}
			if start.Col > 0 {
				c := t.cls(pos(cur.Line, start.Col-1), false)
				for start.Col > 0 && t.cls(pos(cur.Line, start.Col-1), false) == c {
					start.Col--
				}
			}
		}
		b.Delete(buffer.Range{Start: start, End: cur})
		b.SetCursor(start)
	default:
		if !m.insertSpecial(b, tok) && isPrintable(k) {
			b.SetCursor(b.Insert(cur, k.Text))
		}
	}
	m.curswant = cursorCell(b)
}

// insertNewline splits the line at the cursor, keeping the indentation.
func (m *Machine) insertNewline(b *buffer.Buffer) {
	b.SetCursor(splitLine(b, b.Cursor()))
}

// insertSpecial handles the Notty insert-mode keys: tab / shift+tab indent
// and outdent the line, ctrl+t toggles the task. It reports whether tok was
// handled.
func (m *Machine) insertSpecial(b *buffer.Buffer, tok string) bool {
	switch tok {
	case "<tab>", "<s-tab>":
		shiftLine(b, tok == "<tab>")
	case "<c-t>":
		if toggleTask(b) {
			m.eff.ToggleTask = true
		}
	default:
		return false
	}
	return true
}

// typedText returns the text of keys if they are all plain typed text.
func typedText(keys []Key) (string, bool) {
	var sb strings.Builder
	for _, k := range keys {
		switch specialToken(k) {
		case "":
			sb.WriteString(k.Text)
		case "<space>":
			sb.WriteByte(' ')
		default:
			return "", false
		}
	}
	return sb.String(), true
}

// leaveInsert returns to normal mode: it applies the i/a count, closes the
// undo group and moves the cursor left one column, as vim does.
func (m *Machine) leaveInsert(b *buffer.Buffer) {
	keys := m.sessionKeys
	if m.insertRepeat > 1 {
		if text, ok := typedText(keys); ok {
			// Plain typing: insert all the copies in one edit.
			b.SetCursor(b.Insert(b.Cursor(), strings.Repeat(text, m.insertRepeat-1)))
		} else {
			for i := 1; i < m.insertRepeat; i++ {
				for _, k := range keys {
					m.insertApply(b, k, specialToken(k))
				}
			}
		}
	}
	m.endInsertSession(keys)
	m.insertRepeat = 0
	m.sessionKeys = nil
	m.endChange(b)
	m.mode = Normal
	p := b.Cursor()
	if p.Col > 0 {
		p.Col--
	}
	b.SetCursor(p)
	m.clampNormal(b)
	m.curswant = cursorCell(b)
}
