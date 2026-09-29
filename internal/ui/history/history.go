// Package history implements Notty's full-screen History view: a note's git
// history, with the selected revision shown either rendered or as a diff
// against the current buffer, and a way to restore an old version (spec
// §4.4, §8 "History").
//
// The History view owns the whole screen; unlike the pane-embedded views, it
// draws its own header and footer. It never touches the vault or git
// itself: loading a revision and restoring it are both requested as
// messages for the app to perform.
package history

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/glamour/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/mathieucroset/notty/internal/gitsync"
	"github.com/mathieucroset/notty/internal/merge"
	"github.com/mathieucroset/notty/internal/ui/icons"
	"github.com/mathieucroset/notty/internal/ui/theme"
)

// listRatio is the fraction of the width given to the commit list; the rest
// goes to the rendered version or diff.
const listRatio = 2.0 / 5.0

const minListWidth = 20
const minContentWidth = 10

// footerHint is the footer's key hint line.
const footerHint = "tab rendered/diff · enter restore · esc close"

// mode is which way the right pane shows the selected revision.
type mode int

const (
	modeRendered mode = iota
	modeDiff
)

// contentCacheKey identifies one rendering of the right pane: the loaded
// revision, the view mode, the content column's width, and the palette in
// use. The expensive part of showing a revision (a Glamour render or a
// merge.Unified diff) is recomputed only when this key changes.
type contentCacheKey struct {
	rev     string
	mode    mode
	width   int
	palette string
}

// Model is the History view.
type Model struct {
	styles  theme.Styles
	palette theme.Palette

	notePath       string
	currentContent string

	entries  []gitsync.LogEntry
	selRev   string // Rev of the explicitly selected entry, "" if none
	pendingG bool   // a "g" was just pressed, waiting for a second ("gg")

	loadedRev     string // Rev the loaded version belongs to
	loadedContent string

	mode   mode
	scroll int

	width, height int

	// contentCache holds the last computed right-pane content (unscrolled),
	// refreshed only by refreshContent (called from SetVersion, SetSize, and
	// the tab key). rightContent, maxScroll, and scrolledContent read it
	// instead of recomputing on every keystroke or frame.
	contentCache      string
	contentCacheKey   contentCacheKey
	contentCacheValid bool

	// onRender, when set, is called each time refreshContent actually
	// recomputes the right-pane content (a cache miss). It exists so tests
	// can prove the cache is doing its job; production code leaves it nil.
	onRender func()
}

// New returns a History view for notePath, whose current (possibly unsaved)
// content is currentContent, styled with styles and palette.
func New(notePath, currentContent string, styles theme.Styles, palette theme.Palette) Model {
	return Model{
		styles:         styles,
		palette:        palette,
		notePath:       notePath,
		currentContent: currentContent,
	}
}

// SetEntries replaces the commit history (newest first). The current
// selection is kept by revision when still present; otherwise the newest
// entry is selected.
func (m Model) SetEntries(entries []gitsync.LogEntry) Model {
	m.entries = entries
	if _, ok := m.selected(); !ok {
		m.selRev = ""
		if len(entries) > 0 {
			m.selRev = entries[0].Rev
		}
	}
	return m
}

// SetVersion stores the loaded content for revision rev, so it can be
// rendered once it matches the current selection.
func (m Model) SetVersion(rev, content string) Model {
	m.loadedRev = rev
	m.loadedContent = content
	return m.refreshContent()
}

// SetSize sets the view's size in columns and rows. It is the whole screen:
// the History view draws its own header and footer.
func (m Model) SetSize(w, h int) Model {
	m.width, m.height = w, h
	return m.refreshContent()
}

func (m Model) selected() (gitsync.LogEntry, bool) {
	for _, e := range m.entries {
		if e.Rev == m.selRev {
			return e, true
		}
	}
	return gitsync.LogEntry{}, false
}

func (m Model) selectedIndex() int {
	for i, e := range m.entries {
		if e.Rev == m.selRev {
			return i
		}
	}
	return -1
}

