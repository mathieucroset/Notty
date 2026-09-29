package preview

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/mathieucroset/notty/internal/tasks"
)

// doc is one finished render of a note: its segments, their rendered
// blocks, and the stacked layout the view scrolls through.
type doc struct {
	gen           int
	notePath      string
	content       string
	width, height int

	segs     []Segment
	blocks   []block   // parallel to segs
	textKeys []textKey // cache keys of the text segments

	lines     []docLine
	segTop    []int // first layout row of each segment
	segHeight []int

	images   []*imgItem // every image, in document order
	imgTop   []int      // first layout row of each image
	imgRows  []int      // layout rows of each image
	paths    []string   // absolute paths of the images that resolved
	pathIdx  []int      // per image: index into paths, or -1
	tasks    []tasks.Task
	taskRows []int // layout row of each task's marker, -1 when unknown
	// boxes are the open and done checkbox glyphs Glamour drew, which
	// mark the rows that show a task.
	boxes [2]string
}

// docLine is one layout row: a Glamour line, a blank separator, or one row
// of an image block.
type docLine struct {
	text   string
	img    int // index into doc.images, -1 for text rows
	imgRow int // row within the image block
}

// layout stacks the blocks with one blank row between segments and
// between the images of an image segment, and maps tasks to rows.
func (d *doc) layout() {
	d.lines = nil
	d.segTop = make([]int, len(d.segs))
	d.segHeight = make([]int, len(d.segs))
	d.images, d.imgTop, d.imgRows, d.paths, d.pathIdx = nil, nil, nil, nil, nil
	sep := func() {
		if len(d.lines) > 0 {
			d.lines = append(d.lines, docLine{img: -1})
		}
	}
	for si, seg := range d.segs {
		b := d.blocks[si]
		if seg.Kind == Text {
			// A list piece sits right under the previous piece, but an image
			// block in between keeps its blank row.
			underPiece := seg.cont && si > 0 && d.segs[si-1].Kind == Text
			if len(b.text) > 0 && !underPiece {
				sep()
			}
			d.segTop[si] = len(d.lines)
			for _, l := range b.text {
				d.lines = append(d.lines, docLine{text: l, img: -1})
			}
		} else {
			if len(b.images) > 0 {
				sep()
			}
			d.segTop[si] = len(d.lines)
			for k, item := range b.images {
				if k > 0 {
					d.lines = append(d.lines, docLine{img: -1})
				}
				idx := len(d.images)
				d.images = append(d.images, item)
				d.imgTop = append(d.imgTop, len(d.lines))
				n := max(1, len(item.rows))
				d.imgRows = append(d.imgRows, n)
				for r := range n {
					dl := docLine{img: idx, imgRow: r}
					if r < len(item.rows) {
						dl.text = item.rows[r]
					}
					d.lines = append(d.lines, dl)
				}
				if item.abs != "" {
					d.pathIdx = append(d.pathIdx, len(d.paths))
					d.paths = append(d.paths, item.abs)
				} else {
					d.pathIdx = append(d.pathIdx, -1)
				}
			}
		}
		d.segHeight[si] = len(d.lines) - d.segTop[si]
	}
	d.mapTasks()
}

// mapTasks finds the rendered row of every task of the rendered content
// (spec §6.1 task mapping): within the task's text segment, the next row
// showing the task text stripped of markdown; failing that, the next row
// with a checkbox; failing that, the segment's first row.
//
// It runs in linear time: every segment's rows are normalized once, and
// each task searches a bounded window after the previous task's row.
func (d *doc) mapTasks() {
	d.tasks = tasks.Parse(d.content)
	d.taskRows = make([]int, len(d.tasks))
	if len(d.tasks) == 0 {
		return
	}
	lineSeg := make([]int, strings.Count(d.content, "\n")+1)
	for i := range lineSeg {
		lineSeg[i] = -1
	}
	for si, s := range d.segs {
		if s.Kind != Text {
			continue
		}
		for l := max(0, s.StartLine); l <= s.EndLine && l < len(lineSeg); l++ {
			lineSeg[l] = si
		}
	}
	rows := map[int]*segRows{} // segment -> normalized rows, built once
	for i, t := range d.tasks {
		d.taskRows[i] = -1
		si := -1
		if t.Line >= 0 && t.Line < len(lineSeg) {
			si = lineSeg[t.Line]
		}
		if si < 0 || d.segHeight[si] == 0 {
			continue
		}
		sr := rows[si]
		if sr == nil {
			sr = newSegRows(d.blocks[si].text, d.boxes)
			rows[si] = sr
		}
		row := sr.find(taskNeedle(t.Text))
		if row < 0 {
			row = 0
		}
		d.taskRows[i] = d.segTop[si] + row
	}
}

