// Package mdstyle is a per-line markdown tokenizer used to style the editor
// live (spec §5). It produces byte-offset spans tagged with a Kind; the
// editor maps Kinds (and chroma token types inside fenced code) to theme
// styles. State carried between lines makes fenced code blocks work, and
// Cache re-tokenizes incrementally after edits.
package mdstyle

import (
	"strconv"

	"github.com/alecthomas/chroma/v2"

	"github.com/mathieucroset/notty/internal/tags"
)

// Kind is the style class of a span.
type Kind int

const (
	Text   Kind = iota // plain text (never emitted; uncovered bytes are Text)
	Markup             // markup characters, rendered dimmed
	H1                 // heading text, levels 1-6
	H2
	H3
	H4
	H5
	H6
	Bold
	Italic
	BoldItalic
	Strike
	Code         // inline code content
	CodeFence    // a fence opener or closer line
	CodeBlock    // code inside a fence; Span.Token holds the chroma token
	Link         // link text
	LinkURL      // link destination
	Image        // a whole image link, rendered as a chip
	TaskOpen     // "[ ]"
	TaskDone     // "[x]"
	TaskDoneText // text of a completed task
	Quote        // blockquote text
	ListMarker   // "- ", "* ", "+ ", "1. "
	Tag          // "#tag"
	Rule         // horizontal rule
)

var kindNames = [...]string{
	"Text", "Markup", "H1", "H2", "H3", "H4", "H5", "H6", "Bold", "Italic",
	"BoldItalic", "Strike", "Code", "CodeFence", "CodeBlock", "Link", "LinkURL",
	"Image", "TaskOpen", "TaskDone", "TaskDoneText", "Quote", "ListMarker",
	"Tag", "Rule",
}

func (k Kind) String() string {
	if k >= 0 && int(k) < len(kindNames) {
		return kindNames[k]
	}
	return "Kind(" + strconv.Itoa(int(k)) + ")"
}

// Span styles line[Start:End]. Spans returned for a line are sorted and
// non-overlapping; they need not cover the line. Lang is set on CodeFence
// and CodeBlock spans; Token only on CodeBlock spans.
type Span struct {
	Start, End int
	Kind       Kind
	Lang       string
	Token      chroma.TokenType
}

// State is carried from one line to the next.
type State struct {
	InFence     bool
	FenceLang   string
	FenceMarker string // the opening run, e.g. "```" or "~~~~"
}

// TokenizeLine tokenizes one line given the state at its start, returning
// its spans and the state at its end.
func TokenizeLine(line string, in State) ([]Span, State) {
	out := nextState(line, in)
	switch {
	case in.InFence && out.InFence: // code inside a fence
		return HighlightCode(in.FenceLang, line), out
	case in.InFence: // closing fence
		return []Span{{Start: 0, End: len(line), Kind: CodeFence, Lang: in.FenceLang}}, out
	case out.InFence: // opening fence
		return []Span{{Start: 0, End: len(line), Kind: CodeFence, Lang: out.FenceLang}}, out
	case line == "":
		return nil, out
	}
	return tokenizeBlock(line), out
}

// nextState returns the state after line given the state before it. It only
// looks at fence openers and closers, so it is much cheaper than
// TokenizeLine; Cache uses it to propagate state without computing spans.
func nextState(line string, in State) State {
	if in.InFence {
		if tags.IsFenceClose(line, in.FenceMarker) {
			return State{}
		}
		return in
	}
	if marker, lang, ok := tags.FenceOpen(line); ok {
		return State{InFence: true, FenceLang: lang, FenceMarker: marker}
	}
	return State{}
}

