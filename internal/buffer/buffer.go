package buffer

import (
	"slices"
	"strings"
)

// Pos is a position in the buffer. Col counts grapheme clusters, not bytes
// or runes. Col == LineLen(Line) is the position just past the last cluster.
type Pos struct{ Line, Col int }

// Less reports whether p comes before q.
func (p Pos) Less(q Pos) bool {
	return p.Line < q.Line || (p.Line == q.Line && p.Col < q.Col)
}

// Range is a half-open span [Start, End). Operations normalize a range whose
// Start is after its End, then clamp its endpoints: columns to the line, a
// line before the first to the start of the buffer and a line past the last
// to the end of the buffer.
type Range struct{ Start, End Pos }

// Normalized returns r with Start <= End.
func (r Range) Normalized() Range {
	if r.End.Less(r.Start) {
		return Range{Start: r.End, End: r.Start}
	}
	return r
}

// bpos is an internal position with a byte offset instead of a grapheme
// column. All edits are performed and recorded in byte coordinates, which
// stay exact even when an edit merges or splits grapheme clusters.
type bpos struct{ line, off int }

// Buffer is a line-based text model. The zero value is not usable; call New.
type Buffer struct {
	lines           []string
	trailingNewline bool // text ended with "\n"; restored by String
	cursor          Pos

	version      uint64
	firstChanged int // lowest line touched since ResetChanged, -1 if none

	history
}

// normalizeNewlines converts CRLF line endings to LF.
func normalizeNewlines(s string) string {
	return strings.ReplaceAll(s, "\r\n", "\n")
}

// splitText normalizes line endings, strips one trailing "\n" and reports
// whether it was present.
func splitText(text string) (body string, trailing bool) {
	text = normalizeNewlines(text)
	if strings.HasSuffix(text, "\n") {
		return text[:len(text)-1], true
	}
	return text, false
}

// New returns a buffer holding text. Lines are split on "\n" ("\r\n" is
// normalized to "\n"). A single trailing newline is remembered so that
// String round-trips the (normalized) input exactly.
func New(text string) *Buffer {
	body, trailing := splitText(text)
	return &Buffer{
		lines:           strings.Split(body, "\n"),
		trailingNewline: trailing,
		firstChanged:    -1,
	}
}

// String returns the buffer contents, including the trailing newline if the
// original text had one.
func (b *Buffer) String() string {
	s := strings.Join(b.lines, "\n")
	if b.trailingNewline {
		s += "\n"
	}
	return s
}

// LineCount returns the number of lines; always >= 1.
func (b *Buffer) LineCount() int { return len(b.lines) }

// Line returns line i without its newline, or "" if i is out of range.
func (b *Buffer) Line(i int) string {
	if i < 0 || i >= len(b.lines) {
		return ""
	}
	return b.lines[i]
}

// LineLen returns the number of grapheme clusters on line i.
func (b *Buffer) LineLen(i int) int { return graphemeCount(b.Line(i)) }

// Cursor returns the cursor position.
func (b *Buffer) Cursor() Pos { return b.cursor }

// SetCursor moves the cursor, clamping it into the buffer.
func (b *Buffer) SetCursor(p Pos) { b.cursor = b.Clamp(p) }

// Clamp returns the nearest valid position to p: the line is clamped to
// [0, LineCount-1] and the column to [0, LineLen(line)].
func (b *Buffer) Clamp(p Pos) Pos {
	p.Line = max(0, min(p.Line, len(b.lines)-1))
	p.Col = max(0, min(p.Col, b.LineLen(p.Line)))
	return p
}

// Version returns a counter that increases on every change to the text,
// including undo and redo.
func (b *Buffer) Version() uint64 { return b.version }

// FirstChangedLine returns the lowest line index touched by an edit since the
// last ResetChanged, or -1 if nothing changed.
func (b *Buffer) FirstChangedLine() int { return b.firstChanged }

// ResetChanged clears the FirstChangedLine tracking.
func (b *Buffer) ResetChanged() { b.firstChanged = -1 }