// versionLoaded reports whether the loaded content is for the current
// selection.
func (m Model) versionLoaded() bool {
	e, ok := m.selected()
	return ok && m.loadedRev != "" && m.loadedRev == e.Rev
}

// RestoreVersionMsg asks the app to restore Content (loaded from Rev) into
// Path, as a new edit.
type RestoreVersionMsg struct{ Path, Rev, Content string }

// SelectionChangedMsg reports that the selected revision changed, so the
// app can load it.
type SelectionChangedMsg struct{ Rev string }

// CloseMsg asks the app to close the History view.
type CloseMsg struct{}

func emit(msg tea.Msg) tea.Cmd {
	return func() tea.Msg { return msg }
}

// Update handles a key press.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	s := k.String()
	// "gg" is a two-key chord, like the preview's top/bottom keys (spec
	// §4.4). Any key other than a second "g" cancels a pending one.
	wasPendingG := m.pendingG
	m.pendingG = false

	switch s {
	case "j", "down":
		return m.moveTo(m.selectedIndex() + 1)
	case "k", "up":
		return m.moveTo(m.selectedIndex() - 1)
	case "g":
		if wasPendingG {
			return m.moveTo(0)
		}
		m.pendingG = true
	case "G":
		return m.moveTo(len(m.entries) - 1)
	case "pgdown", "ctrl+d":
		m.scroll = m.clampScroll(m.scroll + m.pageStep())
	case "pgup", "ctrl+u":
		m.scroll = m.clampScroll(m.scroll - m.pageStep())
	case "tab":
		if m.mode == modeRendered {
			m.mode = modeDiff
		} else {
			m.mode = modeRendered
		}
		m.scroll = 0
		m = m.refreshContent()
	case "enter":
		if m.versionLoaded() {
			return m, emit(RestoreVersionMsg{Path: m.notePath, Rev: m.loadedRev, Content: m.loadedContent})
		}
	case "esc", "q":
		return m, emit(CloseMsg{})
	}
	return m, nil
}

// moveTo selects entry idx, clamped to the list bounds. It emits
// SelectionChangedMsg and resets the scroll only when the selection
// actually changes.
func (m Model) moveTo(idx int) (Model, tea.Cmd) {
	if len(m.entries) == 0 {
		return m, nil
	}
	idx = min(max(idx, 0), len(m.entries)-1)
	if m.entries[idx].Rev == m.selRev {
		return m, nil
	}
	m.selRev = m.entries[idx].Rev
	m.scroll = 0
	return m, emit(SelectionChangedMsg{Rev: m.selRev})
}

// pageStep is how many lines pgup/pgdn/ctrl+u/ctrl+d scroll by.
func (m Model) pageStep() int {
	if h := m.contentHeight(); h > 1 {
		return h
	}
	return 1
}

func (m Model) contentHeight() int {
	_, _, bodyH := m.layout()
	return bodyH
}

func (m Model) clampScroll(s int) int {
	hi := m.maxScroll()
	if s < 0 {
		return 0
	}
	if s > hi {
		return hi
	}
	return s
}

func (m Model) maxScroll() int {
	lines := len(strings.Split(m.rightContent(), "\n"))
	bodyH := m.contentHeight()
	if lines <= bodyH {
		return 0
	}
	return lines - bodyH
}

// layout returns the header height (0 or 1), footer height (0 or 1), and
// the body height, which always sum to m.height.
func (m Model) layout() (headerH, footerH, bodyH int) {
	if m.height <= 0 {
		return 0, 0, 0
	}
	headerH = 1
	footerH = 0
	if m.height >= 2 {
		footerH = 1
	}
	bodyH = max(m.height-headerH-footerH, 0)
	return
}

