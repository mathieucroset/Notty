package dialog

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// padLine pads or truncates s (which may contain ANSI escape codes) to
// exactly width w, measured in visible columns.
func padLine(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if sw := ansi.StringWidth(s); sw > w {
		return ansi.Truncate(s, w, "")
	} else if sw < w {
		return s + strings.Repeat(" ", w-sw)
	}
	return s
}

// wrapLines word-wraps s to width w, returning one string per visual line.
// A non-positive width leaves s untouched.
func wrapLines(s string, w int) []string {
	if w <= 0 {
		return []string{s}
	}
	return strings.Split(ansi.Wordwrap(s, w, ""), "\n")
}
