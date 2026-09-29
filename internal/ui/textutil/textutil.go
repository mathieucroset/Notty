// Package textutil provides the small set of ANSI-aware text-layout helpers
// shared by Notty's overlay-based UI components (dialogs, toasts, and the
// error log): word-wrapping that never silently drops an overlong word,
// exact-width padding that's safe at wide-grapheme boundaries, and fitting a
// block of lines to an exact width and height.
package textutil

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// Wrap word-wraps s to width w, hard-breaking any word longer than w so it
// is never silently truncated or dropped. It preserves ANSI styling. A
// non-positive width returns s unwrapped, as a single line.
func Wrap(s string, w int) []string {
	if w <= 0 {
		return []string{s}
	}
	// ansi.Wrap (unlike ansi.Wordwrap) breaks word boundaries when a word
	// doesn't fit on its own line, which is exactly what keeps a long
	// unbreakable token (a path, a URL, ...) from being cut off.
	return strings.Split(ansi.Wrap(s, w, ""), "\n")
}

// PadLine truncates or pads s (which may contain ANSI escape codes) to
// exactly width w, measured in visible columns. When a wide grapheme would
// straddle the boundary, it is dropped rather than split, and the line is
// padded back out to w with spaces so the result is always exactly w wide.
func PadLine(s string, w int) string {
	if w <= 0 {
		return ""
	}
	trunc := ansi.Truncate(s, w, "")
	if tw := ansi.StringWidth(trunc); tw < w {
		trunc += strings.Repeat(" ", w-tw)
	}
	return trunc
}

// FitBlock pads or truncates lines to exactly h entries, each exactly w
// columns wide: extra lines are dropped, missing lines are blank, and every
// kept line is padded via PadLine.
func FitBlock(lines []string, w, h int) []string {
	out := make([]string, h)
	blank := strings.Repeat(" ", max(w, 0))
	for i := range h {
		if i < len(lines) {
			out[i] = PadLine(lines[i], w)
		} else {
			out[i] = blank
		}
	}
	return out
}
