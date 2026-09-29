// Package editor is Notty's note editor component (spec §5 "ui/editor"). It
// wraps a *buffer.Buffer, a vim.Editor (the vim Machine or the non-modal
// Plain editor) and an incremental mdstyle cache, and renders them with
// soft-wrap, live markdown styling, a terminal cursor, autosave, and
// read-only and locked modes.
package editor

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/mathieucroset/notty/internal/buffer"
	"github.com/mathieucroset/notty/internal/mdstyle"
	"github.com/mathieucroset/notty/internal/ui/theme"
	"github.com/mathieucroset/notty/internal/vim"
)

// Clipboard is the system clipboard (internal/clipboard implements it).
type Clipboard interface {
	ReadImage() ([]byte, error)
	ReadText() (string, error)
	WriteText(string) error
}

// Options configures a new editor.
type Options struct {
	Vim         bool // vim mode; false uses the non-modal Plain editor
	LineNumbers bool // relative line numbers
	AutosaveMS  int  // idle delay before autosave; <= 0 means 1000
	Styles      theme.Styles
	Palette     theme.Palette
	Clipboard   Clipboard // nil: OSC52 for copies, no clipboard reads
	VaultRoot   string    // absolute vault path, used to resolve image chips
}

// DefaultBanner is the read-only banner shown for a conflicted note.
const DefaultBanner = "This note has a sync conflict. Press c to resolve."

// Model is the editor component. Its setters return an updated copy; the
// buffer, the vim engine and the caches are shared between copies, as usual
// for Bubble Tea models.
type Model struct {
	opts Options
	sty  *styler
	aux  *aux
	lay  *layout

	path string
	buf  *buffer.Buffer
	ed   vim.Editor
	md   *mdstyle.Cache

	w, h int
	top  anchor

	focused  bool
	readOnly bool
	banner   string
	flashing bool
	flashID  int

	locked bool
	queue  []tea.Msg
}

// New returns an editor holding an empty, unnamed buffer.
func New(opts Options) Model {
	if opts.AutosaveMS <= 0 {
		opts.AutosaveMS = 1000
	}
	m := Model{
		opts: opts,
		sty:  newStyler(opts.Styles, opts.Palette),
		aux:  newAux(),
		lay:  &layout{width: 1},
	}
	m.ed = m.newEditor(opts.Vim)
	m.buf = buffer.New("")
	m.md = mdstyle.NewCache()
	m.md.Update([]string{""}, 0)
	return m
}

// newEditor returns a vim Machine (with the wrap navigator) or a Plain
// editor.
func (m Model) newEditor(vimMode bool) vim.Editor {
	if vimMode {
		mc := vim.New()
		mc.SetWrapNavigator(m.lay)
		return mc
	}
	return vim.NewPlain()
}

// Load replaces the buffer with a new one holding content (a new buffer per
// note, so undo history does not leak between notes), places the cursor and
// rebuilds the markdown styling. The read-only and locked states are kept
// (the app sets them per note); input queued while locked is discarded.
func (m Model) Load(path, content string, cursor buffer.Pos) Model {
	m.path = path
	m.queue = nil // queued input belongs to the previous note
	m.buf = buffer.New(content)
	m.buf.SetCursor(cursor)
	m.ed.Reset(m.buf)
	m.ed.SetReadOnly(m.readOnly)
	m.md = mdstyle.NewCache()
	m.md.Update(m.aux.linesOf(m.buf), 0)
	m.buf.ResetChanged()
	m.aux.forgetMissing()
	m.top = anchor{}
	return m.ensureVisible()
}

// Reload replaces the text after an external change, keeping the cursor on
// the same line when it still exists, and marks the buffer saved. The
// engine keeps its mode (keys queued during the merge window replay in the
// same mode) and the change is its own undo step.
func (m Model) Reload(content string) Model {
	cur := m.buf.Cursor()
	m.ed.Resync(m.buf, func() {
		m.buf.SetText(content)
		m.buf.SetCursor(cur)
	})
	m.buf.MarkSaved()
	m.syncStyle()
	m.aux.forgetMissing()
	return m.ensureVisible()
}

// syncStyle feeds buffer edits to the mdstyle cache.
func (m Model) syncStyle() {
	if first := m.buf.FirstChangedLine(); first >= 0 {
		m.md.Update(m.aux.linesOf(m.buf), first)
		m.buf.ResetChanged()
	}
}

// Path returns the vault-relative path of the loaded note.
func (m Model) Path() string { return m.path }

// Content returns the buffer text.
func (m Model) Content() string { return m.buf.String() }

// Cursor returns the cursor position.
func (m Model) Cursor() buffer.Pos { return m.buf.Cursor() }

// CursorLine returns the cursor's source line (for the preview's
// FollowLine).
func (m Model) CursorLine() int { return m.buf.Cursor().Line }

// Dirty reports unsaved changes.
func (m Model) Dirty() bool { return m.buf.Dirty() }

// Version returns the buffer version, to pass back to MarkSaved.
func (m Model) Version() uint64 { return m.buf.Version() }

