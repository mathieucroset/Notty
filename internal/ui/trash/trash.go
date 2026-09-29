// Package trash implements Notty's Trash view: it lists deleted notes and
// folders newest first, previews the selected item, and lets the user
// restore it, delete it forever, or empty the trash (spec §4.4, §8
// "Trash").
//
// The Trash view renders only its content; the app draws the pane border
// around it. It never touches the vault itself: every action is emitted as
// a message for the app to perform (and, for destructive actions, confirm)
// later.
package trash

import (
	"fmt"
	"path"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/glamour/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/mathieucroset/notty/internal/ui/icons"
	"github.com/mathieucroset/notty/internal/ui/msgs"
	"github.com/mathieucroset/notty/internal/ui/theme"
	"github.com/mathieucroset/notty/internal/vault"
)

// listRatio is the fraction of the width given to the item list; the rest
// goes to the preview (spec: "list left (~40%)").
const listRatio = 2.0 / 5.0

// minListWidth and minPreviewWidth are the smallest widths at which the
// list and preview are still worth showing side by side.
const minListWidth = 12
const minPreviewWidth = 8

// rowsPerItem is the number of terminal rows each list entry occupies: the
// name, then the folder and relative-time line.
const rowsPerItem = 2

// Model is the Trash view.
type Model struct {
	styles  theme.Styles
	palette theme.Palette

	items []vault.TrashItem
	now   time.Time

	selID string // ID of the explicitly selected item, "" if none

	previewID      string // ID the preview was loaded for
	previewContent string

	width, height int

	// previewCache holds the last Glamour render of the preview, refreshed
	// only by refreshPreview (called from SetItems, SetPreview, SetSize, and
	// move). View reads it instead of recomputing on every keystroke or
	// frame.
	previewCache      string
	previewCacheKey   previewCacheKey
	previewCacheValid bool

	// onRender, when set, is called each time refreshPreview actually
	// re-renders the preview (a cache miss). It exists so tests can prove
	// the cache is doing its job; production code leaves it nil.
	onRender func()
}

// previewCacheKey identifies one Glamour render of the trash preview: the
// previewed item's ID, the preview column's width, and the palette in use.
type previewCacheKey struct {
	id      string
	width   int
	palette string
}

// New returns an empty Trash view styled with styles and palette. palette
// supplies the raw color tokens needed for row highlighting and the
// Glamour preview, which styles alone cannot express.
func New(styles theme.Styles, palette theme.Palette) Model {
	return Model{styles: styles, palette: palette}
}

// SetTheme re-themes the view (a live theme preview, say), re-rendering
// the preview in the new palette.
func (m Model) SetTheme(styles theme.Styles, palette theme.Palette) Model {
	m.styles, m.palette = styles, palette
	return m.refreshPreview()
}

// SetItems replaces the trash listing (newest first) and the reference time
// used for relative timestamps. The current selection is kept by ID when
// still present; otherwise the newest item is selected.
func (m Model) SetItems(items []vault.TrashItem, now time.Time) Model {
	m.items = items
	m.now = now
	if _, ok := m.findSelected(); !ok {
		m.selID = ""
		if len(items) > 0 {
			m.selID = items[0].ID
		}
	}
	return m.refreshPreview()
}

// SetPreview stores the loaded content for the trash item id, so it can be
// rendered once it matches the current selection.
func (m Model) SetPreview(id, content string) Model {
	m.previewID = id
	m.previewContent = content
	return m.refreshPreview()
}

// Selected returns the currently selected item, or false if the trash is
// empty.
func (m Model) Selected() (vault.TrashItem, bool) {
	return m.findSelected()
}

func (m Model) findSelected() (vault.TrashItem, bool) {
	for _, it := range m.items {
		if it.ID == m.selID {
			return it, true
		}
	}
	return vault.TrashItem{}, false
}

