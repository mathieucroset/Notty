package finder

import (
	"container/list"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/glamour/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/mathieucroset/notty/internal/search"
	"github.com/mathieucroset/notty/internal/ui/icons"
	"github.com/mathieucroset/notty/internal/ui/theme"
)

const (
	// previewCacheCapacity is the number of rendered previews kept in the
	// LRU cache, so revisiting a recently viewed result redraws instantly.
	previewCacheCapacity = 32

	// previewLineBudget bounds how much of a note's source is ever handed to
	// Glamour for the preview, in units of the preview pane's height, so a
	// huge note never makes a render slow.
	previewLineBudget = 4
)

// previewKey identifies one bounded, rendered preview excerpt: a note's
// path, the preview pane's width, and the raw source line range the excerpt
// covers (windowStart, windowLen). The fuzzy finder always renders a
// top-anchored excerpt (windowStart 0); full-text search renders a window
// around the selected hit's line.
type previewKey struct {
	path        string
	width       int
	windowStart int
	windowLen   int
}

// previewRenderedMsg carries a bounded Glamour render, tagged with the seq
// of the request that produced it: a render superseded by a later selection
// change arrives with a stale seq and is discarded (spec: stale results
// discarded).
type previewRenderedMsg struct {
	seq   int
	key   previewKey
	lines []string
}

// checkPreview compares the current selection against the last preview
// request. When it changed, it updates the request and either finds the
// excerpt already cached (nothing more to do; View will show it) or starts
// a bounded Glamour render in a tea.Cmd, tagged with a fresh seq. This is
// the ONLY place Glamour is invoked: View never renders Markdown itself, so
// it can never block the UI on a slow render.
func (m Model) checkPreview() (Model, tea.Cmd) {
	note, ok := m.selectedNote()
	if !ok {
		m.previewCurrentKey = previewKey{}
		return m, nil
	}
	width := m.previewWidth()
	paneHeight := m.bodyHeight()
	if width <= 0 || paneHeight <= 0 {
		return m, nil
	}

	hitLine := -1
	if m.mode == FullText {
		if h, ok := m.currentHit(); ok {
			hitLine = h.Line
		}
	}
	excerpt, windowStart, windowLen := previewExcerpt(note.Content, paneHeight, hitLine)
	key := previewKey{path: note.Path, width: width, windowStart: windowStart, windowLen: windowLen}
	if key == m.previewCurrentKey {
		return m, nil // already showing (or already requested) this excerpt
	}
	m.previewCurrentKey = key

	if _, ok := m.previewCache.Get(key); ok {
		return m, nil // cached: View shows it immediately, no render needed
	}

	m.previewSeq++
	seq := m.previewSeq
	palette, set := m.palette, m.styles.Icons
	return m, func() tea.Msg {
		lines := renderMarkdown(excerpt, width, palette, set)
		return previewRenderedMsg{seq: seq, key: key, lines: lines}
	}
}

// handlePreviewRendered stores a completed render in the cache, unless it
// has been superseded by a later selection change.
func (m Model) handlePreviewRendered(msg previewRenderedMsg) (Model, tea.Cmd) {
	if msg.seq != m.previewSeq {
		return m, nil // stale: a newer selection is current
	}
	m.previewCache.Put(msg.key, msg.lines)
	return m, nil
}

// previewExcerpt returns the bounded slice of content's lines to render
// (joined back into a string), along with the raw line it starts at
// (windowStart) and its length in raw lines (windowLen). hitLine < 0 (the
// fuzzy finder) anchors the excerpt at the top; otherwise the excerpt is a
// window around hitLine (full-text search).
func previewExcerpt(content string, paneHeight, hitLine int) (excerpt string, windowStart, windowLen int) {
	lines := strings.Split(content, "\n")
	budget := max(paneHeight*previewLineBudget, 1)
	if hitLine < 0 {
		end := min(budget, len(lines))
		return strings.Join(lines[:end], "\n"), 0, end
	}
	half := budget / 2
	start := max(hitLine-half, 0)
	end := min(start+budget, len(lines))
	start = max(end-budget, 0)
	return strings.Join(lines[start:end], "\n"), start, end - start
}

