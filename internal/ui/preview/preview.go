// Package preview is the markdown preview pane (spec §4.1, §6.1, §6.4). It
// renders the note buffer with Glamour in segments, shows images inline
// (Kitty unicode placeholders or half-blocks), and in the full preview view
// lets the user scroll, toggle tasks and open images in the image viewer.
package preview

import (
	"maps"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/mathieucroset/notty/internal/imgrender"
	"github.com/mathieucroset/notty/internal/ui/theme"
)

// Mode is how the preview is shown.
type Mode int

// Modes. The constants carry a Mode prefix because Split is also the
// segmenting function.
const (
	// ModeFull is the preview view: interactive, keys act.
	ModeFull Mode = iota
	// ModeSplit is the preview side of split view: passive, it follows the
	// editor's cursor line and ignores keys.
	ModeSplit
)

// DefaultDebounce is the delay between a content change and the re-render
// (spec §4.1).
const DefaultDebounce = 150 * time.Millisecond

// Model is the preview component.
type Model struct {
	styles    theme.Styles
	palette   theme.Palette
	caps      imgrender.Caps
	vaultRoot string

	width, height int
	mode          Mode
	overlay       bool
	// ready is set once the terminal is on the alternate screen, so Kitty
	// transmissions reach the right image store (kitty-spike notes).
	ready bool

	notePath string
	content  string
	// gen increases with every change that needs a re-render; renders
	// carry it and stale ones are dropped.
	gen int
	// needTick asks the next Update to schedule a render (SetSize cannot
	// return a command).
	needTick bool
	debounce time.Duration

	doc     *doc
	offset  int
	taskIdx int // highlighted task, -1 for none
	imgIdx  int // highlighted image, -1 for none
	pending string

	sh *shared
}

// New returns an empty preview. vaultRoot is the absolute vault directory
// image links resolve against.
func New(styles theme.Styles, palette theme.Palette, caps imgrender.Caps, vaultRoot string) Model {
	return Model{
		styles:    styles,
		palette:   palette,
		caps:      caps,
		vaultRoot: vaultRoot,
		debounce:  DefaultDebounce,
		taskIdx:   -1,
		imgIdx:    -1,
		sh: &shared{
			textCache: map[textKey][]string{},
			imgCache:  imgrender.NewCache(imgrender.DefaultCacheSize),
			sent:      map[uint32]bool{},
		},
	}
}

// SetSize sets the pane size. A change re-renders on the next Update (or
// Refresh); until then the previous render is cropped or padded.
func (m Model) SetSize(w, h int) Model {
	if w == m.width && h == m.height {
		return m
	}
	m.width, m.height = max(0, w), max(0, h)
	m.gen++
	m.needTick = true
	m.clampOffset()
	return m
}

// Refresh returns the render command a size change is waiting for, if any.
func (m Model) Refresh() (Model, tea.Cmd) {
	if !m.needTick {
		return m, nil
	}
	m.needTick = false
	return m, m.tick()
}

// SetMode switches between the interactive preview view and the passive
// split side.
func (m Model) SetMode(mode Mode) Model {
	m.mode = mode
	m.pending = ""
	return m
}

// Mode returns the current mode.
func (m Model) Mode() Mode { return m.mode }

// SetContent sets the buffer to preview. The render runs 150ms after the
// last call (debounced) in a command; results of older calls are dropped.
func (m Model) SetContent(notePath, content string) (Model, tea.Cmd) {
	if notePath != m.notePath {
		m.offset, m.taskIdx, m.imgIdx, m.pending = 0, -1, -1, ""
	}
	m.notePath, m.content = notePath, content
	m.gen++
	m.needTick = false
	return m, m.tick()
}

// SetTheme re-renders everything with new styles and palette, right away.
func (m Model) SetTheme(styles theme.Styles, palette theme.Palette) (Model, tea.Cmd) {
	m.styles, m.palette = styles, palette
	m.gen++
	m.needTick = false
	return m, m.startRender()
}

// SetOverlayOpen swaps images for chips while an overlay is open, since
// the overlay would dim the placeholder colors (spec §6.4).
func (m Model) SetOverlayOpen(open bool) Model {
	m.overlay = open
	return m
}

