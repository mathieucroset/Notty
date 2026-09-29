package preview

import (
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/mathieucroset/notty/internal/links"
)

// tabWidth is the tab stop used when expanding tabs in text segments.
const tabWidth = 4

// refDef is a link reference definition ("[label]: target"), with its
// target already rewritten like inline links.
type refDef struct {
	label string // lower case
	line  string
}

var (
	// linkRe matches the "[text](target" start of an inline link;
	// groups: 1 = target (possibly in angle brackets).
	linkRe = regexp.MustCompile(`\[[^\]]*\]\((<[^>]*>|[^)\s]+)`)
	// defRe matches a link reference definition (footnotes excluded);
	// groups: 1 = label, 2 = target, 3 = rest (title).
	defRe = regexp.MustCompile(`^ {0,3}\[([^\]^][^\]]*)\]:[ \t]*(<[^>]*>|\S+)(.*)$`)
	// schemeRe matches a URL scheme ("https:", "mailto:").
	schemeRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]*:`)
)

// prepareText turns a text segment into the markdown handed to Glamour:
//   - its inline images (imgs, from the follow-up image block) become
//     chips "🖼 alt" in a code span, since the image itself renders right
//     after the paragraph (spec §6.1);
//   - note-relative link targets are rewritten to vault-root form
//     ("/dir/file.md"), which is how Glamour prints every relative URL;
//   - the reference definitions the segment uses are appended, so
//     reference-style links render although the definition lives in
//     another segment;
//   - carriage returns are dropped and tabs expanded to 4-column stops,
//     so no control character reaches the frame.
func prepareText(seg Segment, imgs []links.ImageLink, notePath string, defs []refDef) string {
	byLine := map[int][]links.ImageLink{}
	for _, im := range imgs {
		byLine[im.Line] = append(byLine[im.Line], im)
	}
	lines := strings.Split(seg.Markdown, "\n")
	inFence := false
	var fenceMark byte
	for i, line := range lines {
		if ch, ok := fenceDelim(line); ok {
			if !inFence {
				inFence, fenceMark = true, ch
			} else if ch == fenceMark {
				inFence = false
			}
		} else if !inFence {
			line = chipImages(line, byLine[seg.StartLine+i])
			line = rewriteLinks(line, notePath)
		}
		lines[i] = expandTabs(strings.ReplaceAll(line, "\r", ""))
	}
	md := strings.Join(lines, "\n")
	var used []string
	if len(defs) > 0 {
		lower := strings.ToLower(md)
		for _, d := range defs {
			if strings.Contains(lower, "["+d.label+"]") {
				used = append(used, d.line)
			}
		}
	}
	if len(used) > 0 {
		md += "\n\n" + strings.Join(used, "\n")
	}
	return md
}

// chipImages replaces the image links of one line with chips.
func chipImages(line string, imgs []links.ImageLink) string {
	if len(imgs) == 0 {
		return line
	}
	sort.Slice(imgs, func(a, b int) bool { return imgs[a].Start > imgs[b].Start })
	for _, im := range imgs {
		if im.Start < 0 || im.End > len(line) || im.Start >= im.End {
			continue
		}
		label := strings.TrimSpace(strings.ReplaceAll(im.Alt, "`", ""))
		if label == "" {
			label = imageName(im.Target)
		}
		line = line[:im.Start] + "`🖼 " + label + "`" + line[im.End:]
	}
	return line
}

// rewriteLinks rewrites the note-relative targets of the inline links of
// one line (outside code spans) to vault-root form.
func rewriteLinks(line, notePath string) string {
	if !strings.Contains(line, "](") {
		return line
	}
	spans := codeSpans(line)
	ms := linkRe.FindAllStringSubmatchIndex(line, -1)
	for k := len(ms) - 1; k >= 0; k-- {
		m := ms[k]
		if (m[0] > 0 && line[m[0]-1] == '!') || inSpans(spans, m[0]) {
			continue
		}
		if t, ok := vaultTarget(line[m[2]:m[3]], notePath); ok {
			line = line[:m[2]] + t + line[m[3]:]
		}
	}
	return line
}

// vaultTarget returns the vault-root form of a note-relative link target;
// false for absolute, external, anchor and escaping targets.
func vaultTarget(raw, notePath string) (string, bool) {
	t := strings.TrimSuffix(strings.TrimPrefix(raw, "<"), ">")
	if t == "" || strings.HasPrefix(t, "/") || strings.HasPrefix(t, "#") || schemeRe.MatchString(t) {
		return "", false
	}
	rel, external := links.Resolve(t, notePath)
	if external || rel == "" {
		return "", false
	}
	out := "/" + rel
	if strings.ContainsAny(out, " )") {
		out = "<" + out + ">"
	}
	return out, true
}

// collectDefs finds the link reference definitions of content, outside
// fenced code.
func collectDefs(content, notePath string) []refDef {
	if !strings.Contains(content, "]:") {
		return nil
	}
	var defs []refDef
	inFence := false
	var fenceMark byte
	for _, line := range strings.Split(content, "\n") {
		if ch, ok := fenceDelim(line); ok {
			if !inFence {
				inFence, fenceMark = true, ch
			} else if ch == fenceMark {
				inFence = false
			}
			continue
		}
		if inFence {
			continue
		}
		line = strings.TrimRight(line, "\r")
		m := defRe.FindStringSubmatchIndex(line)
		if m == nil {
			continue
		}
		if t, ok := vaultTarget(line[m[4]:m[5]], notePath); ok {
			line = line[:m[4]] + t + line[m[5]:]
		}
		defs = append(defs, refDef{label: strings.ToLower(line[m[2]:m[3]]), line: expandTabs(line)})
	}
	return defs
}

// codeSpans returns the byte ranges of the inline code spans of a line
// (backtick runs closed by a run of the same length).
func codeSpans(line string) [][2]int {
	var spans [][2]int
	for i := 0; i < len(line); {
		if line[i] != '`' {
			i++
			continue
		}
		j := i
		for j < len(line) && line[j] == '`' {
			j++
		}
		run := line[i:j]
		k := strings.Index(line[j:], run)
		for k >= 0 && j+k+len(run) < len(line) && line[j+k+len(run)] == '`' {
			// A longer run does not close the span; look further.
			n := strings.Index(line[j+k+len(run):], run)
			if n < 0 {
				k = -1
				break
			}
			k += len(run) + n
		}
		if k < 0 {
			i = j
			continue
		}
		end := j + k + len(run)
		spans = append(spans, [2]int{i, end})
		i = end
	}
	return spans
}

func inSpans(spans [][2]int, pos int) bool {
	for _, s := range spans {
		if pos >= s[0] && pos < s[1] {
			return true
		}
	}
	return false
}

// expandTabs replaces each tab with spaces up to the next multiple of
// tabWidth columns (counting runes).
func expandTabs(line string) string {
	if !strings.Contains(line, "\t") {
		return line
	}
	var b strings.Builder
	col := 0
	for _, r := range line {
		if r == '\t' {
			n := tabWidth - col%tabWidth
			b.WriteString(strings.Repeat(" ", n))
			col += n
			continue
		}
		b.WriteRune(r)
		if r != utf8.RuneError {
			col++
		}
	}
	return b.String()
}