func tokenizeBlock(line string) []Span {
	if isRule(line) {
		return []Span{{Start: 0, End: len(line), Kind: Rule}}
	}
	indent := 0
	for indent < len(line) && indent < 3 && line[indent] == ' ' {
		indent++
	}

	// ATX heading: 1-6 '#' followed by a space or end of line.
	if n := runLen(line, indent, '#'); n >= 1 && n <= 6 {
		end := indent + n
		if end == len(line) || line[end] == ' ' || line[end] == '\t' {
			if end < len(line) {
				end++
			}
			level := H1 + Kind(n-1)
			p := newInline(line, level)
			out := []Span{{Start: indent, End: end, Kind: Markup}}
			return append(out, p.parse(end, len(line), level)...)
		}
	}

	// Blockquote.
	if indent < len(line) && line[indent] == '>' {
		end := indent + 1
		if end < len(line) && line[end] == ' ' {
			end++
		}
		p := newInline(line, Text)
		out := []Span{{Start: indent, End: end, Kind: Markup}}
		return append(out, p.parse(end, len(line), Quote)...)
	}

	// List item, optionally a task.
	if mStart, mEnd, ok := listMarker(line); ok {
		out := []Span{{Start: mStart, End: mEnd, Kind: ListMarker}}
		rest := mEnd
		if box, done, ok := taskBox(line, mEnd); ok {
			if done {
				out = append(out, Span{Start: mEnd, End: box, Kind: TaskDone})
				textStart := box
				if textStart < len(line) {
					textStart++ // the space after the box
				}
				if textStart < len(line) {
					out = append(out, Span{Start: textStart, End: len(line), Kind: TaskDoneText})
				}
				return out
			}
			out = append(out, Span{Start: mEnd, End: box, Kind: TaskOpen})
			rest = box
		}
		p := newInline(line, Text)
		return append(out, p.parse(rest, len(line), Text)...)
	}

	return newInline(line, Text).parse(0, len(line), Text)
}

// isRule reports whether line is a thematic break: up to three spaces, then
// three or more of the same '-', '*' or '_', optionally separated by spaces.
func isRule(line string) bool {
	i := 0
	for i < len(line) && i < 3 && line[i] == ' ' {
		i++
	}
	if i >= len(line) {
		return false
	}
	c := line[i]
	if c != '-' && c != '*' && c != '_' {
		return false
	}
	count := 0
	for ; i < len(line); i++ {
		switch line[i] {
		case c:
			count++
		case ' ', '\t':
		default:
			return false
		}
	}
	return count >= 3
}

// listMarker finds a list marker after leading indentation: '-', '*', '+'
// or 1-9 digits followed by '.' or ')', then a space. The returned range
// covers the marker and the following space.
func listMarker(line string) (start, end int, ok bool) {
	i := 0
	for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
		i++
	}
	start = i
	if i >= len(line) {
		return 0, 0, false
	}
	switch c := line[i]; {
	case c == '-' || c == '*' || c == '+':
		i++
	case c >= '0' && c <= '9':
		d := 0
		for i < len(line) && line[i] >= '0' && line[i] <= '9' {
			i++
			d++
		}
		if d > 9 || i >= len(line) || (line[i] != '.' && line[i] != ')') {
			return 0, 0, false
		}
		i++
	default:
		return 0, 0, false
	}
	if i >= len(line) || line[i] != ' ' {
		return 0, 0, false
	}
	return start, i + 1, true
}

// taskBox checks for "[ ]", "[x]" or "[X]" at i followed by a space or end
// of line. It returns the end of the box and whether it is checked.
func taskBox(line string, i int) (end int, done, ok bool) {
	if i+3 > len(line) || line[i] != '[' || line[i+2] != ']' {
		return 0, false, false
	}
	if i+3 < len(line) && line[i+3] != ' ' {
		return 0, false, false
	}
	switch line[i+1] {
	case ' ':
		return i + 3, false, true
	case 'x', 'X':
		return i + 3, true, true
	}
	return 0, false, false
}

func runLen(s string, i int, c byte) int {
	n := 0
	for i+n < len(s) && s[i+n] == c {
		n++
	}
	return n
}
