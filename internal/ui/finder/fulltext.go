package finder

import (
	"context"
	"strconv"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/mathieucroset/notty/internal/search"
)

const (
	// debounceDelay is how long a keystroke waits before the full-text
	// search actually runs (spec §8).
	debounceDelay = 50 * time.Millisecond
	// fullTextLimit caps the number of hits a search returns.
	fullTextLimit = 200
)

// debounceMsg fires debounceDelay after a keystroke changed the query. seq
// identifies which keystroke scheduled it, so a tick superseded by a later
// keystroke is a no-op.
type debounceMsg struct{ seq int }

// ftResultMsg carries a full-text search's results, tagged with the seq of
// the query that produced them, so a stale result (from a superseded query)
// is discarded on arrival too.
type ftResultMsg struct {
	seq  int
	hits []search.Hit
}

// queryChangedFullText schedules a debounced search for the current query.
// An empty query clears the results immediately without debouncing.
func (m Model) queryChangedFullText() (Model, tea.Cmd) {
	m.seq++
	seq := m.seq
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	m.cursor, m.offset = 0, 0
	if m.isEmptyQuery() {
		m.hits = nil
		m.searching = false
		return m, nil
	}
	m.searching = true
	return m, tea.Tick(debounceDelay, func(time.Time) tea.Msg {
		return debounceMsg{seq: seq}
	})
}

// handleDebounce runs the actual search once the debounce fires, unless a
// newer keystroke has since superseded it.
func (m Model) handleDebounce(msg debounceMsg) (Model, tea.Cmd) {
	if msg.seq != m.seq {
		return m, nil // superseded by a later keystroke
	}
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	q := search.ParseQuery(m.query)
	notes := m.notes
	seq := msg.seq
	return m, func() tea.Msg {
		defer cancel() // release ctx's resources once the search completes on its own
		hits := search.FullText(ctx, q, notes, fullTextLimit)
		return ftResultMsg{seq: seq, hits: hits}
	}
}

// handleResult applies a search result, discarding it if it is stale.
func (m Model) handleResult(msg ftResultMsg) (Model, tea.Cmd) {
	if msg.seq != m.seq {
		return m, nil // stale: a newer query is in flight or already done
	}
	m.searching = false
	m.hits = msg.hits
	m.cursor, m.offset = 0, 0
	return m, nil
}

func (m Model) currentHit() (search.Hit, bool) {
	if m.cursor < 0 || m.cursor >= len(m.hits) {
		return search.Hit{}, false
	}
	return m.hits[m.cursor], true
}

// fullTextStatus is the list pane's status text (spec §8): the query syntax
// hint for an empty query, "searching…" while a debounced search is in
// flight, "no results", or a count.
func (m Model) fullTextStatus() string {
	if m.isEmptyQuery() {
		return `"phrase"  #tag  in:folder`
	}
	if m.searching {
		return "searching…"
	}
	if len(m.hits) == 0 {
		return "no results"
	}
	return resultCount(len(m.hits))
}

// hitItemLines renders hit i's three lines (spec §8): the dim "path:line"
// location, the matching line text with its matches highlighted (truncated
// around the first match when the line is too long for width), and one line
// of dim context.
func (m Model) hitItemLines(i, width int, selected bool) []string {
	h := m.hits[i]
	base := m.bgStyle(lipgloss.NewStyle(), selected)
	match := m.bgStyle(m.styles.Match, selected)
	dim := m.bgStyle(m.styles.Muted, selected)

	loc := " " + dim.Render(h.Path+":"+strconv.Itoa(h.Line+1))
	text := " " + highlightAround(h.Text, h.Matches, max(width-1, 0), base, match, dim)
	context := " " + highlightAround(h.Context, nil, max(width-1, 0), dim, dim, dim)
	return []string{padLine(loc, width), padLine(text, width), padLine(context, width)}
}
