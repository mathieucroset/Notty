// Package links implements parsing, resolution, and rewriting of markdown
// image links, as used by Notty's preview and attachment handling (see
// docs/superpowers/specs/2026-09-29-notty-design.md §6.2 "Adding images").
package links

import (
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"
)

// ImageLink is a single "![alt](target)" occurrence found in note content.
// Start and End are byte offsets within the containing line, covering the
// whole markup (including "![", "]", "(", ")", and any title).
type ImageLink struct {
	Line, Start, End int
	Alt, Target      string
}

// imgRe matches "![alt](target)" with an optional quoted title, and an
// optional angle-bracket target. Capturing groups: 1=alt, 2=target.
var imgRe = regexp.MustCompile(`!\[([^\]]*)\]\((<[^>]*>|[^)\s]+)(?:\s+"[^"]*")?\)`)

// imageMatch is the internal, fuller record of a single image match within
// a line: it additionally tracks the byte span of the raw target token
// (including its angle brackets, if any) so callers that need to rewrite
// only the target can splice precisely, leaving the alt text and any title
// untouched.
type imageMatch struct {
	line                   int
	start, end             int // whole "![alt](target ...)" span
	targetStart, targetEnd int // span of the raw target token in the line
	alt                    string
	target                 string // decoded target, angle brackets stripped
	bracketed              bool
}

// findImageMatches scans content for image links, skipping fenced code
// blocks (``` or ~~~) and inline code spans (`...`).
func findImageMatches(content string) []imageMatch {
	lines := strings.Split(content, "\n")
	var result []imageMatch
	inFence := false
	var fenceMark byte
	for lineNo, line := range lines {
		if ch, ok := fenceDelim(line); ok {
			if !inFence {
				inFence = true
				fenceMark = ch
			} else if ch == fenceMark {
				inFence = false
			}
			continue
		}
		if inFence {
			continue
		}

		spans := codeSpans(line)
		matches := imgRe.FindAllStringSubmatchIndex(line, -1)
		for _, m := range matches {
			start, end := m[0], m[1]
			if inSpan(spans, start) {
				continue
			}
			alt := line[m[2]:m[3]]
			targetStart, targetEnd := m[4], m[5]
			rawTarget := line[targetStart:targetEnd]
			bracketed := len(rawTarget) >= 2 && rawTarget[0] == '<' && rawTarget[len(rawTarget)-1] == '>'
			target := rawTarget
			if bracketed {
				target = rawTarget[1 : len(rawTarget)-1]
			}
			result = append(result, imageMatch{
				line:        lineNo,
				start:       start,
				end:         end,
				targetStart: targetStart,
				targetEnd:   targetEnd,
				alt:         alt,
				target:      target,
				bracketed:   bracketed,
			})
		}
	}
	return result
}

// FindImages returns every image link in content, skipping fenced code
// blocks (``` or ~~~) and inline code spans (`...`).
func FindImages(content string) []ImageLink {
	matches := findImageMatches(content)
	result := make([]ImageLink, 0, len(matches))
	for _, m := range matches {
		result = append(result, ImageLink{
			Line:   m.line,
			Start:  m.start,
			End:    m.end,
			Alt:    m.alt,
			Target: m.target,
		})
	}
	return result
}

// fenceDelim reports whether line opens or closes a fenced code block: a
// run of at least 3 backticks or tildes preceded by at most 3 leading
// spaces of indentation, per CommonMark (4 or more leading spaces makes it
// an indented code block instead, not a fence). A leading tab counts as
// advancing to the next multiple of 4.
func fenceDelim(line string) (byte, bool) {
	width := 0
	i := 0
loop:
	for i < len(line) {
		switch line[i] {
		case ' ':
			width++
			i++
		case '\t':
			width += 4 - (width % 4)
			i++
		default:
			break loop
		}
	}
	if width > 3 {
		return 0, false
	}
	rest := line[i:]
	if len(rest) < 3 {
		return 0, false
	}
	c := rest[0]
	if c != '`' && c != '~' {
		return 0, false
	}
	if rest[0] == c && rest[1] == c && rest[2] == c {
		return c, true
	}
	return 0, false
}

type byteSpan struct{ start, end int }

// codeSpans finds inline code spans (delimited by matching runs of
// backticks of equal length) within a single line.
func codeSpans(line string) []byteSpan {
	type run struct{ start, length int }
	var runs []run
	i := 0
	for i < len(line) {
		if line[i] == '`' {
			j := i
			for j < len(line) && line[j] == '`' {
				j++
			}
			runs = append(runs, run{i, j - i})
			i = j
		} else {
			i++
		}
	}

	var spans []byteSpan
	i = 0
	for i < len(runs) {
		opener := runs[i]
		matched := -1
		for j := i + 1; j < len(runs); j++ {
			if runs[j].length == opener.length {
				matched = j
				break
			}
		}
		if matched == -1 {
			i++
			continue
		}
		closer := runs[matched]
		spans = append(spans, byteSpan{opener.start, closer.start + closer.length})
		i = matched + 1
	}
	return spans
}

