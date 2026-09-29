package finder

import (
	"strconv"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/mathieucroset/notty/internal/search"
)

// recomputeFuzzy re-ranks the fuzzy results for the current query
// (synchronously: spec §8 expects this to be fast enough for thousands of
// notes) and resets the selection to the top result.
func (m *Model) recomputeFuzzy() {
	m.fuzzy = search.Fuzzy(m.query, m.notes, m.recents)
	m.cursor, m.offset = 0, 0
}

func (m Model) currentFuzzy() (search.FuzzyResult, bool) {
	if m.cursor < 0 || m.cursor >= len(m.fuzzy) {
		return search.FuzzyResult{}, false
	}
	return m.fuzzy[m.cursor], true
}

// fuzzyStatus is the list pane's status text: "Recent" for an empty query
// (spec §8), otherwise a result count.
func (m Model) fuzzyStatus() string {
	if m.isEmptyQuery() {
		return "Recent"
	}
	return strconv.Itoa(len(m.fuzzy)) + " results"
}

// fuzzyItemLines renders result i's two lines: the title (with matched
// runes highlighted) and the dim path underneath (with its own matches
// highlighted).
func (m Model) fuzzyItemLines(i, width int, selected bool) (string, string) {
	r := m.fuzzy[i]
	base := m.bgStyle(lipgloss.NewStyle(), selected)
	match := m.bgStyle(m.styles.Match, selected)
	dim := m.bgStyle(m.styles.Muted, selected)

	title := r.Title
	if title == "" {
		title = "(untitled)"
	}
	line1 := " " + styleRuneIndexes(title, r.TitleMatches, base, match)
	line2 := "  " + styleRuneIndexes(r.Path, r.PathMatches, dim, match)
	return padLine(ansi.Truncate(line1, width, "…"), width), padLine(ansi.Truncate(line2, width, "…"), width)
}
