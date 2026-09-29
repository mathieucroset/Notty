package preview

import (
	"strings"
	"unicode/utf8"
)

// tabWidth is the tab stop used when expanding tabs in text segments.
const tabWidth = 4

// prepareText turns a text segment into the markdown handed to Glamour:
// carriage returns are dropped and tabs expanded to 4-column stops, so no
// control character reaches the frame.
func prepareText(md string) string {
	if !strings.ContainsAny(md, "\t\r") {
		return md
	}
	lines := strings.Split(md, "\n")
	for i, l := range lines {
		lines[i] = expandTabs(strings.ReplaceAll(l, "\r", ""))
	}
	return strings.Join(lines, "\n")
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
