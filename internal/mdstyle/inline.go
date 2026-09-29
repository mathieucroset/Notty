package mdstyle

import (
	"unicode"
	"unicode/utf8"

	"github.com/mathieucroset/notty/internal/tags"
)

// inline parses inline markdown (emphasis, code, links, images, tags) within
// a single line. It is a pragmatic approximation of CommonMark: delimiters
// must hug their content, '_' does not work intraword, and a closer of the
// same length is preferred over a longer one.
type inline struct {
	line    string
	heading Kind // H1..H6 when parsing heading text (emphasis keeps it), else Text
	tags    [][2]int
}

func newInline(line string, heading Kind) *inline {
	return &inline{line: line, heading: heading, tags: tags.FindTags(line)}
}

// parse returns sorted, non-overlapping spans for line[lo:hi]. When fill is
// not Text, bytes not covered by any inline construct get a fill span.
func (p *inline) parse(lo, hi int, fill Kind) []Span {
	var out []Span
	gap := lo
	emit := func(spans ...Span) {
		for _, s := range spans {
			if s.End <= s.Start {
				continue
			}
			if fill != Text && s.Start > gap {
				out = append(out, Span{Start: gap, End: s.Start, Kind: fill})
			}
			out = append(out, s)
			gap = s.End
		}
	}
	line := p.line
	for i := lo; i < hi; {
		c := line[i]
		switch {
		case c == '\\' && i+1 < hi && isASCIIPunct(line[i+1]):
			i += 2
			continue
		case c == '`':
			n := runLenTo(line, i, hi, '`')
			if cl := findRun(line, i+n, hi, '`', n); cl >= 0 {
				emit(Span{Start: i, End: i + n, Kind: Markup},
					Span{Start: i + n, End: cl, Kind: Code},
					Span{Start: cl, End: cl + n, Kind: Markup})
				i = cl + n
			} else {
				i += n
			}
			continue
		case c == '!' && i+1 < hi && line[i+1] == '[':
			if _, _, _, end, ok := p.link(i+1, hi); ok {
				emit(Span{Start: i, End: end, Kind: Image})
				i = end
				continue
			}
		case c == '[':
			if textEnd, urlStart, urlEnd, end, ok := p.link(i, hi); ok {
				emit(Span{Start: i, End: i + 1, Kind: Markup},
					Span{Start: i + 1, End: textEnd, Kind: Link},
					Span{Start: textEnd, End: urlStart, Kind: Markup},
					Span{Start: urlStart, End: urlEnd, Kind: LinkURL},
					Span{Start: urlEnd, End: end, Kind: Markup})
				i = end
				continue
			}
		case c == '#':
			if end := p.tagAt(i); end > i && end <= hi {
				emit(Span{Start: i, End: end, Kind: Tag})
				i = end
				continue
			}
		case c == '*' || c == '_' || c == '~':
			if spans, end, ok := p.emphasis(i, hi); ok {
				emit(spans...)
				i = end
			} else {
				i += runLenTo(line, i, hi, c)
			}
			continue
		}
		i++
	}
	if fill != Text && gap < hi {
		out = append(out, Span{Start: gap, End: hi, Kind: fill})
	}
	return out
}

func (p *inline) tagAt(i int) int {
	for _, r := range p.tags {
		if r[0] == i {
			return r[1]
		}
	}
	return i
}

// link parses "[text](url)" with the '[' at open. It returns the offset of
// the closing ']', the url bounds and the end offset (after ')').
func (p *inline) link(open, hi int) (textEnd, urlStart, urlEnd, end int, ok bool) {
	line := p.line
	depth := 0
	j := open
	for ; j < hi; j++ {
		switch line[j] {
		case '\\':
			j++
			continue
		case '`': // brackets inside a code span do not count
			m := runLenTo(line, j, hi, '`')
			if cl := findRun(line, j+m, hi, '`', m); cl >= 0 {
				j = cl + m - 1
			} else {
				j += m - 1
			}
			continue
		case '[':
			depth++
		case ']':
			depth--
		}
		if depth == 0 {
			break
		}
	}
	if j+1 >= hi || line[j] != ']' || line[j+1] != '(' {
		return 0, 0, 0, 0, false
	}
	textEnd, urlStart = j, j+2
	depth = 1
	for k := urlStart; k < hi; k++ {
		switch line[k] {
		case '\\':
			k++
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return textEnd, urlStart, k, k + 1, true
			}
		}
	}
	return 0, 0, 0, 0, false
}

// emphasis parses a delimited run ('*', '_' or '~') starting at i. A triple
// opener without a matching closer is retried as a single delimiter, so
// "***a** b*" parses as italic wrapping bold.
func (p *inline) emphasis(i, hi int) ([]Span, int, bool) {
	c := p.line[i]
	n := runLenTo(p.line, i, hi, c)
	if spans, end, ok := p.emphasisN(i, n, hi); ok {
		return spans, end, ok
	}
	if n == 3 && c != '~' {
		return p.emphasisN(i, 1, hi)
	}
	return nil, 0, false
}

