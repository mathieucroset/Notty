package finder

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// TestStyleByteRangesUnicode checks that byte ranges are sliced correctly
// around multi-byte runes: the plain-text reconstruction must exactly equal
// the input, and each matched substring must appear wrapped in the match
// style's own rendering.
func TestStyleByteRangesUnicode(t *testing.T) {
	const text = "café ☕ time, naïve façade"
	base := lipgloss.NewStyle()
	match := lipgloss.NewStyle().Bold(true)

	// "café" (multi-byte é) and "☕" (a 3-byte emoji) and "naïve" (multi-byte
	// ï), as byte ranges.
	spans := [][2]int{
		{0, strings.Index(text, "café") + len("café")},
		{strings.Index(text, "☕"), strings.Index(text, "☕") + len("☕")},
		{strings.Index(text, "naïve"), strings.Index(text, "naïve") + len("naïve")},
	}

	got := styleByteRanges(text, spans, base, match)

	if stripped := ansi.Strip(got); stripped != text {
		t.Fatalf("styleByteRanges reconstructed text = %q, want %q", stripped, text)
	}
	for _, sp := range spans {
		want := match.Render(text[sp[0]:sp[1]])
		if !strings.Contains(got, want) {
			t.Errorf("styled output missing match-styled span %q\nfull output: %q", text[sp[0]:sp[1]], got)
		}
	}
}

func TestStyleRuneIndexesUnicode(t *testing.T) {
	const text = "héllo wörld"
	base := lipgloss.NewStyle()
	match := lipgloss.NewStyle().Bold(true)

	runes := []rune(text)
	// Highlight the 'é' (index 1) and the 'ö' (index 8).
	idx := []int{1, 8}

	got := styleRuneIndexes(text, idx, base, match)
	if stripped := ansi.Strip(got); stripped != text {
		t.Fatalf("styleRuneIndexes reconstructed text = %q, want %q", stripped, text)
	}
	for _, i := range idx {
		want := match.Render(string(runes[i]))
		if !strings.Contains(got, want) {
			t.Errorf("styled output missing match-styled rune %q at index %d\nfull output: %q", string(runes[i]), i, got)
		}
	}
}

// TestHighlightAroundKeepsFirstMatchVisible checks that a long line is
// truncated around the first match (not simply from the left), and that the
// match's highlight survives the truncation, on a line with multi-byte
// runes before the match.
func TestHighlightAroundKeepsFirstMatchVisible(t *testing.T) {
	prefix := strings.Repeat("café latté, ", 10) // long multi-byte prefix
	needle := "TARGET"
	suffix := strings.Repeat(" more text", 10)
	text := prefix + needle + suffix
	start := len(prefix)
	end := start + len(needle)
	spans := [][2]int{{start, end}}

	base := lipgloss.NewStyle()
	match := lipgloss.NewStyle().Bold(true)
	dim := lipgloss.NewStyle().Faint(true)

	const width = 40
	got := highlightAround(text, spans, width, base, match, dim)

	if w := ansi.StringWidth(got); w != width {
		t.Fatalf("highlightAround width = %d, want %d (line: %q)", w, width, got)
	}
	wantMatch := match.Render(needle)
	if !strings.Contains(got, wantMatch) {
		t.Errorf("truncated line lost the highlighted match; got %q", got)
	}
	if stripped := ansi.Strip(got); !strings.Contains(stripped, needle) {
		t.Errorf("truncated line does not contain the match text %q: %q", needle, stripped)
	}
}

func TestHighlightAroundFitsWithoutTruncation(t *testing.T) {
	text := "short line with a match"
	spans := [][2]int{{16, len("short line with a match")}}
	base := lipgloss.NewStyle()
	match := lipgloss.NewStyle().Bold(true)
	dim := lipgloss.NewStyle().Faint(true)

	got := highlightAround(text, spans, 80, base, match, dim)
	if w := ansi.StringWidth(got); w != 80 {
		t.Fatalf("width = %d, want 80", w)
	}
	if stripped := strings.TrimRight(ansi.Strip(got), " "); stripped != text {
		t.Fatalf("stripped = %q, want %q", stripped, text)
	}
}