// View renders the History view at exactly the configured width and height.
func (m Model) View() string {
	if m.width <= 0 || m.height <= 0 {
		return ""
	}
	headerH, footerH, bodyH := m.layout()
	var lines []string
	if headerH > 0 {
		title := fmt.Sprintf("History · %s", m.notePath)
		lines = append(lines, padLine(m.styles.DialogTitle.Render(title), m.width))
	}
	if bodyH > 0 {
		lines = append(lines, m.renderBody(bodyH)...)
	}
	if footerH > 0 {
		lines = append(lines, padLine(m.styles.Muted.Render(footerHint), m.width))
	}
	return strings.Join(lines, "\n")
}

func (m Model) renderBody(bodyH int) []string {
	if len(m.entries) == 0 {
		return fitBlock(centered(m.styles.Muted.Render("No history yet"), m.width, bodyH), m.width, bodyH)
	}

	listW, sepW, contentW := split(m.width)
	listLines := fitBlock(m.renderList(listW, bodyH), listW, bodyH)
	if contentW == 0 {
		return listLines
	}
	contentLines := fitBlock(m.scrolledContent(bodyH), contentW, bodyH)

	sep := m.styles.Muted.Render("│")
	rows := make([]string, bodyH)
	for i := 0; i < bodyH; i++ {
		row := listLines[i]
		if sepW > 0 {
			row += sep
		}
		row += contentLines[i]
		rows[i] = row
	}
	return rows
}

// split divides width into the list, separator, and content column widths.
// The content column is dropped when there isn't enough room for both.
func split(width int) (listW, sepW, contentW int) {
	listW = int(float64(width) * listRatio)
	if listW < 1 {
		listW = width
	}
	if width-listW-1 < minContentWidth || listW < minListWidth {
		return width, 0, 0
	}
	return listW, 1, width - listW - 1
}

func (m Model) renderList(w, h int) string {
	visible := max(h, 1)
	idx := max(m.selectedIndex(), 0)
	start := 0
	if idx >= visible {
		start = idx - visible + 1
	}
	if start+visible > len(m.entries) {
		start = max(len(m.entries)-visible, 0)
	}
	end := min(start+visible, len(m.entries))

	overlayBG := lipgloss.NewStyle().Background(m.palette.Overlay)
	on := func(s lipgloss.Style, selected bool) lipgloss.Style {
		if selected {
			return s.Inherit(overlayBG)
		}
		return s
	}

	var lines []string
	for i := start; i < end; i++ {
		e := m.entries[i]
		selected := e.Rev == m.selRev

		textStyle := on(m.styles.StatusText, selected)
		mutedStyle := on(m.styles.Muted, selected)
		addStyle := on(m.styles.Success, selected)
		delStyle := on(m.styles.Error, selected)
		gap := on(lipgloss.NewStyle(), selected)

		row := textStyle.Render(e.Date.Format("2006-01-02 15:04"))
		if e.Host != "" {
			row += gap.Render("  ") + mutedStyle.Render(e.Host)
		}
		row += gap.Render("  ") + addStyle.Render(fmt.Sprintf("+%d", e.Added)) +
			gap.Render(" ") + delStyle.Render(fmt.Sprintf("−%d", e.Deleted))
		lines = append(lines, padRow(row, w, gap))
	}
	return strings.Join(lines, "\n")
}

// scrolledContent returns the right pane's content, scrolled by m.scroll and
// clipped to bodyH lines.
func (m Model) scrolledContent(bodyH int) string {
	full := m.rightContent()
	lines := strings.Split(full, "\n")
	s := m.clampScroll(m.scroll)
	if s >= len(lines) {
		return ""
	}
	end := min(s+bodyH, len(lines))
	return strings.Join(lines[s:end], "\n")
}

// rightContent returns the selected revision's unscrolled content: either
// rendered markdown or a diff against the current content, depending on
// m.mode. It reports a loading placeholder while the version hasn't arrived
// yet, and otherwise always reads contentCache rather than recomputing —
// the cache is refreshed only by refreshContent.
func (m Model) rightContent() string {
	if _, ok := m.selected(); !ok {
		return ""
	}
	if !m.versionLoaded() {
		return m.styles.Muted.Render("loading…")
	}
	return m.contentCache
}

