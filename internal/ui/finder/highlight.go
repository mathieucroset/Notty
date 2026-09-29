package finder

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// padLine truncates or pads s (which may already carry ANSI styling) to
// exactly width columns.
func padLine(s string, width int) string {
	if width <= 0 {
		return ""
	}
	switch w := ansi.StringWidth(s); {
	case w > width:
		return ansi.Truncate(s, width, "")
	case w < width:
		return s + strings.Repeat(" ", width-w)
	default:
		return s
	}
}

// styleRuneIndexes renders s with the runes at idx (rune indexes into s) in
// match and every other rune in base.
func styleRuneIndexes(s string, idx []int, base, match lipgloss.Style) string {
	if len(idx) == 0 {
		return base.Render(s)
	}
	set := make(map[int]bool, len(idx))
	for _, i := range idx {
		set[i] = true
	}
	var out, run strings.Builder
	runMatch, first := false, true
	flush := func() {
		if run.Len() == 0 {
			return
		}
		if runMatch {
			out.WriteString(match.Render(run.String()))
		} else {
			out.WriteString(base.Render(run.String()))
		}
		run.Reset()
	}
	for i, r := range []rune(s) {
		isMatch := set[i]
		if !first && isMatch != runMatch {
			flush()
		}
		first, runMatch = false, isMatch
		run.WriteRune(r)
	}
	flush()
	return out.String()
}

// styleByteRanges renders s with the byte ranges in spans (sorted,
// non-overlapping, referring to s itself) in match and everything else in
// base.
func styleByteRanges(s string, spans [][2]int, base, match lipgloss.Style) string {
	if len(spans) == 0 {
		return base.Render(s)
	}
	var out strings.Builder
	pos := 0
	for _, sp := range spans {
		start, end := sp[0], sp[1]
		if start < pos {
			start = pos
		}
		if start > len(s) {
			start = len(s)
		}
		if end > len(s) {
			end = len(s)
		}
		if end <= start {
			continue
		}
		if start > pos {
			out.WriteString(base.Render(s[pos:start]))
		}
		out.WriteString(match.Render(s[start:end]))
		pos = end
	}
	if pos < len(s) {
		out.WriteString(base.Render(s[pos:]))
	}
	return out.String()
}

// trimLeftToWidth returns the longest suffix of s, cut on a rune boundary,
// whose display width is at most w.
func trimLeftToWidth(s string, w int) string {
	if ansi.StringWidth(s) <= w {
		return s
	}
	runes := []rune(s)
	for i := 1; i < len(runes); i++ {
		if ansi.StringWidth(string(runes[i:])) <= w {
			return string(runes[i:])
		}
	}
	return ""
}

// highlightAround renders the plain-text line text, styling the byte ranges
// in spans (sorted, non-overlapping, referring to the ORIGINAL text) with
// match and everything else with base, truncated to exactly width columns.
// When text is too wide, it is truncated around the first span (keeping a
// little leading context) with a leading and/or trailing ellipsis in dim, so
// the nearest match to the start of the line stays visible.
func highlightAround(text string, spans [][2]int, width int, base, match, dim lipgloss.Style) string {
	if width <= 0 {
		return ""
	}
	if ansi.StringWidth(text) <= width {
		return padLine(styleByteRanges(text, spans, base, match), width)
	}
	if len(spans) == 0 {
		return padLine(ansi.Truncate(styleByteRanges(text, spans, base, match), width, "…"), width)
	}

	first := spans[0][0]
	prefix := text[:first]
	const leftCtxDivisor = 4
	leftCtx := width / leftCtxDivisor

	windowStart := 0
	leftEllipsis := false
	if ansi.StringWidth(prefix) > leftCtx {
		trimmed := trimLeftToWidth(prefix, leftCtx)
		windowStart = len(prefix) - len(trimmed)
		leftEllipsis = true
	}

	rest := text[windowStart:]
	shifted := make([][2]int, 0, len(spans))
	for _, sp := range spans {
		s, e := sp[0], sp[1]
		if e <= windowStart {
			continue
		}
		if s < windowStart {
			s = windowStart
		}
		shifted = append(shifted, [2]int{s - windowStart, e - windowStart})
	}

	styled := styleByteRanges(rest, shifted, base, match)
	if leftEllipsis {
		styled = dim.Render("…") + styled
	}
	return padLine(ansi.Truncate(styled, width, "…"), width)
}