// toBpos converts a range endpoint to byte coordinates. Unlike Clamp it is
// order-preserving: a position before the first line maps to the start of
// the buffer and one past the last line to the end of the buffer, so a
// normalized range stays normalized after clamping.
func (b *Buffer) toBpos(p Pos) bpos {
	last := len(b.lines) - 1
	switch {
	case p.Line < 0:
		return bpos{}
	case p.Line > last:
		return bpos{line: last, off: len(b.lines[last])}
	}
	return bpos{line: p.Line, off: ColToByte(b.lines[p.Line], p.Col)}
}

// toPos converts byte coordinates to a grapheme position, rounding an offset
// inside a cluster down to that cluster (used for the start of a change).
func (b *Buffer) toPos(bp bpos) Pos {
	return Pos{Line: bp.line, Col: ByteToCol(b.lines[bp.line], bp.off)}
}

// toPosCeil is toPos rounding up, used for the end of inserted text: when
// the text merged with the following cluster (e.g. "e" typed before a lone
// combining mark) the end lies after the merged cluster, so the next insert
// at that position goes after it.
func (b *Buffer) toPosCeil(bp bpos) Pos {
	return Pos{Line: bp.line, Col: byteToColCeil(b.lines[bp.line], bp.off)}
}

// bRange converts r to normalized, clamped byte coordinates.
func (b *Buffer) bRange(r Range) (start, end bpos) {
	r = r.Normalized()
	return b.toBpos(r.Start), b.toBpos(r.End)
}

// textBetween returns the text in [start, end) in byte coordinates.
func (b *Buffer) textBetween(start, end bpos) string {
	if start.line == end.line {
		return b.lines[start.line][start.off:end.off]
	}
	var sb strings.Builder
	sb.WriteString(b.lines[start.line][start.off:])
	for i := start.line + 1; i < end.line; i++ {
		sb.WriteByte('\n')
		sb.WriteString(b.lines[i])
	}
	sb.WriteByte('\n')
	sb.WriteString(b.lines[end.line][:end.off])
	return sb.String()
}

// endOf returns the byte position just after text when text is placed at
// start.
func endOf(start bpos, text string) bpos {
	n := strings.Count(text, "\n")
	if n == 0 {
		return bpos{line: start.line, off: start.off + len(text)}
	}
	return bpos{line: start.line + n, off: len(text) - strings.LastIndexByte(text, '\n') - 1}
}

// replaceBytes is the single low-level mutation: it swaps [start, end) for
// text (already newline-normalized), bumps the version, tracks the first
// changed line and keeps the cursor valid. It records nothing for undo.
func (b *Buffer) replaceBytes(start, end bpos, text string) (removed string, newEnd bpos) {
	removed = b.textBetween(start, end)
	joined := b.lines[start.line][:start.off] + text + b.lines[end.line][end.off:]
	b.lines = slices.Replace(b.lines, start.line, end.line+1, strings.Split(joined, "\n")...)

	b.version++
	if b.firstChanged < 0 || start.line < b.firstChanged {
		b.firstChanged = start.line
	}
	b.cursor = b.Clamp(b.cursor)
	return removed, endOf(start, text)
}

// Insert inserts text (which may contain newlines; CRLF is normalized) at p
// and returns the position just after the inserted text. p is clamped like a
// Range endpoint, so a line past the last one appends at the end.
func (b *Buffer) Insert(p Pos, text string) Pos {
	return b.Replace(Range{Start: p, End: p}, text)
}

// InsertLineAfter inserts text as one or more new lines below line (vim `o`
// and linewise put) as a single undoable edit, and returns the start of the
// first new line. line is clamped to the last line, so inserting after the
// last line works; a negative line inserts above the first line. Empty text
// opens one blank line.
func (b *Buffer) InsertLineAfter(line int, text string) Pos {
	text = normalizeNewlines(text)
	if line < 0 {
		b.edit(bpos{}, bpos{}, text+"\n")
		return Pos{}
	}
	line = min(line, len(b.lines)-1)
	eol := bpos{line: line, off: len(b.lines[line])}
	b.edit(eol, eol, "\n"+text)
	return Pos{Line: line + 1}
}

