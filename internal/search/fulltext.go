package search

import (
	"context"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/mathieucroset/notty/internal/index"
)

// ctxCheckLines is how often (in lines) a long note re-checks cancellation.
const ctxCheckLines = 256

// Query is a parsed full-text query. Terms, Phrases and Tags are
// lowercased; In is a folder path prefix without surrounding slashes.
type Query struct {
	Terms, Phrases, Tags []string
	In                   string
}

// Hit is one matching line. Line is 0-based. Matches are byte ranges
// [start, end) in Text, sorted and non-overlapping. Context is the next
// line, or the previous one when Text is the note's last line.
type Hit struct {
	Path, Title string
	Line        int
	Text        string
	Context     string
	Matches     [][2]int
}

// ParseQuery parses the full-text query syntax (spec §8): plain terms,
// "exact phrases", #tags and in:Folder (or in:"Folder With Spaces"),
// separated by whitespace. An unterminated quote extends to the end of the
// input. Empty phrases, a bare "#" and an empty in: are ignored; with
// several in: filters the last wins.
func ParseQuery(s string) Query {
	var q Query
	i := 0
	for i < len(s) {
		r, size := utf8.DecodeRuneInString(s[i:])
		if unicode.IsSpace(r) {
			i += size
			continue
		}
		if s[i] == '"' {
			var phrase string
			phrase, i = quoted(s, i+1)
			if strings.TrimSpace(phrase) != "" {
				q.Phrases = append(q.Phrases, strings.ToLower(phrase))
			}
			continue
		}
		start := i
		i = tokenEnd(s, i)
		tok := s[start:i]
		switch {
		case len(tok) >= 3 && strings.EqualFold(tok[:3], "in:"):
			val := tok[3:]
			if strings.HasPrefix(val, `"`) {
				val, i = quoted(s, start+4)
			}
			if val = strings.Trim(strings.TrimSpace(val), "/"); val != "" {
				q.In = val
			}
		case strings.HasPrefix(tok, "#"):
			if tag := tok[1:]; tag != "" {
				q.Tags = append(q.Tags, strings.ToLower(tag))
			}
		default:
			q.Terms = append(q.Terms, strings.ToLower(tok))
		}
	}
	return q
}

// quoted returns the text from start up to the next '"' and the index just
// past that quote, or the rest of s and len(s) when there is none.
func quoted(s string, start int) (string, int) {
	if start > len(s) {
		return "", len(s)
	}
	if j := strings.IndexByte(s[start:], '"'); j >= 0 {
		return s[start : start+j], start + j + 1
	}
	return s[start:], len(s)
}

// tokenEnd returns the index of the first whitespace rune at or after i.
func tokenEnd(s string, i int) int {
	for i < len(s) {
		r, size := utf8.DecodeRuneInString(s[i:])
		if unicode.IsSpace(r) {
			break
		}
		i += size
	}
	return i
}

// FullText searches notes for q. A note qualifies when every term and
// phrase occurs in its content (case-insensitive substring), it carries
// every tag (nested semantics, as index.HasTag), and its path lies under
// q.In (case-insensitive) when set. Each line of a qualifying note that
// contains a term or phrase is a hit; for a query with only tags and in:,
// the note's first non-empty line is its single hit. Hits are ordered by
// path, then line. At most limit hits are returned (no limit when limit <=
// 0). An empty query returns nil, and so does a cancelled ctx.
func FullText(ctx context.Context, q Query, notes []*index.Note, limit int) []Hit {
	var needles []string
	for _, n := range append(append([]string(nil), q.Terms...), q.Phrases...) {
		if n != "" {
			needles = append(needles, n)
		}
	}
	in := strings.ToLower(strings.Trim(q.In, "/"))
	if len(needles) == 0 && len(q.Tags) == 0 && in == "" {
		return nil
	}
	if !sort.SliceIsSorted(notes, func(i, j int) bool { return notes[i].Path < notes[j].Path }) {
		notes = append([]*index.Note(nil), notes...)
		sort.Slice(notes, func(i, j int) bool { return notes[i].Path < notes[j].Path })
	}

	var hits []Hit
	full := func() bool { return limit > 0 && len(hits) >= limit }
	for _, n := range notes {
		if ctx.Err() != nil {
			return nil
		}
		if !qualifies(n, needles, q.Tags, in) {
			continue
		}
		lines := splitLines(n.Content)
		if len(needles) == 0 {
			li := firstNonEmpty(lines)
			hits = append(hits, newHit(n, lines, li, nil))
			if full() {
				return hits
			}
			continue
		}
		for li, line := range lines {
			if li%ctxCheckLines == ctxCheckLines-1 && ctx.Err() != nil {
				return nil
			}
			if ms := matchRanges(line, needles); ms != nil {
				hits = append(hits, newHit(n, lines, li, ms))
				if full() {
					return hits
				}
			}
		}
	}
	return hits
}