func (m Model) selectedIndex() int {
	for i, it := range m.items {
		if it.ID == m.selID {
			return i
		}
	}
	return -1
}

// SetSize sets the view's content size in columns and rows.
func (m Model) SetSize(w, h int) Model {
	m.width, m.height = w, h
	return m.refreshPreview()
}

// Title returns the pane title, e.g. "Trash · 3 items".
func (m Model) Title() string {
	if len(m.items) == 1 {
		return "Trash · 1 item"
	}
	return fmt.Sprintf("Trash · %d items", len(m.items))
}

// RestoreMsg asks the app to restore Item to its original path.
type RestoreMsg struct{ Item vault.TrashItem }

// DeleteForeverMsg asks the app to permanently delete Item, after
// confirmation.
type DeleteForeverMsg struct{ Item vault.TrashItem }

// EmptyTrashMsg asks the app to empty the trash, after confirmation.
type EmptyTrashMsg struct{}

// SelectionChangedMsg reports that the selected trash item changed, so the
// app can load its preview.
type SelectionChangedMsg struct{ Item vault.TrashItem }

// BackMsg asks the app to leave the Trash view.
type BackMsg struct{}

func emit(msg tea.Msg) tea.Cmd {
	return func() tea.Msg { return msg }
}

// Update handles a key press. Every action is emitted as a message; the
// Trash view never touches the vault itself.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	switch k.String() {
	case "j", "down":
		return m.move(1)
	case "k", "up":
		return m.move(-1)
	case "enter":
		if it, ok := m.findSelected(); ok {
			return m, emit(RestoreMsg{Item: it})
		}
	case "D":
		if it, ok := m.findSelected(); ok {
			return m, emit(DeleteForeverMsg{Item: it})
		}
	case "E":
		return m, emit(EmptyTrashMsg{})
	case "tab":
		return m, emit(msgs.FocusSidebarMsg{})
	case "esc":
		return m, emit(BackMsg{})
	}
	return m, nil
}

// move shifts the selection by dir (-1 or 1), clamped to the list bounds. It
// emits SelectionChangedMsg only when the selection actually changes.
func (m Model) move(dir int) (Model, tea.Cmd) {
	if len(m.items) == 0 {
		return m, nil
	}
	idx := m.selectedIndex()
	if idx < 0 {
		idx = 0
	}
	next := min(max(idx+dir, 0), len(m.items)-1)
	if m.items[next].ID == m.selID {
		return m, nil
	}
	m.selID = m.items[next].ID
	m = m.refreshPreview()
	return m, emit(SelectionChangedMsg{Item: m.items[next]})
}

// refreshPreview recomputes the Glamour preview when its cache key — the
// previewed item's ID, the preview column's width, and the palette — has
// changed. It is called explicitly from SetItems, SetPreview, SetSize, and
// move; View never calls it, so scrolling through the list or re-rendering
// the screen never re-runs Glamour.
func (m Model) refreshPreview() Model {
	it, ok := m.findSelected()
	if !ok || m.previewID != it.ID {
		return m
	}
	_, _, previewW := split(m.width)
	if previewW <= 0 {
		return m
	}
	previewW = previewTextWidth(previewW)
	key := previewCacheKey{id: m.previewID, width: previewW, palette: m.palette.Name}
	if m.previewCacheValid && m.previewCacheKey == key {
		return m
	}
	if m.onRender != nil {
		m.onRender()
	}
	out, err := renderMarkdown(m.previewContent, m.palette, m.styles.Icons, previewW)
	if err != nil {
		out = m.previewContent
	}
	m.previewCacheKey = key
	m.previewCache = out
	m.previewCacheValid = true
	return m
}

// RelativeTime formats t relative to now (spec §8): "just now", "Nm ago",
// "Nh ago", "Nd ago" up to 29 days, and an absolute "2006-01-02" date
// beyond that.
func RelativeTime(t, now time.Time) string {
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d/time.Minute))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d/time.Hour))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d/(24*time.Hour)))
	default:
		return t.Format("2006-01-02")
	}
}

