package resolver

import (
	"path"
	"slices"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/mathieucroset/notty/internal/merge"
)

// choice is a conflict block's resolution.
type choice int

const (
	unset choice = iota
	chooseOurs
	chooseTheirs
	chooseBoth
)

// column is one of the three text columns.
type column int

const (
	colOurs column = iota
	colTheirs
	colResult
)

// textState is a text file's merge: the blocks, a choice per conflict
// block, and the edited result once the embedded editor was accepted.
type textState struct {
	blocks     []merge.Block
	conflicts  []int    // indices into blocks of the Conflict blocks
	conflictOf []int    // per block: its conflict number, -1 for the others
	choices    []choice // one per conflict
	cur        int      // current conflict (index into conflicts)
	twoWay     bool     // no base: the file was created on both sides
	trailing   bool     // the result ends with a newline

	edited     bool   // the result was edited and accepted
	editedText string // the accepted edit

	scroll [3]int // top line of each column
}

func newTextState(f File) *textState {
	ours, theirs := string(f.Ours), string(f.Theirs)
	t := &textState{trailing: trailingNewline(f)}
	o, th := merge.SplitLines(ours), merge.SplitLines(theirs)
	if f.Base == nil {
		t.twoWay = true
		t.blocks = merge.Diff2(o, th)
	} else {
		t.blocks = merge.Diff3(merge.SplitLines(string(f.Base)), o, th)
	}
	t.conflictOf = make([]int, len(t.blocks))
	for i, b := range t.blocks {
		t.conflictOf[i] = -1
		if b.Kind == merge.Conflict {
			t.conflictOf[i] = len(t.conflicts)
			t.conflicts = append(t.conflicts, i)
		}
	}
	t.choices = make([]choice, len(t.conflicts))
	return t
}

// trailingNewline decides whether the result ends with a newline: when
// ours kept the base's final newline (or lack of one), theirs' choice wins,
// otherwise ours' change does. Without a base, ours decides.
func trailingNewline(f File) bool {
	ours := merge.HasTrailingNewline(string(f.Ours))
	if f.Base != nil && ours == merge.HasTrailingNewline(string(f.Base)) {
		return merge.HasTrailingNewline(string(f.Theirs))
	}
	return ours
}

// clone returns a copy that can be modified without touching t.
func (t *textState) clone() *textState {
	c := *t
	c.choices = slices.Clone(t.choices)
	return &c
}

// resolvedCount returns how many conflict blocks have a resolution (all of
// them once the result was edited).
func (t *textState) resolvedCount() int {
	if t.edited {
		return len(t.conflicts)
	}
	n := 0
	for _, c := range t.choices {
		if c != unset {
			n++
		}
	}
	return n
}

// allResolved reports whether the result can be written.
func (t *textState) allResolved() bool { return t.resolvedCount() == len(t.conflicts) }

// conflictIndex returns the conflict number of block i, or -1.
func (t *textState) conflictIndex(i int) int { return t.conflictOf[i] }

// chosen returns conflict block b's lines for choice c; fill is used for an
// unset choice.
func chosen(b merge.Block, c choice, fill []string) []string {
	switch c {
	case chooseOurs:
		return b.Ours
	case chooseTheirs:
		return b.Theirs
	case chooseBoth:
		return append(slices.Clip(b.Ours), b.Theirs...)
	}
	return fill
}

// resultLines builds the merged lines, with unset conflicts filled with
// ours when fillOurs is set and left out otherwise.
func (t *textState) resultLines(fillOurs bool) []string {
	return merge.Resolve(t.blocks, func(i int, b merge.Block) []string {
		var fill []string
		if fillOurs {
			fill = b.Ours
		}
		return chosen(b, t.choices[t.conflictIndex(i)], fill)
	})
}

// content returns the result to write: the accepted edit, or the blocks
// resolved with the current choices.
func (t *textState) content() string {
	if t.edited {
		return t.editedText
	}
	return merge.JoinLines(t.resultLines(false), t.trailing)
}

// baseEditText returns the text the embedded editor opens with: the
// accepted edit, or the current result with unresolved blocks pre-filled
// with ours.
func (t *textState) baseEditText() string {
	if t.edited {
		return t.editedText
	}
	return merge.JoinLines(t.resultLines(true), t.trailing)
}

// resolvedLines returns a non-conflict block's auto-applied lines.
func resolvedLines(b merge.Block) []string {
	if b.Kind == merge.Theirs {
		return b.Theirs
	}
	return b.Ours
}