// qualifies reports whether n passes the folder, tag and needle filters.
func qualifies(n *index.Note, needles, tags []string, in string) bool {
	if in != "" {
		p := strings.ToLower(n.Path)
		if p != in && !strings.HasPrefix(p, in+"/") {
			return false
		}
	}
	for _, t := range tags {
		if !index.HasTag(n, t) {
			return false
		}
	}
	for _, nd := range needles {
		if s, _ := indexFold(n.Content, nd); s < 0 {
			return false
		}
	}
	return true
}

func newHit(n *index.Note, lines []string, li int, ms [][2]int) Hit {
	var ctxLine string
	switch {
	case li+1 < len(lines):
		ctxLine = lines[li+1]
	case li > 0:
		ctxLine = lines[li-1]
	}
	return Hit{Path: n.Path, Title: n.Title, Line: li, Text: lines[li], Context: ctxLine, Matches: ms}
}

// splitLines splits content into lines without their "\r\n" / "\n"
// terminators. A final terminator does not start an extra empty line.
func splitLines(content string) []string {
	lines := strings.Split(content, "\n")
	if len(lines) > 1 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	for i, l := range lines {
		lines[i] = strings.TrimSuffix(l, "\r")
	}
	return lines
}

// firstNonEmpty returns the index of the first line that is not blank, or
// 0 when every line is.
func firstNonEmpty(lines []string) int {
	for i, l := range lines {
		if strings.TrimSpace(l) != "" {
			return i
		}
	}
	return 0
}

// matchRanges returns the merged byte ranges of every occurrence of every
// needle in line, or nil when none occurs.
func matchRanges(line string, needles []string) [][2]int {
	var ms [][2]int
	for _, nd := range needles {
		for off := 0; off < len(line); {
			s, e := indexFold(line[off:], nd)
			if s < 0 {
				break
			}
			ms = append(ms, [2]int{off + s, off + e})
			_, size := utf8.DecodeRuneInString(line[off+s:])
			off += s + size
		}
	}
	if len(ms) == 0 {
		return nil
	}
	sort.Slice(ms, func(i, j int) bool {
		if ms[i][0] != ms[j][0] {
			return ms[i][0] < ms[j][0]
		}
		return ms[i][1] < ms[j][1]
	})
	out := ms[:1]
	for _, m := range ms[1:] {
		last := &out[len(out)-1]
		if m[0] < last[1] {
			last[1] = max(last[1], m[1])
		} else {
			out = append(out, m)
		}
	}
	return out
}

// indexFold returns the byte range [start, end) in s of the first
// occurrence of needle under Unicode simple case folding, or -1, -1. The
// range refers to s itself, whose matched bytes may differ in length from
// needle's (for example the Kelvin sign matching "k"). An empty needle
// never matches.
func indexFold(s, needle string) (int, int) {
	if needle == "" {
		return -1, -1
	}
	first, _ := utf8.DecodeRuneInString(needle)
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if equalFoldRune(r, first) {
			if n, ok := prefixFold(s[i:], needle); ok {
				return i, i + n
			}
		}
		i += size
	}
	return -1, -1
}

// prefixFold reports whether s starts with needle under simple case
// folding, and the length in bytes of the matching prefix of s.
func prefixFold(s, needle string) (int, bool) {
	i := 0
	for _, nr := range needle {
		if i >= len(s) {
			return 0, false
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if !equalFoldRune(r, nr) {
			return 0, false
		}
		i += size
	}
	return i, true
}

// equalFoldRune reports whether a and b are equal under simple case
// folding, as strings.EqualFold compares runes.
func equalFoldRune(a, b rune) bool {
	if a == b {
		return true
	}
	if a < utf8.RuneSelf && b < utf8.RuneSelf {
		if 'A' <= a && a <= 'Z' {
			a += 'a' - 'A'
		}
		if 'A' <= b && b <= 'Z' {
			b += 'a' - 'A'
		}
		return a == b
	}
	r := unicode.SimpleFold(a)
	for r != a && r != b {
		r = unicode.SimpleFold(r)
	}
	return r == b
}