// SetTerminalReady releases Kitty transmissions held until the terminal
// entered the alternate screen. The app calls it after
// tea.KeyboardEnhancementsMsg or a 100ms tick.
func (m Model) SetTerminalReady() (Model, tea.Cmd) {
	if m.ready {
		return m, nil
	}
	m.ready = true
	return m, m.kittyCmd(nil)
}

// ResetKittyState forgets every transmitted image, for after a tea.Exec
// (the terminal left and re-entered the alternate screen, which wipes its
// image store), and re-transmits the images of the current note. Call it
// once the terminal is back.
func (m Model) ResetKittyState() (Model, tea.Cmd) {
	clear(m.sh.sent)
	if !m.ready {
		return m, nil
	}
	return m, m.kittyCmd(nil)
}

// KittyCleanup returns the sequences that delete every transmitted image,
// for the app to write on quit.
func (m Model) KittyCleanup() string {
	var b strings.Builder
	for _, id := range slices.Sorted(maps.Keys(m.sh.sent)) {
		b.WriteString(m.wrap(imgrender.KittyDelete(id)))
	}
	return b.String()
}

// Update handles the render pipeline messages and, in ModeFull, keys.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	var cmds []tea.Cmd
	if m.needTick {
		m.needTick = false
		cmds = append(cmds, m.tick())
	}
	switch msg := msg.(type) {
	case renderTickMsg:
		if msg.sh == m.sh && msg.gen == m.gen {
			cmds = append(cmds, m.startRender())
		}
	case renderedMsg:
		if msg.sh == m.sh {
			var c tea.Cmd
			m, c = m.applyRender(msg)
			cmds = append(cmds, c)
		}
	case tea.KeyPressMsg:
		if m.mode == ModeFull {
			var c tea.Cmd
			m, c = m.handleKey(msg)
			cmds = append(cmds, c)
		}
	}
	return m, tea.Batch(cmds...)
}

func (m Model) tick() tea.Cmd {
	sh, gen := m.sh, m.gen
	return tea.Tick(m.debounce, func(time.Time) tea.Msg { return renderTickMsg{sh: sh, gen: gen} })
}

// startRender snapshots what the render needs and returns the command that
// runs it. Text segments already in the cache are passed along as is.
func (m Model) startRender() tea.Cmd {
	segs := Split(m.content)
	job := renderJob{
		gen:       m.gen,
		sh:        m.sh,
		notePath:  m.notePath,
		content:   m.content,
		width:     m.width,
		height:    m.height,
		palette:   m.palette,
		caps:      m.caps,
		vaultRoot: m.vaultRoot,
		segs:      segs,
	}
	cw := contentWidth(m.width)
	for _, s := range segs {
		if s.Kind != Text {
			continue
		}
		key := textKey{hash: hashString(s.Markdown), width: cw, palette: m.palette.Name}
		lines, ok := m.sh.textCache[key]
		job.texts = append(job.texts, textJob{key: key, markdown: s.Markdown, lines: lines, cached: ok})
	}
	return job.run
}

// applyRender stores a finished render. Evicted Kitty images are deleted
// even when the render itself is stale.
func (m Model) applyRender(msg renderedMsg) (Model, tea.Cmd) {
	maps.Copy(m.sh.textCache, msg.newTexts)
	var deletes []string
	for _, r := range msg.evicted {
		if r.KittyID != 0 && m.sh.sent[r.KittyID] {
			deletes = append(deletes, m.wrap(imgrender.KittyDelete(r.KittyID)))
			delete(m.sh.sent, r.KittyID)
		}
	}
	if msg.gen != m.gen {
		return m, rawCmd(strings.Join(deletes, ""))
	}
	// Keep only the text renderings the current document uses.
	used := map[textKey]bool{}
	cw := contentWidth(msg.doc.width)
	for _, s := range msg.doc.segs {
		if s.Kind == Text {
			used[textKey{hash: hashString(s.Markdown), width: cw, palette: m.palette.Name}] = true
		}
	}
	maps.DeleteFunc(m.sh.textCache, func(k textKey, _ []string) bool { return !used[k] })

	m.doc = msg.doc
	if m.taskIdx >= len(m.doc.tasks) {
		m.taskIdx = len(m.doc.tasks) - 1
	}
	if m.imgIdx >= len(m.doc.images) {
		m.imgIdx = len(m.doc.images) - 1
	}
	m.clampOffset()
	return m, m.kittyCmd(deletes)
}