// taskWindow bounds how far past the previous task a task's row is
// searched for: up to the third checkbox row, and at most this many rows.
const taskWindow = 256

// segRows are a text segment's rendered rows, normalized for matching,
// with a cursor just past the last task found.
type segRows struct {
	norm []string
	box  []bool // row shows a task checkbox
	next int
}

func newSegRows(lines []string, boxes [2]string) *segRows {
	sr := &segRows{norm: make([]string, len(lines)), box: make([]bool, len(lines))}
	for i, l := range lines {
		sr.norm[i] = normalize(l)
		sr.box[i] = hasBox(sr.norm[i], boxes)
	}
	return sr
}

// hasBox reports whether the normalized row shows one of the checkbox
// glyphs.
func hasBox(row string, boxes [2]string) bool {
	for _, b := range boxes {
		if b != "" && strings.Contains(row, b) {
			return true
		}
	}
	return false
}

// find returns the row of the next task, whose text starts with needle:
// the first row in the window with a checkbox and the needle, else with
// the needle, else with a checkbox; -1 when none. The cursor moves past a
// found row.
func (sr *segRows) find(needle string) int {
	from := sr.next
	end, boxes := from, 0
	for end < len(sr.norm) && end < from+taskWindow {
		if sr.box[end] {
			if boxes++; boxes > 2 {
				break
			}
		}
		end++
	}
	found := -1
	if needle != "" {
		for i := from; i < end && found < 0; i++ {
			if sr.box[i] && strings.Contains(sr.norm[i], needle) {
				found = i
			}
		}
		for i := from; i < end && found < 0; i++ {
			if strings.Contains(sr.norm[i], needle) {
				found = i
			}
		}
	}
	for i := from; i < end && found < 0; i++ {
		if sr.box[i] {
			found = i
		}
	}
	if found >= 0 {
		sr.next = found + 1
	}
	return found
}

// taskNeedle is the start of a task's text, stripped of markdown, short
// enough to sit on the task's first rendered row.
func taskNeedle(text string) string {
	words := strings.Fields(normalize(stripLinks(text)))
	var b strings.Builder
	for _, w := range words {
		if b.Len() > 0 {
			if b.Len()+1+len(w) > 16 {
				break
			}
			b.WriteByte(' ')
		}
		b.WriteString(w)
	}
	return b.String()
}

// stripLinks turns "[text](url)" into "text" and drops images.
func stripLinks(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == '!' && i+1 < len(s) && s[i+1] == '[' {
			if end, ok := linkEnd(s, i+1); ok {
				i = end
				continue
			}
		}
		if s[i] == '[' {
			if end, ok := linkEnd(s, i); ok {
				b.WriteString(s[i+1 : strings.Index(s[i:], "](")+i])
				i = end
				continue
			}
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

// linkEnd returns the index just past "[...](...)" starting at s[i]=='['.
func linkEnd(s string, i int) (int, bool) {
	mid := strings.Index(s[i:], "](")
	if mid < 0 || strings.Contains(s[i+1:i+mid], "[") {
		return 0, false
	}
	close := strings.IndexByte(s[i+mid+2:], ')')
	if close < 0 {
		return 0, false
	}
	return i + mid + 2 + close + 1, true
}

// normalize drops emphasis and code markers and collapses whitespace, so
// source text and rendered rows compare equal.
// Escape sequences (CSI, OSC and two-byte ones) are skipped.
func normalize(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	space := false
	for i := 0; i < len(s); {
		if s[i] == 0x1b {
			i = skipEscape(s, i)
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		i += size
		switch {
		case r == '*' || r == '_' || r == '`' || r == '~':
			continue
		case unicode.IsSpace(r):
			space = b.Len() > 0
			continue
		}
		if space {
			b.WriteByte(' ')
			space = false
		}
		b.WriteRune(r)
	}
	return b.String()
}

// skipEscape returns the index just past the escape sequence at s[i].
func skipEscape(s string, i int) int {
	if i+1 >= len(s) {
		return len(s)
	}
	switch s[i+1] {
	case '[': // CSI: parameters, then a final byte in 0x40..0x7e
		for j := i + 2; j < len(s); j++ {
			if s[j] >= 0x40 && s[j] <= 0x7e {
				return j + 1
			}
		}
		return len(s)
	case ']', 'P', '_': // OSC, DCS, APC: until BEL or ST
		for j := i + 2; j < len(s); j++ {
			if s[j] == 0x07 {
				return j + 1
			}
			if s[j] == 0x1b && j+1 < len(s) && s[j+1] == '\\' {
				return j + 2
			}
		}
		return len(s)
	}
	return i + 2
}