// setChoice sets the current conflict's choice.
func (t *textState) setChoice(c choice) {
	if len(t.conflicts) == 0 {
		return
	}
	t.choices[t.cur] = c
}

// nextUnresolved returns the next conflict after the current one without a
// choice (wrapping around), or the current one when there is none.
func (t *textState) nextUnresolved() int {
	n := len(t.conflicts)
	for k := 1; k <= n; k++ {
		if i := (t.cur + k) % n; t.choices[i] == unset {
			return i
		}
	}
	return t.cur
}

// lineKind classifies a displayed column line.
type lineKind int

const (
	linePlain      lineKind = iota
	lineConflict            // a line of a conflict region
	lineEmpty               // marker for a conflict side with no lines
	lineUnresolved          // Result placeholder for an unresolved conflict
	lineResolved            // a Result line coming from a resolved conflict
)

// colLine is one displayed line of a column.
type colLine struct {
	text     string
	kind     lineKind
	conflict int // conflict number, -1 outside conflicts
}

const (
	unresolvedPlaceholder = "⟨unresolved: o/t/b/e⟩"
	emptySide             = "⟨no lines⟩"
)

// columnLines returns the lines shown in column c.
func (t *textState) columnLines(c column) []colLine {
	var out []colLine
	if c == colResult && t.edited {
		for _, l := range merge.SplitLines(t.editedText) {
			out = append(out, colLine{text: l, kind: linePlain, conflict: -1})
		}
		return out
	}
	for i, b := range t.blocks {
		k := t.conflictIndex(i)
		if k < 0 {
			ls := resolvedLines(b)
			switch c {
			case colOurs:
				ls = b.Ours
			case colTheirs:
				ls = b.Theirs
			}
			for _, l := range ls {
				out = append(out, colLine{text: l, kind: linePlain, conflict: -1})
			}
			continue
		}
		var ls []string
		kind := lineConflict
		switch c {
		case colOurs:
			ls = b.Ours
		case colTheirs:
			ls = b.Theirs
		default:
			if t.choices[k] == unset {
				out = append(out, colLine{text: unresolvedPlaceholder, kind: lineUnresolved, conflict: k})
				continue
			}
			ls, kind = chosen(b, t.choices[k], nil), lineResolved
		}
		if len(ls) == 0 {
			out = append(out, colLine{text: emptySide, kind: lineEmpty, conflict: k})
			continue
		}
		for _, l := range ls {
			out = append(out, colLine{text: l, kind: kind, conflict: k})
		}
	}
	return out
}

// conflictSpan returns the line range [start, end) of conflict k in lines,
// or (-1, -1).
func conflictSpan(lines []colLine, k int) (int, int) {
	start, end := -1, -1
	for i, l := range lines {
		if l.conflict == k {
			if start < 0 {
				start = i
			}
			end = i + 1
		}
	}
	return start, end
}

// scrollContext is how many lines stay visible above a conflict scrolled
// into view.
const scrollContext = 2

// scrollTo scrolls every column so the current conflict is visible in a
// body of h lines.
func (t *textState) scrollTo(h int) {
	if len(t.conflicts) == 0 || h <= 0 {
		return
	}
	for c := colOurs; c <= colResult; c++ {
		lines := t.columnLines(c)
		start, end := conflictSpan(lines, t.cur)
		if start < 0 {
			continue
		}
		if start < t.scroll[c] || end > t.scroll[c]+h {
			t.scroll[c] = max(0, start-scrollContext)
		}
		t.scroll[c] = clampScroll(t.scroll[c], len(lines), h)
	}
}

// scrollBy scrolls every column by d lines in a body of h lines.
func (t *textState) scrollBy(d, h int) {
	for c := colOurs; c <= colResult; c++ {
		t.scroll[c] = clampScroll(t.scroll[c]+d, len(t.columnLines(c)), h)
	}
}

func clampScroll(s, n, h int) int {
	return max(0, min(s, n-h))
}

// validate returns an error message when content cannot be written to
// file p: a .toml result must parse as TOML (spec §7).
func validate(p, content string) string {
	if !strings.EqualFold(path.Ext(p), ".toml") {
		return ""
	}
	var v map[string]any
	if _, err := toml.Decode(content, &v); err != nil {
		return "Invalid TOML: " + firstLine(err.Error())
	}
	return ""
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
