// Package finder implements Notty's finder overlay (plan Task 27): one
// component, two modes. The ctrl+p fuzzy finder jumps to a note by path or
// title (an empty query shows recently opened notes); the ctrl+f full-text
// search shows matching lines with highlighted snippets. Both share a
// rounded overlay box with a results list on the left and a Glamour-rendered
// preview of the selected note on the right (spec §4.2, §4.4, §8).
//
// The component never touches the vault or the index directly: New takes a
// snapshot of the notes to search, and choosing a result emits
// msgs.OpenNoteMsg for the caller to act on.
package finder

import (
	"context"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/mathieucroset/notty/internal/index"
	"github.com/mathieucroset/notty/internal/search"
	"github.com/mathieucroset/notty/internal/ui/msgs"
	"github.com/mathieucroset/notty/internal/ui/theme"
)

// Mode selects the finder's behavior.
type Mode int

// Modes.
const (
	// Fuzzy jumps to a note by path or title.
	Fuzzy Mode = iota
	// FullText searches note content for matching lines.
	FullText
)

// CloseMsg asks the caller to close the finder overlay (esc).
type CloseMsg struct{}

const (
	// minBoxWidth and minBoxHeight are the smallest overlay box SetSize will
	// produce, so the layout never has to render in negative space.
	minBoxWidth  = 40
	minBoxHeight = 10

	// listWidthPct is the share of the content width given to the results
	// list; the rest goes to the preview, minus the one-column separator.
	listWidthPct = 45

	// fixedRows is the number of content rows that are always drawn
	// regardless of body size: header, input, status, footer.
	fixedRows = 4
)

// Model is the finder overlay. Build it with New, size it with SetSize, and
// drive it with Update; View renders the overlay box exactly at its
// configured size.
type Model struct {
	mode    Mode
	notes   []*index.Note
	byPath  map[string]*index.Note
	recents []string

	styles  theme.Styles
	palette theme.Palette
	surface lipgloss.Style

	width, height int // the overlay box, in terminal cells

	query  string
	cursor int // selected item index
	offset int // first visible item index

	// Fuzzy mode state.
	fuzzy []search.FuzzyResult

	// Full-text mode state.
	hits      []search.Hit
	seq       int
	searching bool
	cancel    context.CancelFunc

	// Preview state: rendering happens off the View path (see preview.go).
	// previewCurrentKey is the excerpt the current selection wants; the
	// actual rendered lines, once ready, live in previewCache.
	previewCache      *lruCache
	previewCurrentKey previewKey
	previewSeq        int
}

// New returns a finder overlay for mode, searching notes. recents is the
// most-recently-opened note paths (most recent first), used by the fuzzy
// finder's empty-query list.
func New(mode Mode, notes []*index.Note, recents []string, styles theme.Styles, palette theme.Palette) Model {
	byPath := make(map[string]*index.Note, len(notes))
	for _, n := range notes {
		byPath[n.Path] = n
	}
	m := Model{
		mode:         mode,
		notes:        notes,
		byPath:       byPath,
		recents:      recents,
		styles:       styles,
		palette:      palette,
		surface:      lipgloss.NewStyle().Background(palette.Surface),
		previewCache: newLRUCache(previewCacheCapacity),
	}
	if mode == Fuzzy {
		m.recomputeFuzzy()
	}
	return m
}

// Init starts rendering the initially selected result's preview, returning
// the updated Model (which now expects that render) alongside the command
// that produces it. Like Update, it threads the Model through rather than
// mutating in place; the caller invokes it once, alongside the rest of the
// application's initial commands, after New and SetSize.
func (m Model) Init() (Model, tea.Cmd) {
	return m.checkPreview()
}

// SetSize sets the terminal size. The overlay box is 80% of the width and
// 70% of the height (at least minBoxWidth x minBoxHeight, and never larger
// than the terminal); the caller centers it over a dimmed background.
func (m Model) SetSize(termW, termH int) Model {
	m.width = boxDim(termW, 80, minBoxWidth)
	m.height = boxDim(termH, 70, minBoxHeight)
	return m
}

func boxDim(total, pct, min int) int {
	if total <= 0 {
		return 0
	}
	d := total * pct / 100
	if d < min {
		d = min
	}
	if d > total {
		d = total
	}
	return d
}

func emit(msg tea.Msg) tea.Cmd { return func() tea.Msg { return msg } }

// bodyHeight is the number of rows left for the list/preview body once the
// header, input, status and footer rows (and the box border) are removed.
func (m Model) bodyHeight() int {
	return max(m.height-2-fixedRows, 0)
}

