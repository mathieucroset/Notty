package textutil

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestWrapHardBreaksOverlongWord(t *testing.T) {
	word := strings.Repeat("x", 60)
	lines := Wrap(word, 20)
	if len(lines) != 3 {
		t.Fatalf("expected a 60-char unbreakable token at width 20 to hard-break into 3 lines, got %d: %q", len(lines), lines)
	}
	for i, l := range lines {
		if w := ansi.StringWidth(l); w != 20 {
			t.Fatalf("line %d: expected width 20, got %d (%q)", i, w, l)
		}
	}
	if strings.Join(lines, "") != word {
		t.Fatalf("expected no characters lost, got %q", strings.Join(lines, ""))
	}
}

func TestWrapPreservesANSI(t *testing.T) {
	// Wrap must not corrupt or drop escape sequences present in the input,
	// and must measure width by visible columns, not raw bytes, so styled
	// text still wraps at the right place.
	styled := lipgloss.NewStyle().Foreground(lipgloss.Color("#ff0000")).Render(strings.Repeat("y", 30))
	lines := Wrap(styled, 10)
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(lines))
	}
	for i, l := range lines {
		if w := ansi.StringWidth(l); w > 10 {
			t.Fatalf("line %d: expected visible width <= 10, got %d (%q)", i, w, l)
		}
	}
	joined := strings.Join(lines, "")
	if plain := ansi.Strip(joined); plain != strings.Repeat("y", 30) {
		t.Fatalf("expected the plain text to survive wrapping intact, got %q", plain)
	}
	if !strings.Contains(joined, "\x1b[") {
		t.Fatal("expected the original styling to still be present somewhere in the output")
	}
}

func TestWrapNormalWords(t *testing.T) {
	lines := Wrap("the quick brown fox jumps", 10)
	for _, l := range lines {
		if w := ansi.StringWidth(l); w > 10 {
			t.Fatalf("expected no line wider than 10, got %d: %q", w, l)
		}
	}
	if strings.ReplaceAll(strings.Join(lines, " "), "  ", " ") == "" {
		t.Fatal("expected non-empty wrapped output")
	}
}

func TestWrapNonPositiveWidth(t *testing.T) {
	lines := Wrap("hello", 0)
	if len(lines) != 1 || lines[0] != "hello" {
		t.Fatalf("expected a non-positive width to leave the string untouched, got %v", lines)
	}
}

func TestPadLineWideGraphemeBoundary(t *testing.T) {
	got := PadLine("abcd中efgh", 5)
	if w := ansi.StringWidth(got); w != 5 {
		t.Fatalf("expected width exactly 5, got %d (%q)", w, got)
	}
	if !strings.HasPrefix(got, "abcd") {
		t.Fatalf("expected the ascii prefix to survive, got %q", got)
	}
	if strings.Contains(got, "中") {
		t.Fatalf("expected the straddling wide grapheme to be dropped rather than split, got %q", got)
	}
}

func TestPadLineShortStringIsPadded(t *testing.T) {
	got := PadLine("hi", 5)
	if got != "hi   " {
		t.Fatalf("expected \"hi   \", got %q", got)
	}
}

func TestPadLineExactWidthUnchanged(t *testing.T) {
	got := PadLine("hello", 5)
	if got != "hello" {
		t.Fatalf("expected \"hello\" unchanged, got %q", got)
	}
}

func TestPadLineNonPositiveWidth(t *testing.T) {
	if got := PadLine("hello", 0); got != "" {
		t.Fatalf("expected empty string for width 0, got %q", got)
	}
}

func TestFitBlockExactDimensions(t *testing.T) {
	lines := []string{"one", "two", "three", "four"}
	out := FitBlock(lines, 6, 2)
	if len(out) != 2 {
		t.Fatalf("expected 2 lines (truncated), got %d", len(out))
	}
	for i, l := range out {
		if w := ansi.StringWidth(l); w != 6 {
			t.Fatalf("line %d: expected width 6, got %d (%q)", i, w, l)
		}
	}

	out = FitBlock([]string{"only"}, 6, 4)
	if len(out) != 4 {
		t.Fatalf("expected 4 lines (padded), got %d", len(out))
	}
	for i, l := range out {
		if w := ansi.StringWidth(l); w != 6 {
			t.Fatalf("line %d: expected width 6, got %d (%q)", i, w, l)
		}
	}
}
