package editor

import (
	"fmt"
	"image/color"
	"path"
	"path/filepath"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/styles"

	"github.com/mathieucroset/notty/internal/buffer"
	"github.com/mathieucroset/notty/internal/imgrender"
	"github.com/mathieucroset/notty/internal/links"
	"github.com/mathieucroset/notty/internal/mdstyle"
	"github.com/mathieucroset/notty/internal/tasks"
	"github.com/mathieucroset/notty/internal/ui/icons"
	"github.com/mathieucroset/notty/internal/ui/theme"
)

// role marks display-only units whose style does not come from a span.
type role uint8

const (
	roleNone role = iota
	roleTaskGlyph
	roleTaskDoneGlyph
	roleChip
	roleChipMissing
	roleProgress
	roleQuoteBar
	roleRule
	roleControl
)

// sty identifies the style of one cell. It is comparable, so runs of equal
// cells are rendered together and styles are memoized.
type sty struct {
	kind mdstyle.Kind
	tok  chroma.TokenType
	role role
	sel  bool
}

// unit is one grapheme of a line's display form. [src, end) are the source
// grapheme columns it stands for, used for the selection: one column for a
// typed grapheme, the replaced range for a substitution (a task glyph, a
// chip, a rule), empty for display-only additions (heading progress).
type unit struct {
	g        string
	src, end int
	st       sty
}

// styler maps cell styles to lipgloss styles built from the palette.
type styler struct {
	p      theme.Palette
	s      theme.Styles
	chroma *chroma.Style
	cache  map[sty]lipgloss.Style
}

func newStyler(s theme.Styles, p theme.Palette) *styler {
	var cs *chroma.Style
	if p.Name != "" {
		unlock := theme.RLockChroma()
		cs = styles.Get(theme.ChromaStyleName(p))
		unlock()
	}
	return &styler{p: p, s: s, chroma: cs, cache: map[sty]lipgloss.Style{}}
}

func (r *styler) style(k sty) lipgloss.Style {
	if st, ok := r.cache[k]; ok {
		return st
	}
	st := r.base(k)
	if k.sel {
		st = bg(st, r.p.Overlay)
	}
	r.cache[k] = st
	return st
}

// fg sets the foreground when c is set (a zero Palette has nil colors).
func fg(st lipgloss.Style, c color.Color) lipgloss.Style {
	if c == nil {
		return st
	}
	return st.Foreground(c)
}

// bg sets the background when c is set.
func bg(st lipgloss.Style, c color.Color) lipgloss.Style {
	if c == nil {
		return st
	}
	return st.Background(c)
}

func (r *styler) base(k sty) lipgloss.Style {
	p := r.p
	st := lipgloss.NewStyle()
	switch k.role {
	case roleTaskGlyph:
		return fg(st, p.Accent)
	case roleTaskDoneGlyph, roleQuoteBar, roleRule, roleControl:
		return fg(st, p.Muted)
	case roleChip:
		return bg(fg(st, p.Accent2), p.Surface)
	case roleChipMissing:
		return bg(fg(st, p.Warning), p.Surface)
	case roleProgress:
		return fg(st, p.Success)
	}
	switch k.kind {
	case mdstyle.Markup, mdstyle.CodeFence, mdstyle.LinkURL, mdstyle.Rule:
		return fg(st, p.Muted)
	case mdstyle.H1, mdstyle.H2, mdstyle.H3, mdstyle.H4, mdstyle.H5, mdstyle.H6:
		return fg(st.Bold(true), p.Headings[k.kind-mdstyle.H1])
	case mdstyle.Bold:
		return fg(st.Bold(true), p.Text)
	case mdstyle.Italic:
		return fg(st.Italic(true), p.Text)
	case mdstyle.BoldItalic:
		return fg(st.Bold(true).Italic(true), p.Text)
	case mdstyle.Strike:
		return fg(st.Strikethrough(true), p.Text)
	case mdstyle.Code:
		return bg(fg(st, p.Text), p.Surface)
	case mdstyle.CodeBlock:
		return r.code(st, k.tok)
	case mdstyle.Link:
		return fg(st.Underline(true), p.Accent)
	case mdstyle.Image, mdstyle.Tag:
		return fg(st, p.Accent2)
	case mdstyle.TaskOpen, mdstyle.ListMarker:
		return fg(st, p.Accent)
	case mdstyle.TaskDone:
		return fg(st, p.Muted)
	case mdstyle.TaskDoneText:
		return fg(st.Strikethrough(true), p.Muted)
	case mdstyle.Quote:
		return fg(st.Italic(true), p.Subtext)
	}
	return fg(st, p.Text)
}