// View renders the Trash view at exactly the configured width and height.
func (m Model) View() string {
	if m.width <= 0 || m.height <= 0 {
		return ""
	}
	if len(m.items) == 0 {
		return centered(m.styles.Muted.Render("Trash is empty"), m.width, m.height)
	}

	listW, sepW, previewW := split(m.width)
	listLines := fitBlock(m.renderList(listW), listW, m.height)
	if previewW == 0 {
		return strings.Join(listLines, "\n")
	}
	textW := previewTextWidth(previewW)
	previewLines := fitBlock(m.renderPreview(textW), textW, m.height)
	padR := strings.Repeat(" ", max(previewW-textW-1, 0))

	sep := m.styles.Muted.Render("│")
	rows := make([]string, m.height)
	for i := 0; i < m.height; i++ {
		row := listLines[i]
		if sepW > 0 {
			row += sep
		}
		row += " " + previewLines[i] + padR
		rows[i] = row
	}
	return strings.Join(rows, "\n")
}

// previewTextWidth is the preview's text width in a column previewW wide:
// one column of padding on each side, like every pane.
func previewTextWidth(previewW int) int { return max(previewW-2, 1) }

// split divides width into the list, separator, and preview column widths.
// The preview column is dropped (previewW == 0, sepW == 0) when there is not
// enough room for both.
func split(width int) (listW, sepW, previewW int) {
	listW = int(float64(width) * listRatio)
	if listW < 1 {
		listW = width
	}
	if width-listW-1 < minPreviewWidth || listW < minListWidth {
		return width, 0, 0
	}
	return listW, 1, width - listW - 1
}

// renderList renders the trash list at width w, scrolled so the selection
// stays visible.
func (m Model) renderList(w int) string {
	visible := max(m.height/rowsPerItem, 1)
	idx := max(m.selectedIndex(), 0)
	start := 0
	if idx >= visible {
		start = idx - visible + 1
	}
	if start+visible > len(m.items) {
		start = max(len(m.items)-visible, 0)
	}
	end := min(start+visible, len(m.items))

	overlayBG := lipgloss.NewStyle().Background(m.palette.Overlay)
	on := func(s lipgloss.Style, selected bool) lipgloss.Style {
		if selected {
			return s.Inherit(overlayBG)
		}
		return s
	}

	var lines []string
	for i := start; i < end; i++ {
		it := m.items[i]
		selected := it.ID == m.selID

		nameStyle := m.styles.SidebarItem.Bold(true)
		if selected {
			nameStyle = m.styles.SidebarSelectedFocused
		}
		lines = append(lines, padRow(nameStyle.Render(it.Name), w, on(lipgloss.NewStyle(), selected)))

		folder := path.Dir(it.OriginalPath)
		if folder == "." {
			folder = "/"
		}
		meta := RelativeTime(it.DeletedAt, m.now)
		if it.Host != "" {
			meta += " · " + it.Host
		}
		folderStyle := on(m.styles.PaneTitle, selected)
		metaStyle := on(m.styles.Muted, selected)
		row := folderStyle.Render(folder) + on(lipgloss.NewStyle(), selected).Render("  ") + metaStyle.Render(meta)
		lines = append(lines, padRow(row, w, on(lipgloss.NewStyle(), selected)))
	}
	return strings.Join(lines, "\n")
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

// renderPreview returns the selected item's preview: a loading placeholder
// while its content hasn't arrived yet, and otherwise always previewCache
// rather than a fresh Glamour render — the cache is refreshed only by
// refreshPreview.
func (m Model) renderPreview(w int) string {
	it, ok := m.findSelected()
	if !ok {
		return ""
	}
	if m.previewID != it.ID {
		return m.styles.Muted.Render("loading…")
	}
	return m.previewCache
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
