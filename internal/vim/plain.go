package vim

import (
	"github.com/mathieucroset/notty/internal/buffer"
)

// Plain is the non-modal editor used when vim mode is off (spec §4.4
// "Editor, vim off"): always typing, shift+arrows select, ctrl+z / ctrl+y
// undo and redo, ctrl+c / ctrl+x / ctrl+v use the system clipboard.
type Plain struct {
	readOnly  bool
	selecting bool
	anchor    buffer.Pos
	curswant  int
	lastPos   buffer.Pos

	// typing groups consecutive insertions into one undo step until a
	// non-insertion key.
	typing   bool
	groupBuf *buffer.Buffer
}

var _ Editor = (*Plain)(nil)

// NewPlain returns a plain editor.
func NewPlain() *Plain { return &Plain{} }

// ModeName returns "PLAIN", or "READ-ONLY".
func (p *Plain) ModeName() string {
	if p.readOnly {
		return "READ-ONLY"
	}
	return "PLAIN"
}

// SetReadOnly toggles read-only mode: edits return Effect.Blocked.
func (p *Plain) SetReadOnly(ro bool) {
	p.readOnly = ro
	if ro && p.groupBuf != nil {
		p.endTyping(p.groupBuf)
	}
}

// Selection returns the shift-selection, if any.
func (p *Plain) Selection(b *buffer.Buffer) (buffer.Range, bool) {
	if !p.selecting {
		return buffer.Range{}, false
	}
	r := buffer.Range{Start: b.Clamp(p.anchor), End: b.Cursor()}.Normalized()
	if r.Start == r.End {
		return buffer.Range{}, false
	}
	return r, true
}

// plainEdits are the keys that modify the buffer.
var plainEdits = map[string]bool{
	"<bs>": true, "<del>": true, "<cr>": true, "<c-z>": true, "<c-y>": true,
	"<c-x>": true, "<c-v>": true, "<tab>": true, "<s-tab>": true, "<c-t>": true,
}

// Handle processes one key.
func (p *Plain) Handle(b *buffer.Buffer, k Key) Effect {
	var eff Effect
	if b.Cursor() != p.lastPos {
		p.curswant = b.Cursor().Col
	}
	defer func() { p.lastPos = b.Cursor() }()

	tok := specialToken(k)
	insertion := tok == "<space>" || (isPrintable(k) && k.Name == "")
	if !insertion {
		p.endTyping(b)
	}
	if p.readOnly && (insertion || plainEdits[tok]) {
		eff.Blocked = true
		return eff
	}
	switch tok {
	case "<left>", "<right>", "<up>", "<down>", "<home>", "<end>":
		if r, ok := p.Selection(b); ok && (tok == "<left>" || tok == "<right>") {
			// Collapse the selection to the side of the arrow.
			if tok == "<left>" {
				b.SetCursor(r.Start)
			} else {
				b.SetCursor(r.End)
			}
			p.curswant = b.Cursor().Col
		} else {
			p.move(b, tok)
		}
		p.selecting = false
	case "<s-left>", "<s-right>", "<s-up>", "<s-down>", "<s-home>", "<s-end>":
		if !p.selecting {
			p.selecting = true
			p.anchor = b.Cursor()
		}
		p.move(b, "<"+tok[3:])
	case "<c-a>":
		p.selecting = true
		p.anchor = pos(0, 0)
		b.SetCursor(lineEnd(b, b.LineCount()-1))
	case "<esc>":
		p.selecting = false
		eff.FocusSidebar = true
	case "<bs>", "<del>":
		b.BeginGroup()
		if !p.deleteSelection(b) {
			deleteChar(b, tok == "<bs>")
		}
		b.EndGroup()
	case "<cr>":
		b.BeginGroup()
		p.deleteSelection(b)
		b.SetCursor(splitLine(b, b.Cursor()))
		b.EndGroup()
	case "<c-z>", "<c-y>":
		p.selecting = false
		if tok == "<c-z>" {
			b.Undo()
		} else {
			b.Redo()
		}
	case "<c-c>", "<c-x>":
		text, r := p.copyRange(b)
		eff.Clipboard = &text
		if tok == "<c-x>" {
			b.BeginGroup()
			b.Delete(r)
			b.EndGroup()
			b.SetCursor(r.Start)
			p.selecting = false
		}
	case "<c-v>":
		eff.NeedClipboard = true
	case "<tab>", "<s-tab>":
		p.shiftLines(b, tok == "<tab>")
	default:
		if p.handleSpecial(b, tok, &eff) {
			break
		}
		if insertion {
			text := k.Text
			if tok == "<space>" {
				text = " "
			}
			p.typeText(b, text)
		}
	}
	if !plainVertical[tok] {
		p.curswant = b.Cursor().Col
	}
	return eff
}

// plainVertical keys keep the desired column.
var plainVertical = map[string]bool{"<up>": true, "<down>": true, "<s-up>": true, "<s-down>": true}

// handleSpecial handles the Notty additions: ctrl+t toggles the task on
// the cursor line.
func (p *Plain) handleSpecial(b *buffer.Buffer, tok string, eff *Effect) bool {
	if tok != "<c-t>" {
		return false
	}
	b.BeginGroup()
	eff.ToggleTask = toggleTask(b)
	b.EndGroup()
	return true
}