// code styles a fenced code token with the chroma style the preview uses.
func (r *styler) code(st lipgloss.Style, tok chroma.TokenType) lipgloss.Style {
	if r.chroma == nil {
		return fg(st, r.p.Text)
	}
	e := r.chroma.Get(tok)
	if e.Colour.IsSet() {
		st = st.Foreground(lipgloss.Color(e.Colour.String()))
	} else {
		st = fg(st, r.p.Text)
	}
	if e.Bold == chroma.Yes {
		st = st.Bold(true)
	}
	if e.Italic == chroma.Yes {
		st = st.Italic(true)
	}
	return st
}

// dim is a cached image size lookup.
type dim struct {
	w, h int
	ok   bool
}

// aux holds render caches shared by copies of a Model.
type aux struct {
	dims map[string]dim // full image path -> size

	progBuf     *buffer.Buffer
	progVersion uint64
	prog        map[int][2]int // heading line -> done, total
	lines       []string

	// Line layout memo, valid for one buffer version: ensureVisible, View
	// and CursorPosition all need the same lines within one frame.
	memoBuf     *buffer.Buffer
	memoVersion uint64
	memo        map[int]*lineLayout
	builds      int // layouts computed (for tests)
}

// layoutKey is what a line's layout depends on besides the buffer version.
type layoutKey struct {
	text  string
	raw   bool
	width int
}

// lineLayout is the display form of a line and its soft-wrap rows.
type lineLayout struct {
	key    layoutKey
	units  []unit
	gs     []string // graphemes of units
	starts []int
}

func newAux() *aux { return &aux{dims: map[string]dim{}} }

// layout returns the memoized layout of line i when it is still valid for
// key, or nil. It resets the memo when the buffer changed or when it grew
// well past a screenful (scrolling through a long note).
func (a *aux) layout(b *buffer.Buffer, i int, key layoutKey, height int) *lineLayout {
	if a.memoBuf != b || a.memoVersion != b.Version() || a.memo == nil || len(a.memo) > 4*height+64 {
		a.memoBuf, a.memoVersion = b, b.Version()
		a.memo = map[int]*lineLayout{}
		return nil
	}
	if l := a.memo[i]; l != nil && l.key == key {
		return l
	}
	return nil
}

// invalidate drops the layout memo (image sizes or the theme changed).
func (a *aux) invalidate() { a.memo = nil }

// linesOf returns the buffer's lines, cached per buffer version.
func (a *aux) linesOf(b *buffer.Buffer) []string {
	if a.progBuf != b || a.progVersion != b.Version() || a.lines == nil {
		a.progBuf, a.progVersion = b, b.Version()
		a.lines = make([]string, b.LineCount())
		for i := range a.lines {
			a.lines[i] = b.Line(i)
		}
		a.prog = map[int][2]int{}
	}
	return a.lines
}

// progress returns the task progress of the heading at line i.
func (a *aux) progress(b *buffer.Buffer, i int) (done, total int) {
	lines := a.linesOf(b)
	if p, ok := a.prog[i]; ok {
		return p[0], p[1]
	}
	done, total = tasks.Progress(lines, i)
	a.prog[i] = [2]int{done, total}
	return done, total
}

// dimensions returns the cached pixel size of the image at full.
func (a *aux) dimensions(full string) dim {
	if d, ok := a.dims[full]; ok {
		return d
	}
	w, h, _ := imgrender.Dimensions(full)
	d := dim{w: w, h: h, ok: w > 0 && h > 0}
	a.dims[full] = d
	return d
}

