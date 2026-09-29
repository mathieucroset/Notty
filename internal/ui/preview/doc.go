package preview

import (
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/mathieucroset/notty/internal/tasks"
)

// doc is one finished render of a note: its segments, their rendered
// blocks, and the stacked layout the view scrolls through.
type doc struct {
	gen           int
	notePath      string
	content       string
	width, height int

	segs   []Segment
	blocks []block // parallel to segs

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
			if len(b.text) > 0 {
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
func (d *doc) mapTasks() {
	d.tasks = tasks.Parse(d.content)
	d.taskRows = make([]int, len(d.tasks))
	next := map[int]int{} // segment -> first unsearched row within it
	for i, t := range d.tasks {
		d.taskRows[i] = -1
		si := -1
		for k, s := range d.segs {
			if s.Kind == Text && s.StartLine <= t.Line && t.Line <= s.EndLine {
				si = k
				break
			}
		}
		if si < 0 || d.segHeight[si] == 0 {
			continue
		}
		lines := d.blocks[si].text
		from := next[si]
		row := findTaskRow(lines, from, t.Text)
		if row < 0 {
			row = 0
		} else {
			next[si] = row + 1
		}
		d.taskRows[i] = d.segTop[si] + row
	}
}

func findTaskRow(lines []string, from int, text string) int {
	needle := taskNeedle(text)
	norm := make([]string, len(lines))
	for i, l := range lines {
		norm[i] = normalize(ansi.Strip(l))
	}
	box := func(s string) bool { return strings.ContainsAny(s, "☐☑") }
	if needle != "" {
		for i := from; i < len(norm); i++ {
			if box(norm[i]) && strings.Contains(norm[i], needle) {
				return i
			}
		}
		for i := from; i < len(norm); i++ {
			if strings.Contains(norm[i], needle) {
				return i
			}
		}
	}
	for i := from; i < len(norm); i++ {
		if box(norm[i]) {
			return i
		}
	}
	return -1
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
func normalize(s string) string {
	s = strings.Map(func(r rune) rune {
		switch r {
		case '*', '_', '`', '~':
			return -1
		}
		return r
	}, s)
	return strings.Join(strings.Fields(s), " ")
}
