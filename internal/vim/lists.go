package vim

import (
	"regexp"
	"strconv"

	"github.com/mathieucroset/notty/internal/buffer"
	"github.com/mathieucroset/notty/internal/tasks"
)

// listItemRe matches a markdown list item: indentation, a bullet ("-", "*",
// "+") or number ("3." / "3)"), whitespace, an optional task checkbox, and
// the content.
var listItemRe = regexp.MustCompile(`^([ \t]*)([-*+]|([0-9]{1,9})([.)]))([ \t]+)(\[[ xX]\](?:[ \t]+|$))?(.*)$`)

// listItem is a parsed list line.
type listItem struct {
	indent, marker, ws string
	num                int
	delim              string // "." or ")" for numbered items
	task               bool
	content            string
	prefixLen          int // columns before the content (the prefix is ASCII)
}

func parseListItem(line string) (listItem, bool) {
	m := listItemRe.FindStringSubmatch(line)
	if m == nil {
		return listItem{}, false
	}
	it := listItem{indent: m[1], marker: m[2], delim: m[4], ws: m[5], task: m[6] != "", content: m[7]}
	if m[3] != "" {
		it.num, _ = strconv.Atoi(m[3])
	}
	it.prefixLen = len(line) - len(it.content)
	return it, true
}

// ContinueList returns the prefix for the line after line when enter is
// pressed at its end (spec §5 "Todo editing"): the same indentation and
// bullet, the next number for a numbered item, and an unchecked box for a
// task ("- [x] a" -> "- [ ] "). For an empty item ("- ", "- [ ] ", "4. ")
// it returns endList: the caller clears the marker instead of adding a line.
// Lines that are not list items return "", false.
func ContinueList(line string) (prefix string, endList bool) {
	it, ok := parseListItem(line)
	if !ok {
		return "", false
	}
	if isBlank(it.content) {
		return "", true
	}
	marker := it.marker
	if it.delim != "" {
		marker = strconv.Itoa(it.num+1) + it.delim
	}
	prefix = it.indent + marker + it.ws
	if it.task {
		prefix += "[ ] "
	}
	return prefix, false
}

// splitLine breaks the line at p and returns the new cursor position. On a
// list item (with the cursor past its marker) the list continues; on an
// empty item the marker is removed, leaving the indentation. Other lines
// keep their indentation.
func splitLine(b *buffer.Buffer, p buffer.Pos) buffer.Pos {
	line := b.Line(p.Line)
	if it, ok := parseListItem(line); ok && p.Col >= it.prefixLen {
		if isBlank(it.content) {
			b.Replace(buffer.Range{Start: pos(p.Line, 0), End: lineEnd(b, p.Line)}, it.indent)
			return pos(p.Line, graphemeLen(it.indent))
		}
		prefix, _ := ContinueList(line)
		return b.Insert(p, "\n"+prefix)
	}
	indent := leadingWS(line)
	if p.Col < graphemeLen(indent) {
		indent = ""
	}
	return b.Insert(p, "\n"+indent)
}

// toggleTask toggles the task on the cursor line with tasks.ToggleLine,
// keeping the cursor on the same text. It reports whether the line changed.
func toggleTask(b *buffer.Buffer) bool {
	cur := b.Cursor()
	old := b.Line(cur.Line)
	nw := tasks.ToggleLine(old)
	if nw == old {
		return false
	}
	cp := 0
	for cp < len(old) && cp < len(nw) && old[cp] == nw[cp] {
		cp++
	}
	off := buffer.ColToByte(old, cur.Col)
	if off >= cp {
		off = max(cp, off+len(nw)-len(old))
	}
	b.Replace(buffer.Range{Start: pos(cur.Line, 0), End: lineEnd(b, cur.Line)}, nw)
	b.SetCursor(pos(cur.Line, buffer.ByteToCol(nw, off)))
	return true
}

// shiftLine indents or outdents the cursor line by one level (insert-mode
// tab / shift+tab), moving the cursor with the text.
func shiftLine(b *buffer.Buffer, indent bool) {
	cur := b.Cursor()
	var d int
	if indent {
		d = indentLine(b, cur.Line, 1, false)
	} else {
		d = -outdentLine(b, cur.Line, 1)
	}
	b.SetCursor(pos(cur.Line, max(0, cur.Col+d)))
}