// emphasisN parses an emphasis whose opener is the n delimiters at i.
func (p *inline) emphasisN(i, n, hi int) ([]Span, int, bool) {
	line := p.line
	c := line[i]
	var kind Kind
	switch {
	case c == '~' && n == 2:
		kind = Strike
	case c == '~':
		return nil, 0, false
	case n == 1:
		kind = Italic
	case n == 2:
		kind = Bold
	case n == 3:
		kind = BoldItalic
	default:
		return nil, 0, false
	}
	// The opener must be followed by non-space; '_' must not be intraword.
	if i+n >= hi || isSpaceAt(line, i+n) || (c == '_' && i > 0 && isAlnumBefore(line, i)) {
		return nil, 0, false
	}
	closeAt, fallback, fallbackLen := -1, -1, 0
	for j := i + n; j < hi && closeAt < 0; {
		switch line[j] {
		case '\\':
			j += 2
			continue
		case '`':
			m := runLenTo(line, j, hi, '`')
			if cl := findRun(line, j+m, hi, '`', m); cl >= 0 {
				j = cl + m
			} else {
				j += m
			}
			continue
		case c:
			m := runLenTo(line, j, hi, c)
			valid := j > i+n && !isSpaceBefore(line, j) &&
				(c != '_' || j+m >= len(line) || !isAlnumAt(line, j+m))
			if valid {
				if m == n {
					closeAt = j
				} else if m > n && fallback < 0 && c != '~' {
					fallback, fallbackLen = j, m
				}
			}
			j += m
			continue
		}
		j++
	}
	if closeAt < 0 && fallback >= 0 {
		// A longer closing run: its first n chars close this emphasis and the
		// rest is literal ("**bold***"), unless the content has an unclosed
		// opener that the extra chars close ("**a *b***").
		extra := fallbackLen - n
		if p.hasUnclosedOpener(i+n, fallback, c, extra) {
			closeAt = fallback + extra
		} else {
			closeAt = fallback
		}
	}
	if closeAt < 0 {
		return nil, 0, false
	}
	content := kind
	if p.heading != Text {
		content = p.heading
	}
	spans := []Span{{Start: i, End: i + n, Kind: Markup}}
	spans = append(spans, p.parse(i+n, closeAt, content)...)
	spans = append(spans, Span{Start: closeAt, End: closeAt + n, Kind: Markup})
	return spans, closeAt + n, true
}

// hasUnclosedOpener reports whether line[lo:hi] contains a run of exactly k
// c delimiters that can open emphasis and is not closed by a later run of k
// within the range. Code spans are skipped.
func (p *inline) hasUnclosedOpener(lo, hi int, c byte, k int) bool {
	line := p.line
	open := 0
	for j := lo; j < hi; {
		switch line[j] {
		case '\\':
			j += 2
			continue
		case '`':
			m := runLenTo(line, j, hi, '`')
			if cl := findRun(line, j+m, hi, '`', m); cl >= 0 {
				j = cl + m
			} else {
				j += m
			}
			continue
		case c:
			m := runLenTo(line, j, hi, c)
			if m == k {
				canClose := j > lo && !isSpaceBefore(line, j)
				canOpen := j+m < hi && !isSpaceAt(line, j+m) &&
					(c != '_' || j == 0 || !isAlnumBefore(line, j))
				switch {
				case canClose && open > 0:
					open--
				case canOpen:
					open++
				}
			}
			j += m
			continue
		}
		j++
	}
	return open > 0
}

// runLenTo counts consecutive c bytes starting at i, stopping at hi.
func runLenTo(s string, i, hi int, c byte) int {
	n := 0
	for i+n < hi && s[i+n] == c {
		n++
	}
	return n
}

// findRun returns the start of the next run of exactly n c bytes in
// s[from:hi], or -1.
func findRun(s string, from, hi int, c byte, n int) int {
	for j := from; j < hi; {
		if s[j] != c {
			j++
			continue
		}
		m := runLenTo(s, j, hi, c)
		if m == n {
			return j
		}
		j += m
	}
	return -1
}

func isASCIIPunct(b byte) bool {
	return (b >= '!' && b <= '/') || (b >= ':' && b <= '@') || (b >= '[' && b <= '`') || (b >= '{' && b <= '~')
}

func isSpaceAt(s string, i int) bool {
	r, _ := utf8.DecodeRuneInString(s[i:])
	return unicode.IsSpace(r)
}

func isSpaceBefore(s string, i int) bool {
	r, _ := utf8.DecodeLastRuneInString(s[:i])
	return unicode.IsSpace(r)
}

func isAlnumAt(s string, i int) bool {
	r, _ := utf8.DecodeRuneInString(s[i:])
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}

func isAlnumBefore(s string, i int) bool {
	r, _ := utf8.DecodeLastRuneInString(s[:i])
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}
