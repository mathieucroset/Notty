package vim

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/mathieucroset/notty/internal/buffer"
)

// text is a read-only grapheme view of a buffer with a per-line cache. It is
// created fresh for each motion or text object, so edits never leave it stale.
type text struct {
	b     *buffer.Buffer
	cache map[int][]string
}

func newText(b *buffer.Buffer) *text {
	return &text{b: b, cache: map[int][]string{}}
}

// g returns the graphemes of line l.
func (t *text) g(l int) []string {
	if gs, ok := t.cache[l]; ok {
		return gs
	}
	gs := buffer.Graphemes(t.b.Line(l))
	t.cache[l] = gs
	return gs
}

func (t *text) len(l int) int { return len(t.g(l)) }

func (t *text) last() int { return t.b.LineCount() - 1 }

// at returns the grapheme at p, or "" at or past the end of the line.
func (t *text) at(p buffer.Pos) string {
	gs := t.g(p.Line)
	if p.Col < 0 || p.Col >= len(gs) {
		return ""
	}
	return gs[p.Col]
}

// inc moves p one position forward, where every line has the positions
// 0..len (len being the end-of-line position), like vim's inc(). It returns
// 0 for a move within the line, 2 for a move onto the end-of-line position, 1
// for a move to the next line and -1 if p is at the end of the buffer.
func (t *text) inc(p *buffer.Pos) int {
	n := t.len(p.Line)
	if p.Col < n {
		p.Col++
		if p.Col == n {
			return 2
		}
		return 0
	}
	if p.Line < t.last() {
		p.Line++
		p.Col = 0
		return 1
	}
	return -1
}

// dec moves p one position backward (the inverse of inc). It returns 0 for a
// move within the line, 1 for a move onto the end of the previous line and
// -1 at the start of the buffer.
func (t *text) dec(p *buffer.Pos) int {
	if p.Col > 0 {
		p.Col--
		return 0
	}
	if p.Line > 0 {
		p.Line--
		p.Col = t.len(p.Line)
		return 1
	}
	return -1
}

// Character classes for word motions.
const (
	clsBlank = iota
	clsPunct
	clsWord
)

// charClass classifies a grapheme: blank (whitespace or end of line),
// keyword (letters, digits, underscore) or punctuation. For WORD motions
// every non-blank is one class.
func charClass(g string, big bool) int {
	if g == "" {
		return clsBlank
	}
	r, _ := utf8.DecodeRuneInString(g)
	if unicode.IsSpace(r) {
		return clsBlank
	}
	if big {
		return clsPunct
	}
	if r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) {
		return clsWord
	}
	return clsPunct
}

func (t *text) cls(p buffer.Pos, big bool) int { return charClass(t.at(p), big) }

// emptyLine reports whether p is on an empty line.
func (t *text) emptyLine(p buffer.Pos) bool { return t.len(p.Line) == 0 }

// firstNonBlank returns the column of the first non-blank grapheme of line l
// (or the last column if the line is all blanks).
func firstNonBlank(b *buffer.Buffer, l int) int {
	line := b.Line(l)
	ws := len(line) - len(strings.TrimLeft(line, " \t"))
	if ws == len(line) {
		return max(0, b.LineLen(l)-1)
	}
	return buffer.ByteToCol(line, ws)
}

// leadingWS returns the leading spaces and tabs of s.
func leadingWS(s string) string {
	return s[:len(s)-len(strings.TrimLeft(s, " \t"))]
}

// isBlank reports whether s has only whitespace.
func isBlank(s string) bool { return strings.TrimSpace(s) == "" }

// graphemeLen returns the number of grapheme clusters in s.
func graphemeLen(s string) int { return len(buffer.Graphemes(s)) }

// pos is a shorthand constructor.
func pos(line, col int) buffer.Pos { return buffer.Pos{Line: line, Col: col} }

// lineEnd returns the end-of-line position of line l.
func lineEnd(b *buffer.Buffer, l int) buffer.Pos { return pos(l, b.LineLen(l)) }