// renderMarkdown renders content with Glamour at wordWrap width, styled from
// p, split into individually width-padded lines. Render errors fall back to
// a single placeholder line rather than failing the whole overlay.
func renderMarkdown(content string, width int, p theme.Palette, set icons.Set) []string {
	r, err := glamour.NewTermRenderer(
		glamour.WithStyles(theme.GlamourStyle(p, set)),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return []string{padLine("(preview unavailable)", width)}
	}
	out, err := r.Render(content)
	if err != nil {
		return []string{padLine("(preview unavailable)", width)}
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	for i, l := range lines {
		lines[i] = padLine(l, width)
	}
	return lines
}

// mapLineToRendered approximates which rendered preview line corresponds to
// rawLine (0-based, relative to the excerpt's own start), by position:
// Glamour reformats markdown, so there is no exact mapping between source
// and rendered lines, but scaling by each side's line count keeps the guess
// close. It is the fallback when findHitLine finds no textual match.
func mapLineToRendered(rawLine, totalRawLines, renderedLen int) int {
	if renderedLen <= 0 {
		return -1
	}
	if totalRawLines <= 1 {
		return 0
	}
	idx := rawLine * (renderedLen - 1) / (totalRawLines - 1)
	return max(0, min(idx, renderedLen-1))
}

// findHitLine locates hitText within lines (ANSI-stripped), preferring the
// occurrence nearest guess (the proportional estimate from
// mapLineToRendered), and falling back to guess itself when hitText does
// not appear verbatim in any rendered line (Glamour can reflow or style
// text so it no longer matches exactly).
func findHitLine(lines []string, hitText string, guess int) int {
	needle := strings.TrimSpace(hitText)
	if needle == "" {
		return guess
	}
	best, bestDist := -1, 0
	for i, l := range lines {
		if !strings.Contains(ansi.Strip(l), needle) {
			continue
		}
		d := i - guess
		if d < 0 {
			d = -d
		}
		if best == -1 || d < bestDist {
			best, bestDist = i, d
		}
	}
	if best >= 0 {
		return best
	}
	return guess
}

// previewMarkIndex is the rendered line to scroll to and mark for a
// full-text hit, within an excerpt cached under key.
func previewMarkIndex(full []string, key previewKey, h search.Hit) int {
	guess := mapLineToRendered(h.Line-key.windowStart, key.windowLen, len(full))
	if guess < 0 {
		return -1
	}
	return findHitLine(full, h.Text, guess)
}

// previewWidth is the preview pane's content width, given the box's current
// size: the pane less one column of padding on each side.
func (m Model) previewWidth() int {
	cw := max(m.width-2, 0)
	_, previewW := m.paneWidths(cw)
	return max(previewW-2, 0)
}

// previewPaneLines builds the preview pane's body from the cache: the
// selected note's rendered excerpt, scrolled (for full-text) so the hit
// line is visible and marked, or from the top (for fuzzy). It never renders
// Markdown itself; when the excerpt is not cached yet (the async render is
// still in flight), it shows a "loading…" placeholder instead.
func (m Model) previewPaneLines(width, height int) []string {
	blank := m.bgStyle(lipgloss.NewStyle(), false).Render(strings.Repeat(" ", max(width, 0)))
	if height <= 0 {
		return nil
	}
	if _, ok := m.selectedNote(); !ok || width <= 0 {
		out := make([]string, height)
		for i := range out {
			out[i] = blank
		}
		return out
	}

	full, cached := m.previewCache.Peek(m.previewCurrentKey)
	if !cached {
		return m.previewLoadingLines(width, height, blank)
	}

	markIdx, start := -1, 0
	if m.mode == FullText {
		if h, ok := m.currentHit(); ok {
			markIdx = previewMarkIndex(full, m.previewCurrentKey, h)
			start = markIdx - height/2
		}
	}
	if maxStart := max(len(full)-height, 0); start > maxStart {
		start = maxStart
	}
	start = max(start, 0)

	out := make([]string, height)
	for i := range out {
		idx := start + i
		if idx >= len(full) {
			out[i] = blank
			continue
		}
		line := full[idx]
		if idx == markIdx {
			line = m.styles.Selection.Render(line)
		}
		out[i] = line
	}
	return out
}

// previewLoadingLines is the placeholder shown while a preview render is in
// flight.
func (m Model) previewLoadingLines(width, height int, blank string) []string {
	out := make([]string, height)
	for i := range out {
		out[i] = blank
	}
	if height > 0 && width > 0 {
		out[0] = padLine(" "+m.bgStyle(m.styles.Muted, false).Render("loading…"), width)
	}
	return out
}

// lruEntry is one cached preview, tracked in lruCache's access-order list.
type lruEntry struct {
	key   previewKey
	value []string
}

// lruCache is a small fixed-capacity least-recently-used cache from
// previewKey to rendered lines.
type lruCache struct {
	capacity int
	order    *list.List // front = most recently used
	items    map[previewKey]*list.Element
}

func newLRUCache(capacity int) *lruCache {
	return &lruCache{capacity: capacity, order: list.New(), items: map[previewKey]*list.Element{}}
}

// Get returns key's cached value, promoting it to most-recently-used.
func (c *lruCache) Get(key previewKey) ([]string, bool) {
	el, ok := c.items[key]
	if !ok {
		return nil, false
	}
	c.order.MoveToFront(el)
	return el.Value.(*lruEntry).value, true
}

// Peek returns key's cached value without affecting recency, for read-only
// lookups (View must not have side effects beyond rendering).
func (c *lruCache) Peek(key previewKey) ([]string, bool) {
	el, ok := c.items[key]
	if !ok {
		return nil, false
	}
	return el.Value.(*lruEntry).value, true
}

// Put inserts or updates key's value as most-recently-used, evicting the
// least-recently-used entry once the cache is over capacity.
func (c *lruCache) Put(key previewKey, value []string) {
	if el, ok := c.items[key]; ok {
		el.Value.(*lruEntry).value = value
		c.order.MoveToFront(el)
		return
	}
	el := c.order.PushFront(&lruEntry{key: key, value: value})
	c.items[key] = el
	if c.capacity > 0 {
		for c.order.Len() > c.capacity {
			oldest := c.order.Back()
			if oldest == nil {
				break
			}
			c.order.Remove(oldest)
			delete(c.items, oldest.Value.(*lruEntry).key)
		}
	}
}

// Len reports the number of entries currently cached.
func (c *lruCache) Len() int { return c.order.Len() }
