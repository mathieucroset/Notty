package preview

import (
	"strings"

	"github.com/mathieucroset/notty/internal/links"
)

// SegmentKind says how a segment is rendered.
type SegmentKind int

// Segment kinds.
const (
	// Text segments are rendered by Glamour.
	Text SegmentKind = iota
	// Image segments are rendered by imgrender, one block per image.
	Image
)

func (k SegmentKind) String() string {
	if k == Image {
		return "Image"
	}
	return "Text"
}

// Segment is one piece of the preview (spec §6.1). StartLine and EndLine
// are the 0-based, inclusive source lines it covers. For the image block
// that follows a text paragraph with inline images, they cover the lines
// holding those images.
type Segment struct {
	Kind      SegmentKind
	Markdown  string
	Images    []links.ImageLink
	StartLine int
	EndLine   int
}

// Split cuts markdown into segments (spec §6.1). Blocks are separated by
// blank lines; a block whose first line is indented continues the previous
// one (list continuations, indented code), and blank lines inside fenced
// code never separate blocks. A block made only of image links becomes an
// Image segment. A block with images among other text becomes a Text
// segment followed by an Image segment holding those images. Images inside
// fenced code or inline code spans are ignored.
func Split(content string) []Segment {
	lines := strings.Split(content, "\n")

	imgsByLine := map[int][]links.ImageLink{}
	for _, im := range links.FindImages(content) {
		imgsByLine[im.Line] = append(imgsByLine[im.Line], im)
	}

	type block struct {
		start, end int
		fenced     bool // contains a fence line
	}
	var blocks []block
	inFence := false
	var fenceMark byte
	cur := -1 // index into blocks of the open block, -1 when none
	sawBlank := false
	for i, line := range lines {
		isFence := false
		if ch, ok := fenceDelim(line); ok {
			isFence = true
			if !inFence {
				inFence, fenceMark = true, ch
			} else if ch == fenceMark {
				inFence = false
			}
		}
		inside := inFence || isFence
		if !inside && strings.TrimSpace(line) == "" {
			sawBlank = true
			continue
		}
		startsNew := cur < 0 || (sawBlank && !indented(line))
		if startsNew {
			blocks = append(blocks, block{start: i, end: i})
			cur = len(blocks) - 1
		} else {
			blocks[cur].end = i
		}
		if isFence {
			blocks[cur].fenced = true
		}
		sawBlank = false
	}

	var segs []Segment
	for _, b := range blocks {
		md := strings.Join(lines[b.start:b.end+1], "\n")
		var imgs []links.ImageLink
		imageOnly := !b.fenced
		for i := b.start; i <= b.end; i++ {
			imgs = append(imgs, imgsByLine[i]...)
			if imageOnly && strings.TrimSpace(lines[i]) != "" && !links.ImageOnlyParagraph(lines[i]) {
				imageOnly = false
			}
		}
		if imageOnly && len(imgs) > 0 && !codeIndent(lines[b.start]) {
			segs = append(segs, Segment{Kind: Image, Markdown: md, Images: imgs, StartLine: b.start, EndLine: b.end})
			continue
		}
		segs = append(segs, Segment{Kind: Text, Markdown: md, StartLine: b.start, EndLine: b.end})
		if len(imgs) > 0 {
			first, last := imgs[0].Line, imgs[len(imgs)-1].Line
			var marks []string
			for _, im := range imgs {
				marks = append(marks, lines[im.Line][im.Start:im.End])
			}
			segs = append(segs, Segment{Kind: Image, Markdown: strings.Join(marks, "\n"), Images: imgs, StartLine: first, EndLine: last})
		}
	}
	return segs
}

// indented reports whether line starts with a space or a tab.
func indented(line string) bool {
	return line != "" && (line[0] == ' ' || line[0] == '\t')
}

// codeIndent reports whether line is indented by 4 columns or more (an
// indented code block in CommonMark).
func codeIndent(line string) bool {
	w := 0
	for i := 0; i < len(line); i++ {
		switch line[i] {
		case ' ':
			w++
		case '\t':
			w += 4 - w%4
		default:
			return w >= 4
		}
	}
	return false
}

// segmentForLine returns the index of the segment covering source line: the
// last segment starting at or before it (so blank lines belong to the
// segment above), or 0 for lines before the first segment. It returns -1
// when there are no segments.
func segmentForLine(segs []Segment, line int) int {
	if len(segs) == 0 {
		return -1
	}
	best := 0
	for i, s := range segs {
		if s.StartLine > line {
			break
		}
		// A follow-up image block lies inside its text segment's lines;
		// the text segment stands for them.
		if s.Kind == Image && i > 0 && s.StartLine <= segs[i-1].EndLine {
			continue
		}
		best = i
	}
	return best
}

// fenceDelim reports whether line opens or closes a fenced code block: at
// least three backticks or tildes after at most three columns of
// indentation (the rule used by the links and tasks packages).
func fenceDelim(line string) (byte, bool) {
	w, i := 0, 0
	for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
		if line[i] == ' ' {
			w++
		} else {
			w += 4 - w%4
		}
		i++
	}
	if w > 3 {
		return 0, false
	}
	rest := line[i:]
	if len(rest) < 3 {
		return 0, false
	}
	c := rest[0]
	if (c == '`' || c == '~') && rest[1] == c && rest[2] == c {
		return c, true
	}
	return 0, false
}