// typeText inserts typed text, replacing the selection, inside the open
// typing group.
func (p *Plain) typeText(b *buffer.Buffer, text string) {
	if !p.typing {
		b.BeginGroup()
		p.typing, p.groupBuf = true, b
	}
	p.deleteSelection(b)
	b.SetCursor(b.Insert(b.Cursor(), text))
}

// endTyping closes the typing undo group.
func (p *Plain) endTyping(b *buffer.Buffer) {
	if p.typing {
		b.EndGroup()
		p.typing, p.groupBuf = false, nil
	}
}

// move moves the cursor for an arrow, home or end key.
func (p *Plain) move(b *buffer.Buffer, tok string) {
	cur := b.Cursor()
	last := b.LineCount() - 1
	switch tok {
	case "<left>":
		if cur.Col > 0 {
			cur.Col--
		} else if cur.Line > 0 {
			cur = lineEnd(b, cur.Line-1)
		}
	case "<right>":
		if cur.Col < b.LineLen(cur.Line) {
			cur.Col++
		} else if cur.Line < last {
			cur = pos(cur.Line+1, 0)
		}
	case "<up>", "<down>":
		l := cur.Line - 1
		if tok == "<down>" {
			l = cur.Line + 1
		}
		switch {
		case l < 0:
			cur = pos(0, 0)
		case l > last:
			cur = lineEnd(b, last)
		default:
			cur = pos(l, min(p.curswant, b.LineLen(l)))
		}
		b.SetCursor(cur)
		return
	case "<home>":
		cur.Col = 0
	case "<end>":
		cur.Col = b.LineLen(cur.Line)
	}
	b.SetCursor(cur)
}

// deleteSelection deletes the selection, if any, and reports whether it did.
func (p *Plain) deleteSelection(b *buffer.Buffer) bool {
	r, ok := p.Selection(b)
	p.selecting = false
	if !ok {
		return false
	}
	b.Delete(r)
	b.SetCursor(r.Start)
	return true
}

// deleteChar deletes the grapheme before (backspace) or after the cursor,
// joining lines at the line edges.
func deleteChar(b *buffer.Buffer, backward bool) {
	cur := b.Cursor()
	if backward {
		switch {
		case cur.Col > 0:
			b.Delete(buffer.Range{Start: pos(cur.Line, cur.Col-1), End: cur})
			b.SetCursor(pos(cur.Line, cur.Col-1))
		case cur.Line > 0:
			prev := lineEnd(b, cur.Line-1)
			b.Delete(buffer.Range{Start: prev, End: cur})
			b.SetCursor(prev)
		}
		return
	}
	if cur.Col < b.LineLen(cur.Line) {
		b.Delete(buffer.Range{Start: cur, End: pos(cur.Line, cur.Col+1)})
	} else if cur.Line < b.LineCount()-1 {
		b.Delete(buffer.Range{Start: cur, End: pos(cur.Line+1, 0)})
	}
	b.SetCursor(cur)
}

// copyRange returns the text for ctrl+c / ctrl+x and the range to cut: the
// selection, or the whole current line (with its newline) when there is
// none.
func (p *Plain) copyRange(b *buffer.Buffer) (string, buffer.Range) {
	if r, ok := p.Selection(b); ok {
		return b.TextIn(r), r
	}
	l := b.Cursor().Line
	text := b.Line(l) + "\n"
	var r buffer.Range
	switch {
	case l < b.LineCount()-1:
		r = buffer.Range{Start: pos(l, 0), End: pos(l+1, 0)}
	case l > 0:
		r = buffer.Range{Start: lineEnd(b, l-1), End: lineEnd(b, l)}
	default:
		r = buffer.Range{Start: pos(0, 0), End: lineEnd(b, 0)}
	}
	return text, r
}

// shiftLines indents (or outdents) the current line or the selected lines,
// keeping the cursor and selection on the same text.
func (p *Plain) shiftLines(b *buffer.Buffer, indent bool) {
	cur := b.Cursor()
	l1, l2 := cur.Line, cur.Line
	r, sel := p.Selection(b)
	if sel {
		l1, l2 = r.Start.Line, r.End.Line
		if r.End.Col == 0 && l2 > l1 {
			l2--
		}
	}
	b.BeginGroup()
	defer b.EndGroup()
	for l := l1; l <= l2; l++ {
		var d int
		if indent {
			d = indentLine(b, l, 1, sel)
		} else {
			d = -outdentLine(b, l, 1)
		}
		if cur.Line == l {
			cur.Col = max(0, cur.Col+d)
		}
		if sel && p.anchor.Line == l {
			p.anchor.Col = max(0, p.anchor.Col+d)
		}
	}
	b.SetCursor(cur)
}

// PasteClipboard inserts clipboard text at the cursor, replacing the
// selection, as one undo step.
func (p *Plain) PasteClipboard(b *buffer.Buffer, text string, before bool) {
	defer func() { p.lastPos = b.Cursor() }()
	if p.readOnly || text == "" {
		return
	}
	p.endTyping(b)
	b.BeginGroup()
	p.deleteSelection(b)
	b.SetCursor(b.Insert(b.Cursor(), text))
	b.EndGroup()
}