func inSpan(spans []byteSpan, pos int) bool {
	for _, s := range spans {
		if pos >= s.start && pos < s.end {
			return true
		}
	}
	return false
}

func isExternal(target string) bool {
	lower := strings.ToLower(target)
	return strings.HasPrefix(lower, "http://") ||
		strings.HasPrefix(lower, "https://") ||
		strings.HasPrefix(lower, "data:")
}

func decode(s string) string {
	if u, err := url.PathUnescape(s); err == nil {
		return u
	}
	return s
}

// Resolve resolves an image target relative to noteRel (the vault-relative
// path of the note containing it):
//   - A leading "/" means relative to the vault root: "/x/y.png" -> "x/y.png".
//   - "http://", "https://", and "data:" targets are external.
//   - Anything else is resolved relative to the note's directory.
//
// The returned vaultRel is always a cleaned, vault-relative path with no
// leading "/". If resolution would escape the vault (a path starting with
// "../" after cleaning), Resolve returns ("", false) instead.
func Resolve(target, noteRel string) (vaultRel string, external bool) {
	if isExternal(target) {
		return "", true
	}

	decoded := decode(target)

	var candidate string
	if strings.HasPrefix(decoded, "/") {
		candidate = strings.TrimPrefix(decoded, "/")
	} else {
		candidate = path.Join(path.Dir(noteRel), decoded)
	}
	candidate = path.Clean(candidate)

	if candidate == ".." || strings.HasPrefix(candidate, "../") {
		return "", false
	}
	return candidate, false
}

// RewriteForMove rewrites note-relative image links in content that resolve
// (from oldNoteRel) to a file that exists in the vault, so they remain
// valid after the note moves to newNoteRel. Root-relative ("/...") and
// external links are left untouched, as are links that do not resolve to
// an existing vault file.
//
// Only the target token is replaced; the alt text and any title are left
// byte-for-byte untouched. If the target was originally wrapped in angle
// brackets, the rewritten target keeps them; if it wasn't, but the new
// target contains a space or ')' (which would otherwise break the plain
// "(target)" syntax), it is wrapped in angle brackets.
//
// Returns the (possibly unchanged) content and whether anything changed.
func RewriteForMove(content, oldNoteRel, newNoteRel string, exists func(vaultRel string) bool) (string, bool) {
	matches := findImageMatches(content)
	if len(matches) == 0 {
		return content, false
	}

	byLine := make(map[int][]imageMatch)
	for _, m := range matches {
		byLine[m.line] = append(byLine[m.line], m)
	}

	lines := strings.Split(content, "\n")
	changed := false
	newDir := path.Dir(newNoteRel)

	for lineNo, ms := range byLine {
		// Process rightmost matches first so earlier byte offsets on the
		// same line stay valid as we splice.
		sort.Slice(ms, func(i, j int) bool { return ms[i].targetStart > ms[j].targetStart })
		line := lines[lineNo]
		for _, m := range ms {
			if strings.HasPrefix(m.target, "/") {
				continue
			}
			vaultRel, external := Resolve(m.target, oldNoteRel)
			if external || vaultRel == "" {
				continue
			}
			if !exists(vaultRel) {
				continue
			}
			newTarget := RelPath(newDir, vaultRel)
			replacement := newTarget
			if m.bracketed || strings.ContainsAny(newTarget, " )") {
				replacement = "<" + newTarget + ">"
			}
			line = line[:m.targetStart] + replacement + line[m.targetEnd:]
			changed = true
		}
		lines[lineNo] = line
	}

	if !changed {
		return content, false
	}
	return strings.Join(lines, "\n"), true
}

// ImageOnlyParagraph reports whether line consists only of one or more
// image links separated by whitespace.
func ImageOnlyParagraph(line string) bool {
	matches := imgRe.FindAllStringIndex(line, -1)
	if len(matches) == 0 {
		return false
	}
	last := 0
	for _, m := range matches {
		if strings.TrimSpace(line[last:m[0]]) != "" {
			return false
		}
		last = m[1]
	}
	return strings.TrimSpace(line[last:]) == ""
}

// RelPath computes the "/"-separated relative path from fromDir (a
// vault-relative directory, or "." for the vault root) to toRel (a
// vault-relative file path).
func RelPath(fromDir, toRel string) string {
	fromDir = path.Clean(fromDir)
	toRel = path.Clean(toRel)

	if fromDir == "." {
		return toRel
	}

	fromParts := strings.Split(fromDir, "/")
	toDir := path.Dir(toRel)
	toBase := path.Base(toRel)

	var toDirParts []string
	if toDir != "." {
		toDirParts = strings.Split(toDir, "/")
	}

	n := 0
	for n < len(fromParts) && n < len(toDirParts) && fromParts[n] == toDirParts[n] {
		n++
	}

	ups := len(fromParts) - n
	parts := make([]string, 0, ups+len(toDirParts)-n+1)
	for i := 0; i < ups; i++ {
		parts = append(parts, "..")
	}
	parts = append(parts, toDirParts[n:]...)
	parts = append(parts, toBase)
	return strings.Join(parts, "/")
}