// rowHeight is the number of visual lines each result row occupies: two for
// a fuzzy result (title, dim path), three for a full-text hit (path:line,
// matching text, dim context line — spec §8).
func (m Model) rowHeight() int {
	if m.mode == FullText {
		return 3
	}
	return 2
}

// listRows is the number of result rows the list pane can show at once.
func (m Model) listRows() int {
	return max(m.bodyHeight()/m.rowHeight(), 1)
}

// Update handles key presses, the full-text debounce tick and search
// results, and full-text preview renders. Unrecognized messages are
// ignored. After handling the message, it always re-checks whether the
// selection now points at a different preview (see checkPreview in
// preview.go) and, if so, folds in the command that (re)renders it — this
// centralizes preview triggering rather than repeating it at every call
// site that can change the selection.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		m, cmd = m.handleKey(msg)
	case debounceMsg:
		m, cmd = m.handleDebounce(msg)
	case ftResultMsg:
		m, cmd = m.handleResult(msg)
	case previewRenderedMsg:
		m, cmd = m.handlePreviewRendered(msg)
	default:
		return m, nil
	}
	m, previewCmd := m.checkPreview()
	return m, tea.Batch(cmd, previewCmd)
}

// cancelSearch cancels any in-flight full-text search context. It is
// called at both ends of an overlay session — esc (closing the overlay) and
// enter (opening the chosen note) — so an abandoned search never keeps
// running in the background.
func (m *Model) cancelSearch() {
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
}

func (m Model) handleKey(k tea.KeyPressMsg) (Model, tea.Cmd) {
	switch k.String() {
	case "esc":
		m.cancelSearch()
		return m, emit(CloseMsg{})
	case "enter":
		m.cancelSearch()
		return m, m.choose()
	case "up", "ctrl+k":
		m.move(-1)
		return m, nil
	case "down", "ctrl+j":
		m.move(1)
		return m, nil
	case "pgup":
		m.move(-m.pageItems())
		return m, nil
	case "pgdown":
		m.move(m.pageItems())
		return m, nil
	case "backspace":
		r := []rune(m.query)
		if len(r) == 0 {
			return m, nil
		}
		m.query = string(r[:len(r)-1])
		return m.queryChanged()
	default:
		if k.Text != "" {
			m.query += k.Text
			return m.queryChanged()
		}
	}
	return m, nil
}

// queryChanged recomputes the results after the query text changed: fuzzy
// results synchronously, full-text results after a debounce.
func (m Model) queryChanged() (Model, tea.Cmd) {
	if m.mode == Fuzzy {
		m.recomputeFuzzy()
		return m, nil
	}
	return m.queryChangedFullText()
}

// itemCount is the number of selectable rows for the current mode.
func (m Model) itemCount() int {
	if m.mode == Fuzzy {
		return len(m.fuzzy)
	}
	return len(m.hits)
}

func (m Model) pageItems() int {
	if n := m.listRows() / 2; n > 1 {
		return n
	}
	return 1
}

func (m *Model) move(dir int) {
	n := m.itemCount()
	if n == 0 {
		return
	}
	m.cursor += dir
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor > n-1 {
		m.cursor = n - 1
	}
	m.ensureVisible()
}

func (m *Model) ensureVisible() {
	rows := m.listRows()
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+rows {
		m.offset = m.cursor - rows + 1
	}
	if m.offset < 0 {
		m.offset = 0
	}
}

// choose builds the command for enter: opening the selected result, or nil
// when nothing is selected.
func (m Model) choose() tea.Cmd {
	if m.mode == Fuzzy {
		r, ok := m.currentFuzzy()
		if !ok {
			return nil
		}
		return emit(msgs.OpenNoteMsg{Path: r.Path, Line: -1})
	}
	h, ok := m.currentHit()
	if !ok {
		return nil
	}
	return emit(msgs.OpenNoteMsg{Path: h.Path, Line: h.Line})
}

// selectedNote looks up the note behind the current selection, for the
// preview pane.
func (m Model) selectedNote() (*index.Note, bool) {
	if m.mode == Fuzzy {
		r, ok := m.currentFuzzy()
		if !ok {
			return nil, false
		}
		n, ok := m.byPath[r.Path]
		return n, ok
	}
	h, ok := m.currentHit()
	if !ok {
		return nil, false
	}
	n, ok := m.byPath[h.Path]
	return n, ok
}

// bgStyle layers s over the box's surface background, and over the
// selection background as well when selected.
func (m Model) bgStyle(s lipgloss.Style, selected bool) lipgloss.Style {
	if selected {
		s = s.Inherit(m.styles.Selection)
	}
	return s.Inherit(m.surface)
}

// isEmptyQuery reports whether the query is blank (ignoring whitespace).
func (m Model) isEmptyQuery() bool { return strings.TrimSpace(m.query) == "" }
