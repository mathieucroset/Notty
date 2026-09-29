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

// sanitizeName applies the spec §3 filename rules without an extension:
// forbidden and control characters removed, leading dots removed, and
// surrounding whitespace trimmed. It may return "".
func sanitizeName(s string) string {
	s = strings.Map(func(r rune) rune {
		if strings.ContainsRune(forbiddenChars, r) || unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
	// Trim and strip dots until stable so " .x" and ". x" cannot yield a
	// dotfile or leading whitespace.
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
	name := sanitizeName(title)
	if name == "" {
		name = untitled
	}
	return name + ".md"
}

// Title returns a note's title: its first ATX H1 ("# ") heading outside
// fenced code blocks, or else the file name without ".md".
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
	return strings.TrimSuffix(path.Base(clean(rel)), ".md")
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
