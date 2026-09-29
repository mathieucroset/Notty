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

// FindImages returns every image link in content, skipping fenced code
// blocks (``` or ~~~) and inline code spans (`...`).
func FindImages(content string) []ImageLink {
	lines := strings.Split(content, "\n")
	var result []ImageLink
	inFence := false
	var fenceChar byte
	for lineNo, line := range lines {
		trimmed := strings.TrimLeft(line, " \t")
		if isFenceDelim(trimmed) {
			ch := trimmed[0]
			if !inFence {
				inFence = true
				fenceChar = ch
			} else if ch == fenceChar {
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
			target := line[m[4]:m[5]]
			if len(target) >= 2 && target[0] == '<' && target[len(target)-1] == '>' {
				target = target[1 : len(target)-1]
			}
			result = append(result, ImageLink{
				Line:   lineNo,
				Start:  start,
				End:    end,
				Alt:    alt,
				Target: target,
			})
		}
	}
	return result
}

// isFenceDelim reports whether s (already left-trimmed) opens or closes a
// fenced code block: a run of at least 3 backticks or tildes.
func isFenceDelim(s string) bool {
	if len(s) < 3 {
		return false
	}
	c := s[0]
	if c != '`' && c != '~' {
		return false
	}
	return s[0] == c && s[1] == c && s[2] == c
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
// an existing vault file. Returns the (possibly unchanged) content and
// whether anything changed.
func RewriteForMove(content, oldNoteRel, newNoteRel string, exists func(vaultRel string) bool) (string, bool) {
	images := FindImages(content)
	if len(images) == 0 {
		return content, false
	}

	byLine := make(map[int][]ImageLink)
	for _, im := range images {
		byLine[im.Line] = append(byLine[im.Line], im)
	}

	lines := strings.Split(content, "\n")
	changed := false
	newDir := path.Dir(newNoteRel)

	for lineNo, ims := range byLine {
		sort.Slice(ims, func(i, j int) bool { return ims[i].Start > ims[j].Start })
		line := lines[lineNo]
		for _, im := range ims {
			if strings.HasPrefix(im.Target, "/") {
				continue
			}
			vaultRel, external := Resolve(im.Target, oldNoteRel)
			if external || vaultRel == "" {
				continue
			}
			if !exists(vaultRel) {
				continue
			}
			newTarget := RelPath(newDir, vaultRel)
			replacement := "![" + im.Alt + "](" + newTarget + ")"
			line = line[:im.Start] + replacement + line[im.End:]
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
