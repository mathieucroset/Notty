// Package tags extracts #tags from markdown note content (spec §8).
//
// A tag is '#' followed by a letter, then any run of letters, digits, '_',
// '-' or '/'. A trailing '/' is not part of the tag. The '#' must be at the
// start of the line or preceded by whitespace; a '#' directly after any
// other character (URL fragments, anchor links like "[x](#x)", "a#b", "##",
// "(#x)") is not a tag. Tags inside fenced code blocks and inline code spans
// are ignored.
package tags

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Parse returns the unique tags in content, in first-seen order and without
// the leading '#'. Tags are deduplicated case-insensitively; the first
// spelling seen wins. It returns nil when content has no tags.
func Parse(content string) []string {
	var (
		out    []string
		seen   map[string]bool
		fence  string // opening fence marker while inside a fenced block
		inside bool
	)
	for line := range strings.SplitSeq(content, "\n") {
		line = strings.TrimSuffix(line, "\r")
		if inside {
			if IsFenceClose(line, fence) {
				inside = false
			}
			continue
		}
		if marker, _, ok := FenceOpen(line); ok {
			inside, fence = true, marker
			continue
		}
		for _, r := range FindTags(line) {
			tag := line[r[0]+1 : r[1]]
			key := strings.ToLower(tag)
			if seen[key] {
				continue
			}
			if seen == nil {
				seen = make(map[string]bool)
			}
			seen[key] = true
			out = append(out, tag)
		}
	}
	return out
}

// FindTags returns the byte ranges [start, end) of every tag in a single
// line, each range including the leading '#'. It has no fence state: callers
// must skip lines inside fenced code blocks themselves.
func FindTags(line string) [][2]int {
	if strings.IndexByte(line, '#') < 0 {
		return nil
	}
	code := codeSpans(line)
	var out [][2]int
	for i := 0; i < len(line); i++ {
		if line[i] != '#' {
			continue
		}
		if inRanges(code, i) || !boundaryBefore(line, i) {
			continue
		}
		end := tagEnd(line, i+1)
		if end > i+1 {
			out = append(out, [2]int{i, end})
			i = end - 1
		}
	}
	return out
}

// boundaryBefore reports whether a '#' at byte i may start a tag.
func boundaryBefore(line string, i int) bool {
	if i == 0 {
		return true
	}
	r, _ := utf8.DecodeLastRuneInString(line[:i])
	return unicode.IsSpace(r)
}

// tagEnd returns the end offset of the tag word starting at start (just after
// the '#'), or start when there is no valid tag word.
func tagEnd(line string, start int) int {
	r, size := utf8.DecodeRuneInString(line[start:])
	if size == 0 || !unicode.IsLetter(r) {
		return start
	}
	end := start + size
	for end < len(line) {
		r, size = utf8.DecodeRuneInString(line[end:])
		if !isWordRune(r) {
			break
		}
		end += size
	}
	for end > start && line[end-1] == '/' {
		end--
	}
	return end
}

func isWordRune(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsMark(r) ||
		r == '_' || r == '-' || r == '/'
}

// codeSpans returns the byte ranges of inline code spans (including their
// backtick delimiters). A backtick run closes only on a run of the same
// length; an unmatched run is literal text.
func codeSpans(line string) [][2]int {
	if strings.IndexByte(line, '`') < 0 {
		return nil
	}
	var out [][2]int
	for i := 0; i < len(line); {
		if line[i] != '`' {
			i++
			continue
		}
		n := runLen(line, i, '`')
		if closeAt := findBacktickRun(line, i+n, n); closeAt >= 0 {
			out = append(out, [2]int{i, closeAt + n})
			i = closeAt + n
		} else {
			i += n
		}
	}
	return out
}

// findBacktickRun finds the start of the next backtick run of exactly n
// backticks at or after from, or -1.
func findBacktickRun(line string, from, n int) int {
	for j := from; j < len(line); {
		if line[j] != '`' {
			j++
			continue
		}
		m := runLen(line, j, '`')
		if m == n {
			return j
		}
		j += m
	}
	return -1
}

func runLen(s string, i int, c byte) int {
	n := 0
	for i+n < len(s) && s[i+n] == c {
		n++
	}
	return n
}

func inRanges(rs [][2]int, i int) bool {
	for _, r := range rs {
		if i >= r[0] && i < r[1] {
			return true
		}
	}
	return false
}

// FenceOpen reports whether line opens a fenced code block: up to three
// spaces, then three or more '`' or '~'. It returns the fence marker (the
// full run, e.g. "````") and the language (first word of the info string).
// A backtick fence's info string may not contain a backtick.
func FenceOpen(line string) (marker, lang string, ok bool) {
	i := leadingSpaces(line)
	if i > 3 || i >= len(line) || (line[i] != '`' && line[i] != '~') {
		return "", "", false
	}
	c := line[i]
	n := runLen(line, i, c)
	if n < 3 {
		return "", "", false
	}
	info := line[i+n:]
	if c == '`' && strings.IndexByte(info, '`') >= 0 {
		return "", "", false
	}
	if f := strings.Fields(info); len(f) > 0 {
		lang = f[0]
	}
	return line[i : i+n], lang, true
}

// IsFenceClose reports whether line closes a fence opened with marker: up to
// three spaces, a run of the same character at least as long as marker, then
// only whitespace.
func IsFenceClose(line, marker string) bool {
	if marker == "" {
		return false
	}
	i := leadingSpaces(line)
	if i > 3 || i >= len(line) || line[i] != marker[0] {
		return false
	}
	n := runLen(line, i, marker[0])
	return n >= len(marker) && strings.TrimSpace(line[i+n:]) == ""
}

func leadingSpaces(s string) int {
	i := 0
	for i < len(s) && s[i] == ' ' {
		i++
	}
	return i
}