// kittyCmd writes pending delete sequences and, once the terminal is
// ready, the transmissions of the current note's Kitty images not sent
// yet, as one tea.Raw.
func (m Model) kittyCmd(deletes []string) tea.Cmd {
	var b strings.Builder
	for _, d := range deletes {
		b.WriteString(d)
	}
	if m.ready && m.doc != nil {
		for _, item := range m.doc.images {
			if item.kittyID == 0 || item.transmit == "" || m.sh.sent[item.kittyID] {
				continue
			}
			b.WriteString(item.transmit)
			m.sh.sent[item.kittyID] = true
		}
	}
	return rawCmd(b.String())
}

func rawCmd(s string) tea.Cmd {
	if s == "" {
		return nil
	}
	return tea.Raw(s)
}

func (m Model) wrap(seq string) string {
	if seq != "" && m.caps.TmuxPassthrough {
		return imgrender.WrapTmux(seq)
	}
	return seq
}

// FollowLine scrolls, in split view, so the rendered part of the segment
// holding source line is visible.
func (m Model) FollowLine(line int) Model {
	if m.doc == nil {
		return m
	}
	si := segmentForLine(m.doc.segs, line)
	if si < 0 {
		return m
	}
	seg := m.doc.segs[si]
	top, n := m.doc.segTop[si], m.doc.segHeight[si]
	row := top
	if span := seg.EndLine - seg.StartLine + 1; n > 1 && span > 1 {
		row = top + min(n-1, (min(max(line, seg.StartLine), seg.EndLine)-seg.StartLine)*n/span)
	}
	if row < m.offset || row >= m.offset+m.height {
		m.offset = row - m.height/3
		m.clampOffset()
	}
	return m
}

func (m Model) totalLines() int {
	if m.doc == nil {
		return 0
	}
	return len(m.doc.lines)
}

func (m *Model) clampOffset() {
	m.offset = max(0, min(m.offset, m.totalLines()-m.height))
}

// View renders exactly height lines of width columns.
func (m Model) View() string {
	if m.height <= 0 {
		return ""
	}
	out := make([]string, m.height)
	for i := range out {
		out[i] = m.row(m.offset + i)
	}
	return strings.Join(out, "\n")
}

// row renders one layout row: a left padding column (task marker or image
// bar), the content, and a right padding column. Image rows are plain
// concatenation: Kitty placeholder cells must never get a foreground style.
func (m Model) row(r int) string {
	w := m.width
	if w <= 0 {
		return ""
	}
	left, content := " ", ""
	if m.doc != nil && r >= 0 && r < len(m.doc.lines) {
		dl := m.doc.lines[r]
		content = dl.text
		if dl.img >= 0 {
			item := m.doc.images[dl.img]
			if m.overlay || item.rows == nil {
				content = ""
				if dl.imgRow == 0 {
					content = m.chip(item)
				}
			}
			if dl.img == m.imgIdx {
				left = m.styles.Accent.Render("▌")
			}
		}
		if m.taskIdx >= 0 && m.taskIdx < len(m.doc.taskRows) && m.doc.taskRows[m.taskIdx] == r {
			left = m.styles.Accent.Render("▸")
		}
	}
	if w == 1 {
		return left
	}
	return left + fit(content, w-2) + " "
}

// chip is the one-line stand-in for an image: a warning when it cannot be
// shown, a plain chip otherwise (overlay open, images off).
func (m Model) chip(item *imgItem) string {
	if item.reason != "" {
		return m.styles.Warning.Render("🖼 " + item.name + "  (" + item.reason + ")")
	}
	return m.styles.Chip.Render("🖼 " + item.name)
}

// fit crops or pads s to exactly n columns.
func fit(s string, n int) string {
	if n <= 0 {
		return ""
	}
	w := ansi.StringWidth(s)
	if w > n {
		// The cut may leave a style open; close it before the padding.
		s = ansi.Truncate(s, n, "") + "\x1b[m"
		w = ansi.StringWidth(s)
	}
	if w < n {
		s += strings.Repeat(" ", n-w)
	}
	return s
}