// ModeName returns the mode for the status bar: "READ-ONLY" for a read-only
// note, otherwise the engine's mode (NORMAL, INSERT, VISUAL, V-LINE,
// COMMAND or PLAIN).
func (m Model) ModeName() string {
	if m.readOnly {
		return "READ-ONLY"
	}
	return m.ed.ModeName()
}

// CommandLine returns the ":" or "/" command line being typed, for the app
// to show in the status area, or "".
func (m Model) CommandLine() string {
	if mc, ok := m.ed.(*vim.Machine); ok {
		return mc.CommandLine()
	}
	return ""
}

// Snapshot returns what to save: the note path, the text and the version
// to pass back to MarkSaved once the write succeeded.
func (m Model) Snapshot() (path, content string, version uint64) {
	return m.path, m.buf.String(), m.buf.Version()
}

// MarkSaved marks the buffer saved if it still holds the note at path at
// version. A save that raced with typing, or that finished after another
// note was loaded, leaves the buffer dirty.
func (m Model) MarkSaved(path string, version uint64) Model {
	if m.path == path && m.buf.Version() == version {
		m.buf.MarkSaved()
	}
	return m
}

// SetSize sets the editor's size in cells.
func (m Model) SetSize(w, h int) Model {
	m.w, m.h = max(0, w), max(0, h)
	m.lay.width = m.wrapWidth()
	return m.ensureVisible()
}

// SetFocused sets whether the editor has keyboard focus (and shows a
// cursor).
func (m Model) SetFocused(f bool) Model {
	m.focused = f
	return m
}

// SetTheme restyles the editor.
func (m Model) SetTheme(styles theme.Styles, palette theme.Palette) Model {
	m.opts.Styles, m.opts.Palette = styles, palette
	m.sty = newStyler(styles, palette)
	return m
}

// SetVim switches between the vim Machine and the Plain editor.
func (m Model) SetVim(on bool) Model {
	if _, isVim := m.ed.(*vim.Machine); isVim == on {
		return m
	}
	m.opts.Vim = on
	m.ed = m.newEditor(on)
	m.ed.SetReadOnly(m.readOnly)
	m.ed.Reset(m.buf)
	return m
}

// SetLineNumbers shows or hides relative line numbers.
func (m Model) SetLineNumbers(on bool) Model {
	m.opts.LineNumbers = on
	m.lay.width = m.wrapWidth()
	return m.ensureVisible()
}

// gutterWidth is the width of the line-number column (or of the left
// padding when line numbers are off).
func (m Model) gutterWidth() int {
	if !m.opts.LineNumbers {
		return 1
	}
	return m.numberWidth() + 1
}

func (m Model) numberWidth() int {
	return max(2, len(strconv.Itoa(m.buf.LineCount())))
}

// textWidth is the width of the text area right of the gutter.
func (m Model) textWidth() int { return max(1, m.w-m.gutterWidth()) }

// wrapWidth is the soft-wrap width: the text area minus the reserved
// column for the end-of-row cursor.
func (m Model) wrapWidth() int { return max(1, m.textWidth()-1) }

// bannerRows is the number of rows taken by the read-only banner.
func (m Model) bannerRows() int {
	if m.readOnly && m.h > 1 {
		return 1
	}
	return 0
}

// textHeight is the number of rows showing text.
func (m Model) textHeight() int { return max(0, m.h-m.bannerRows()) }

// rawLine reports whether line i is shown as typed: the cursor line of an
// editable note keeps its markdown so the cursor maps 1:1.
func (m Model) rawLine(i int) bool {
	return !m.readOnly && i == m.buf.Cursor().Line
}

// lineUnits returns the display units of line i and its row starts.
func (m Model) lineUnits(i int) ([]unit, []int) {
	units := m.displayUnits(i, m.rawLine(i))
	return units, wrapGraphemes(graphemesOf(units), m.lay.width)
}

// rows returns the number of screen rows of line i.
func (m Model) rows(i int) int {
	_, starts := m.lineUnits(i)
	return len(starts)
}

// cursorAnchor returns the screen row of the cursor and its cell column.
func (m Model) cursorAnchor() (anchor, int) {
	p := m.buf.Cursor()
	gs := buffer.Graphemes(m.buf.Line(p.Line))
	starts := wrapGraphemes(gs, m.lay.width)
	return anchor{line: p.Line, row: rowOf(starts, p.Col)}, cellX(gs, starts, p.Col, m.lay.width)
}

// ensureVisible scrolls so the cursor keeps the scroll margin.
func (m Model) ensureVisible() Model {
	m.lay.width = m.wrapWidth() // the gutter grows with the line count
	if m.textHeight() <= 0 {
		return m
	}
	cur, _ := m.cursorAnchor()
	m.top = scroll(m.top, cur, m.textHeight(), m.buf.LineCount(), m.rows)
	return m
}

