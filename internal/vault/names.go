package vault

import (
	"path"
	"strings"
	"unicode"
)

// untitled is the base name used when a title sanitizes to nothing.
const untitled = "Untitled"

// forbiddenChars are removed from titles when deriving file names (spec §3).
const forbiddenChars = `/\:*?"<>|`

// noteExt is the extension of note files. Matching is case-insensitive;
// names the vault creates always use this lowercase form.
const noteExt = ".md"

// sanitizeName applies the spec §3 filename rules without an extension:
// forbidden and control characters removed, leading dots removed, and
// surrounding whitespace trimmed. It may return "".
func sanitizeName(s string) string { return trimName(removeForbidden(s)) }

// noteBase is sanitizeName for note names: a trailing ".md" (any case) is
// dropped so callers can append the canonical extension without doubling
// it. It may return "".
func noteBase(s string) string {
	s = strings.TrimSpace(removeForbidden(s))
	if hasNoteExt(s) {
		s = s[:len(s)-len(noteExt)]
	}
	return trimName(s)
}

// hasNoteExt reports whether name ends in ".md", ignoring case.
func hasNoteExt(name string) bool {
	n := len(name) - len(noteExt)
	return n >= 0 && strings.EqualFold(name[n:], noteExt)
}

func removeForbidden(s string) string {
	return strings.Map(func(r rune) rune {
		if strings.ContainsRune(forbiddenChars, r) || unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
}

// trimName trims whitespace and strips leading dots until stable, so " .x"
// and ". x" cannot yield a dotfile or leading whitespace.
func trimName(s string) string {
	for {
		t := strings.TrimLeft(strings.TrimSpace(s), ".")
		if t == s {
			return s
		}
		s = t
	}
}

// FileNameFromTitle derives a note file name from a title (spec §3).
func FileNameFromTitle(title string) string {
	name := noteBase(title)
	if name == "" {
		name = untitled
	}
	return name + noteExt
}

// Title returns a note's title: its first ATX H1 ("# ") heading outside
// fenced code blocks, or else the file name without ".md" (any case).
func Title(content, rel string) string {
	var fenceChar byte
	var fenceLen int
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSuffix(line, "\r")
		if c, n, rest := fence(line); n > 0 {
			switch {
			case fenceLen == 0:
				fenceChar, fenceLen = c, n
			case c == fenceChar && n >= fenceLen && strings.TrimSpace(rest) == "":
				fenceLen = 0
			}
			continue
		}
		if fenceLen > 0 || !strings.HasPrefix(line, "# ") {
			continue
		}
		if t := headingText(line[2:]); t != "" {
			return t
		}
	}
	name := path.Base(clean(rel))
	if hasNoteExt(name) {
		name = name[:len(name)-len(noteExt)]
	}
	return name
}

// fence reports whether line opens or closes a code fence: up to three
// spaces of indentation followed by at least three ` or ~ characters. It
// returns the fence character, its run length, and the rest of the line.
func fence(line string) (c byte, n int, rest string) {
	s := line
	for i := 0; i < 3 && strings.HasPrefix(s, " "); i++ {
		s = s[1:]
	}
	if s == "" || (s[0] != '`' && s[0] != '~') {
		return 0, 0, ""
	}
	c = s[0]
	for n < len(s) && s[n] == c {
		n++
	}
	if n < 3 {
		return 0, 0, ""
	}
	return c, n, s[n:]
}

// headingText trims an ATX heading's content and its optional closing
// sequence of '#' characters.
func headingText(s string) string {
	s = strings.TrimSpace(s)
	if t := strings.TrimRight(s, "#"); t != s && (t == "" || strings.HasSuffix(t, " ")) {
		s = strings.TrimSpace(t)
	}
	return s
}