// refreshContent recomputes the right pane's content when its cache key —
// the loaded revision, the view mode, the content width, and the palette —
// has changed. It is called explicitly from SetVersion, SetSize, and the
// tab key; View, scrolling, and moving the selection never call it, so
// neither Glamour nor the differ runs on every frame or keystroke.
func (m Model) refreshContent() Model {
	key := contentCacheKey{rev: m.loadedRev, mode: m.mode, width: m.contentWidth(), palette: m.palette.Name}
	if m.contentCacheValid && m.contentCacheKey == key {
		return m
	}
	if m.onRender != nil {
		m.onRender()
	}
	m.contentCacheKey = key
	m.contentCache = m.computeContent()
	m.contentCacheValid = true
	return m
}

// computeContent is the expensive part of rightContent: it runs Glamour or
// the differ. Call it only through refreshContent, which caches the result.
func (m Model) computeContent() string {
	if m.loadedRev == "" {
		return ""
	}
	if m.mode == modeDiff {
		return m.renderDiff()
	}
	out, err := renderMarkdown(m.loadedContent, m.palette, m.styles.Icons, m.contentWidth())
	if err != nil {
		return m.loadedContent
	}
	return out
}

func (m Model) contentWidth() int {
	_, _, contentW := split(m.width)
	if contentW == 0 {
		return max(m.width, 1)
	}
	return contentW
}

func (m Model) renderDiff() string {
	a := merge.SplitLines(m.loadedContent)
	b := merge.SplitLines(m.currentContent)
	diff := merge.Unified(a, b, 3)
	if diff == nil {
		return m.styles.Muted.Render("No changes")
	}
	var lines []string
	for _, dl := range diff {
		switch {
		case dl.Kind == merge.Equal && dl.ALine == 0 && dl.BLine == 0:
			lines = append(lines, m.styles.Muted.Render("  "+dl.Text))
		case dl.Kind == merge.Insert:
			lines = append(lines, m.styles.Success.Render("+ "+dl.Text))
		case dl.Kind == merge.Delete:
			lines = append(lines, m.styles.Error.Render("- "+dl.Text))
		default: // unchanged context line
			lines = append(lines, m.styles.Muted.Render("  "+dl.Text))
		}
	}
	return strings.Join(lines, "\n")
}

// renderMarkdown renders content as Glamour-styled markdown at width w using
// p's tokens.
func renderMarkdown(content string, p theme.Palette, set icons.Set, w int) (string, error) {
	if w < 1 {
		w = 1
	}
	r, err := glamour.NewTermRenderer(
		glamour.WithStyles(theme.GlamourStyle(p, set)),
		glamour.WithWordWrap(w),
	)
	if err != nil {
		return "", err
	}
	out, err := r.Render(content)
	if err != nil {
		return "", err
	}
	return strings.TrimRight(out, "\n"), nil
}

// padRow pads s to exactly width w using padStyle for the added spaces, so a
// selected row's highlight covers the full row.
func padRow(s string, w int, padStyle lipgloss.Style) string {
	cur := ansi.StringWidth(s)
	if cur >= w {
		return ansi.Truncate(s, w, "…")
	}
	return s + padStyle.Render(strings.Repeat(" ", w-cur))
}

// fitBlock splits content into exactly h lines, each padded or truncated to
// exactly w columns.
func fitBlock(content string, w, h int) []string {
	lines := strings.Split(content, "\n")
	out := make([]string, h)
	for i := range out {
		if i < len(lines) {
			out[i] = padLine(lines[i], w)
		} else {
			out[i] = padLine("", w)
		}
	}
	return out
}

// padLine pads or truncates s to exactly width w.
func padLine(s string, w int) string {
	if w <= 0 {
		return ""
	}
	sw := ansi.StringWidth(s)
	switch {
	case sw > w:
		return ansi.Truncate(s, w, "…")
	case sw < w:
		return s + strings.Repeat(" ", w-sw)
	default:
		return s
	}
}

// centered places msg in the middle of a width×height block.
func centered(msg string, width, height int) string {
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, msg)
}