// Delete removes the text in r (normalized and clamped; may span lines) and
// returns it.
func (b *Buffer) Delete(r Range) string {
	start, end := b.bRange(r)
	if start == end {
		return ""
	}
	removed, _ := b.edit(start, end, "")
	return removed
}

// Replace swaps the text in r (normalized and clamped) for text and returns
// the position just after the new text.
func (b *Buffer) Replace(r Range, text string) Pos {
	text = normalizeNewlines(text)
	start, end := b.bRange(r)
	if start == end && text == "" {
		return b.toPos(start)
	}
	_, newEnd := b.edit(start, end, text)
	return b.toPosCeil(newEnd)
}

// TextIn returns the text in r (normalized and clamped).
func (b *Buffer) TextIn(r Range) string {
	start, end := b.bRange(r)
	return b.textBetween(start, end)
}

// SetText replaces the whole contents (e.g. on reload from disk) as one
// undoable change, keeping the cursor clamped. Lines shared at the start and
// end of the old and new text are left alone, so only the differing middle
// is edited (FirstChangedLine stays accurate and undo stores less).
// Identical text is a no-op. It does not mark the buffer saved; callers do
// that if appropriate.
func (b *Buffer) SetText(text string) {
	body, trailing := splitText(text)
	old, lines := b.lines, strings.Split(body, "\n")
	if trailing == b.trailingNewline && slices.Equal(old, lines) {
		return
	}

	// p common leading lines, s common trailing lines, not overlapping.
	p := 0
	for p < min(len(old), len(lines)) && old[p] == lines[p] {
		p++
	}
	s := 0
	for p+s < min(len(old), len(lines)) && old[len(old)-1-s] == lines[len(lines)-1-s] {
		s++
	}
	oldEnd, newMid := len(old)-s, lines[p:len(lines)-s]
	lastOld := bpos{line: len(old) - 1, off: len(old[len(old)-1])}

	switch {
	case s > 0:
		// Whole lines (with their "\n") between two kept regions.
		text := ""
		if len(newMid) > 0 {
			text = strings.Join(newMid, "\n") + "\n"
		}
		b.editTrailing(bpos{line: p}, bpos{line: oldEnd}, text, trailing)
	case p < len(old) && len(newMid) > 0:
		// Old lines from p to the end become newMid.
		b.editTrailing(bpos{line: p}, lastOld, strings.Join(newMid, "\n"), trailing)
	case p < len(old):
		// New text is a strict prefix: drop old lines p.. and their
		// preceding "\n" (p >= 1 because the new text has at least one line).
		b.editTrailing(bpos{line: p - 1, off: len(old[p-1])}, lastOld, "", trailing)
	default:
		// Old text is a prefix: append newMid lines (empty when only the
		// trailing newline changes, recorded as an empty edit).
		text := ""
		if len(newMid) > 0 {
			text = "\n" + strings.Join(newMid, "\n")
		}
		b.editTrailing(lastOld, lastOld, text, trailing)
	}
}

// edit applies and records a primitive change that keeps the
// trailing-newline flag.
func (b *Buffer) edit(start, end bpos, text string) (removed string, newEnd bpos) {
	return b.editTrailing(start, end, text, b.trailingNewline)
}

// editTrailing applies a primitive change, sets the trailing-newline flag and
// records the change (with enough to invert it) for undo.
func (b *Buffer) editTrailing(start, end bpos, text string, trailing bool) (removed string, newEnd bpos) {
	before := b.trailingNewline
	removed, newEnd = b.replaceBytes(start, end, text)
	b.trailingNewline = trailing
	b.record(change{start: start, removed: removed, inserted: text, trailBefore: before, trailAfter: trailing})
	return removed, newEnd
}