// CursorPosition returns the terminal cursor relative to the editor's
// top-left corner, or nil when the editor is unfocused, read-only, in
// command mode (the app draws the command line cursor) or scrolled away.
// The shape is a bar while typing (insert mode, Plain) and a block
// otherwise.
func (m Model) CursorPosition() *tea.Cursor {
	if !m.focused || m.readOnly || m.w <= 0 || m.textHeight() <= 0 {
		return nil
	}
	shape := tea.CursorBar
	if mc, ok := m.ed.(*vim.Machine); ok {
		switch mc.Mode() {
		case vim.Command:
			return nil
		case vim.Insert:
		default:
			shape = tea.CursorBlock
		}
	}
	cur, x := m.cursorAnchor()
	if cur.less(m.top) {
		return nil
	}
	y := distance(m.top, cur, m.textHeight(), m.rows)
	if y >= m.textHeight() {
		return nil
	}
	c := tea.NewCursor(m.gutterWidth()+min(x, m.textWidth()-1), y+m.bannerRows())
	c.Shape = shape
	c.Blink = true
	return c
}

// selectionIn returns the selected columns [from, to) of line i and whether
// the line break after it is selected.
func selectionIn(r buffer.Range, i, lineLen int) (from, to int, eol, ok bool) {
	if i < r.Start.Line || i > r.End.Line || (i == r.End.Line && r.End.Col == 0 && i != r.Start.Line) {
		return 0, 0, false, false
	}
	from, to = 0, lineLen
	if i == r.Start.Line {
		from = r.Start.Col
	}
	if i == r.End.Line {
		to = min(r.End.Col, lineLen)
	} else {
		eol = true
	}
	return from, to, eol, true
}

// View renders exactly h rows of exactly w cells.
func (m Model) View() string {
	if m.w <= 0 || m.h <= 0 {
		return strings.TrimSuffix(strings.Repeat("\n", max(0, m.h)), "\n")
	}
	out := make([]string, 0, m.h)
	if m.bannerRows() > 0 {
		out = append(out, m.bannerView())
	}
	sel, hasSel := m.ed.Selection(m.buf)
	if hasSel {
		sel = sel.Normalized()
	}
	cursorLine := m.buf.Cursor().Line
	gw, tw := m.gutterWidth(), m.textWidth()
	for a := m.top; len(out) < m.h && a.line < m.buf.LineCount(); a = (anchor{line: a.line + 1}) {
		units, starts := m.lineUnits(a.line)
		eol := false
		if hasSel {
			if from, to, e, ok := selectionIn(sel, a.line, m.buf.LineLen(a.line)); ok {
				eol = e
				for k := range units {
					if units[k].src >= from && units[k].src < to {
						units[k].st.sel = true
					}
				}
			}
		}
		for r := a.row; r < len(starts) && len(out) < m.h; r++ {
			text, width := m.renderRow(units[starts[r]:rowEnd(starts, r, len(units))], m.lay.width)
			if eol && r == len(starts)-1 && width < tw {
				text += m.sty.style(sty{sel: true}).Render(" ")
				width++
			}
			out = append(out, fit(m.gutter(a.line, r, cursorLine)+text, gw+width, m.w))
		}
	}
	blank := strings.Repeat(" ", m.w)
	for len(out) < m.h {
		out = append(out, blank)
	}
	return strings.Join(out, "\n")
}

// gutter renders the gutter for row r of line i: relative line numbers
// (the cursor line shows its absolute number in Accent) or padding.
func (m Model) gutter(i, r, cursorLine int) string {
	if !m.opts.LineNumbers {
		return " "
	}
	nw := m.numberWidth()
	if r > 0 {
		return strings.Repeat(" ", nw+1)
	}
	if i == cursorLine {
		return fg(m.sty.style(sty{}), m.opts.Palette.Accent).Render(padLeft(strconv.Itoa(i+1), nw)) + " "
	}
	n := i - cursorLine
	if n < 0 {
		n = -n
	}
	return fg(m.sty.style(sty{}), m.opts.Palette.Muted).Render(padLeft(strconv.Itoa(n), nw)) + " "
}

// fit truncates or pads row (of the given width in cells) to exactly w.
func fit(row string, width, w int) string {
	if width > w {
		row = ansi.Truncate(row, w, "")
		width = ansi.StringWidth(row)
	}
	return row + strings.Repeat(" ", max(0, w-width))
}

func padLeft(s string, w int) string {
	if len(s) >= w {
		return s
	}
	return strings.Repeat(" ", w-len(s)) + s
}

// SetReadOnly makes the note read-only (a conflicted note, spec §4.4) with a
// banner at the top; an empty banner uses DefaultBanner. Edits are blocked,
// `c` opens the resolver and autosave never writes.
func (m Model) SetReadOnly(ro bool, banner string) Model {
	if banner == "" {
		banner = DefaultBanner
	}
	m.readOnly, m.banner = ro, banner
	m.ed.SetReadOnly(ro)
	if !ro {
		m.flashing = false
	}
	return m.ensureVisible()
}

// bannerView renders the read-only banner row, reversed while flashing.
func (m Model) bannerView() string {
	text := " ⚠ " + m.banner + " "
	if ansi.StringWidth(text) > m.w {
		text = ansi.Truncate(text, m.w, "…")
	}
	text += strings.Repeat(" ", m.w-ansi.StringWidth(text))
	st := m.opts.Styles.Warning
	if m.flashing {
		st = st.Reverse(true)
	}
	return st.Render(text)
}