// forgetMissing drops cached misses so images added since are picked up.
func (a *aux) forgetMissing() {
	a.invalidate()
	for k, d := range a.dims {
		if !d.ok {
			delete(a.dims, k)
		}
	}
}

// progressBar renders a 5-cell bar for done/total in set's glyphs, like
// "▰▰▰▱▱ 3/5".
func progressBar(set icons.Set, done, total int) string {
	const width = 5
	filled := (done*width + total/2) / total
	return strings.Repeat(set.ProgressFull, filled) + strings.Repeat(set.ProgressEmpty, width-filled) + fmt.Sprintf(" %d/%d", done, total)
}

// rawUnits returns line i as typed, styled by its mdstyle spans.
func (m Model) rawUnits(i int) []unit {
	line := m.buf.Line(i)
	spans := m.md.Spans(i)
	gs := buffer.Graphemes(line)
	units := make([]unit, len(gs))
	off, j := 0, 0
	for c, g := range gs {
		for j < len(spans) && spans[j].End <= off {
			j++
		}
		var st sty
		if j < len(spans) && spans[j].Start <= off {
			st = sty{kind: spans[j].Kind, tok: spans[j].Token}
		}
		if glyph, ok := controlGlyph(g); ok {
			units[c] = unit{g: glyph, src: c, end: c + 1, st: sty{role: roleControl}}
		} else {
			units[c] = unit{g: g, src: c, end: c + 1, st: st}
		}
		off += len(g)
	}
	return units
}

// textUnits turns s into display units standing for source columns
// [from, to).
func textUnits(s string, from, to int, st sty) []unit {
	gs := buffer.Graphemes(s)
	out := make([]unit, len(gs))
	for i, g := range gs {
		if glyph, ok := controlGlyph(g); ok {
			g = glyph // e.g. an escape character in an image name
		}
		out[i] = unit{g: g, src: from, end: to, st: st}
	}
	return out
}

// splice replaces units[from:to] with repl.
func splice(units []unit, from, to int, repl []unit) []unit {
	out := make([]unit, 0, len(units)-(to-from)+len(repl))
	out = append(out, units[:from]...)
	out = append(out, repl...)
	return append(out, units[to:]...)
}

// displayUnits returns the display form of line i. Raw lines (the cursor
// line) show the markdown as typed; other lines get the display
// substitutions of spec §5: task glyphs, image chips, quote bars, rules and
// heading progress.
func (m Model) displayUnits(i int, raw bool) []unit {
	units := m.rawUnits(i)
	if raw {
		return units
	}
	line := m.buf.Line(i)
	spans := m.md.Spans(i)
	if len(spans) == 0 {
		return units
	}
	n := len(units)

	switch spans[0].Kind {
	case mdstyle.Rule:
		return textUnits(strings.Repeat("─", max(1, m.lay.width)), 0, n, sty{role: roleRule})
	case mdstyle.CodeBlock, mdstyle.CodeFence:
		return units
	}

	if links.ImageOnlyParagraph(line) {
		if chips := m.chips(line); chips != nil {
			return chips
		}
	}

	quote := spans[0].Kind == mdstyle.Markup && strings.HasPrefix(strings.TrimLeft(line, " "), ">")
	if quote {
		// The tokenizer does not look for tasks inside a quote; do it on the
		// quoted text so "> - [ ] x" shows the bar and the glyph.
		q := spans[0].End
		inner, _ := mdstyle.TokenizeLine(line[q:], mdstyle.State{})
		for k := range inner {
			inner[k].Start += q
			inner[k].End += q
		}
		units = taskGlyph(m.opts.Styles.Icons, units, line, inner, true)
		sp := spans[0]
		bar := "▎"
		if sp.End-sp.Start > 1 && strings.HasSuffix(line[sp.Start:sp.End], " ") {
			bar += " "
		}
		from, to := buffer.ByteToCol(line, sp.Start), buffer.ByteToCol(line, sp.End)
		return splice(units, from, to, textUnits(bar, from, to, sty{role: roleQuoteBar}))
	}

	units = taskGlyph(m.opts.Styles.Icons, units, line, spans, false)

	if spans[0].Kind == mdstyle.Markup && strings.HasPrefix(strings.TrimLeft(line, " "), "#") {
		if done, total := m.aux.progress(m.buf, i); total > 0 {
			units = append(units, textUnits(" "+progressBar(m.opts.Styles.Icons, done, total), n, n, sty{role: roleProgress})...)
		}
	}
	return units
}

// taskGlyph replaces the checkbox of a task line (and a bullet marker
// before it) with set's checkbox. Units are still one per source column, so span
// columns index them directly. restyle marks the text of a done task as
// done (for tasks inside a quote, which the tokenizer styles as quote text).
func taskGlyph(set icons.Set, units []unit, line string, spans []mdstyle.Span, restyle bool) []unit {
	col := func(off int) int { return buffer.ByteToCol(line, off) }
	for k, sp := range spans {
		if sp.Kind != mdstyle.TaskOpen && sp.Kind != mdstyle.TaskDone {
			continue
		}
		glyph, r := set.TaskOpen, roleTaskGlyph
		if sp.Kind == mdstyle.TaskDone {
			glyph, r = set.TaskDone, roleTaskDoneGlyph
			if restyle {
				for c := col(sp.End); c < len(units); c++ {
					units[c].st = sty{kind: mdstyle.TaskDoneText}
				}
			}
		}
		from, to := col(sp.Start), col(sp.End)
		if k > 0 && spans[k-1].Kind == mdstyle.ListMarker {
			if mk := strings.TrimSpace(line[spans[k-1].Start:spans[k-1].End]); mk == "-" || mk == "*" || mk == "+" {
				from = col(spans[k-1].Start)
			}
		}
		return splice(units, from, to, textUnits(glyph, from, to, sty{role: r}))
	}
	return units
}

// chips renders an image-only line as one chip per image:
// "<image icon> name.png  640×480", or "missing" when the file cannot be
// read.
func (m Model) chips(line string) []unit {
	imgs := links.FindImages(line)
	if len(imgs) == 0 {
		return nil
	}
	var out []unit
	for k, img := range imgs {
		src, end := buffer.ByteToCol(line, img.Start), buffer.ByteToCol(line, img.End)
		if k > 0 {
			out = append(out, textUnits(" ", src, src, sty{})...)
		}
		name := path.Base(img.Target)
		icon := m.opts.Styles.Icons.Image
		text, r := " "+icon+" "+name+" ", roleChip
		rel, external := links.Resolve(img.Target, m.path)
		switch {
		case external:
		case rel == "":
			text, r = " "+icon+" "+name+"  missing ", roleChipMissing
		default:
			d := m.aux.dimensions(filepath.Join(m.opts.VaultRoot, filepath.FromSlash(rel)))
			if d.ok {
				text = fmt.Sprintf(" %s %s  %d×%d ", icon, name, d.w, d.h)
			} else {
				text, r = " "+icon+" "+name+"  missing ", roleChipMissing
			}
		}
		out = append(out, textUnits(text, src, end, sty{role: r})...)
	}
	return out
}

// graphemesOf returns the graphemes of units.
func graphemesOf(units []unit) []string {
	gs := make([]string, len(units))
	for i, u := range units {
		gs[i] = u.g
	}
	return gs
}

// renderRow renders units as one screen row wrapped at width and returns
// the text and its width in cells.
func (m Model) renderRow(units []unit, width int) (string, int) {
	var sb strings.Builder
	var run strings.Builder
	var cur sty
	x := 0
	flush := func() {
		if run.Len() > 0 {
			sb.WriteString(m.sty.style(cur).Render(run.String()))
			run.Reset()
		}
	}
	for _, u := range units {
		w := cellWidth(u.g, x, width)
		if u.st != cur {
			flush()
			cur = u.st
		}
		if u.g == "\t" {
			run.WriteString(strings.Repeat(" ", w))
		} else {
			run.WriteString(u.g)
		}
		x += w
	}
	flush()
	return sb.String(), x
}
