package resolver

import (
	"bytes"
	"fmt"
	"image"
	"path"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/mathieucroset/notty/internal/gitsync"
	"github.com/mathieucroset/notty/internal/imgrender"
	"github.com/mathieucroset/notty/internal/ui/textutil"

	// Formats for binary previews (the same set imgrender accepts).
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	_ "golang.org/x/image/webp"
)

// option is one numbered choice.
type option struct {
	label string
	kind  ChoiceKind
}

// choiceState is a choice screen: binary, modify/delete or path conflict.
type choiceState struct {
	explain  string
	options  []option
	selected int

	previews [2]*preview // binary: yours, theirs (nil when not an image)
	sizes    [2]int      // binary: byte sizes, -1 when the side is absent

	content []string // modify/delete: the edited side's lines
	scroll  int
}

func newChoiceState(f File) *choiceState {
	c := &choiceState{}
	switch f.Kind {
	case Binary:
		c.explain = "Binary file changed on both computers."
		c.options = []option{{"Keep yours", KeepOurs}, {"Keep theirs", KeepTheirs}}
		for i, b := range [][]byte{f.Ours, f.Theirs} {
			c.sizes[i] = -1
			if b != nil {
				c.sizes[i] = len(b)
			}
			if looksLikeImage(f.Path) {
				c.previews[i] = decodePreview(b)
			}
		}
	case ModifyDelete:
		edited := f.Ours
		if f.Deleted == "ours" {
			c.explain = "Deleted on this computer; edited on the other."
			edited = f.Theirs
		} else {
			c.explain = "Edited on this computer; deleted on the other."
		}
		c.options = []option{{"Keep edited", KeepEdited}, {"Delete", Delete}}
		if bytes.IndexByte(edited, 0) >= 0 {
			c.content = []string{fmt.Sprintf("(binary content, %s)", humanSize(len(edited)))}
		} else {
			c.content = strings.Split(strings.TrimSuffix(string(edited), "\n"), "\n")
		}
	case PathConflict:
		c.explain, c.options = pathOptions(f)
	}
	return c
}

func (c *choiceState) clone() *choiceState {
	d := *c
	return &d
}

// pathOptions explains a path conflict and lists its choices; the first
// option is the default.
func pathOptions(f File) (string, []option) {
	switch f.PathKind {
	case gitsync.TrashVsMove:
		if isTrash(f.OursPath) {
			return fmt.Sprintf("Moved to trash on this computer; moved to %s on the other.", f.TheirsPath),
				[]option{{"Keep at " + f.TheirsPath, KeepAtNewPath}, {"Keep in trash", KeepInTrash}}
		}
		return fmt.Sprintf("Moved to %s on this computer; moved to trash on the other.", f.OursPath),
			[]option{{"Keep at " + f.OursPath, KeepAtNewPath}, {"Keep in trash", KeepInTrash}}
	case gitsync.RenameRename:
		return fmt.Sprintf("Moved to %s on this computer; moved to %s on the other.", f.OursPath, f.TheirsPath),
			[]option{{"Keep " + f.OursPath, KeepPathA}, {"Keep " + f.TheirsPath, KeepPathB}, {"Keep both", KeepBoth}}
	}
	return "Moved to trash on both computers.", []option{{"Keep both trash items", KeepBoth}}
}

func isTrash(p string) bool { return strings.HasPrefix(p, ".trash/") }

// choiceKey handles a navigation key on an unresolved choice screen.
func (m Model) choiceKey(s string) (Model, tea.Cmd) {
	it := m.current()
	c := it.choice
	m.hint = ""
	if n, err := strconv.Atoi(s); err == nil && len(s) == 1 {
		if n >= 1 && n <= len(c.options) {
			m = m.mutateChoice(func(c *choiceState) { c.selected = n - 1 })
		}
		return m, nil
	}
	switch s {
	case "enter":
		if len(c.options) == 0 {
			return m, nil
		}
		path, kind := it.file.Path, c.options[c.selected].kind
		m = m.mutate(m.sel, func(it *item) { it.pending, it.err = true, "" })
		return m, emit(ResolveChoiceMsg{Path: path, Choice: kind})
	case "ctrl+d", "pgdown":
		return m.scrollChoice(max(1, m.columnHeight()/2)), nil
	case "ctrl+u", "pgup":
		return m.scrollChoice(-max(1, m.columnHeight()/2)), nil
	}
	return m, nil
}

// mutateChoice applies f to a copy of the selected file's choice state.
func (m Model) mutateChoice(f func(c *choiceState)) Model {
	return m.mutate(m.sel, func(it *item) {
		if it.choice == nil {
			return
		}
		it.choice = it.choice.clone()
		f(it.choice)
	})
}

// scrollChoice scrolls a modify/delete file's content.
func (m Model) scrollChoice(d int) Model {
	it := m.current()
	_, _, w := m.split()
	_, h := m.choiceLayout(it.choice, it.file, w, m.contentHeight())
	return m.mutateChoice(func(c *choiceState) {
		c.scroll = clampScroll(c.scroll+d, len(c.content), h)
	})
}

// preview is a decoded image and its half-block renderings by size.
type preview struct {
	img      image.Image
	w, h     int
	rendered map[[2]int][]string
}

var imageExts = map[string]bool{".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true}

func looksLikeImage(p string) bool { return imageExts[strings.ToLower(path.Ext(p))] }

// decodePreview decodes b as an image, rejecting oversized ones from the
// header before decoding any pixel data. It returns nil on failure.
func decodePreview(b []byte) *preview {
	if len(b) == 0 {
		return nil
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(b))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 ||
		cfg.Width > imgrender.MaxImageSide || cfg.Height > imgrender.MaxImageSide ||
		cfg.Width*cfg.Height > imgrender.MaxImagePixels {
		return nil
	}
	img, _, err := image.Decode(bytes.NewReader(b))
	if err != nil {
		return nil
	}
	return &preview{img: img, w: cfg.Width, h: cfg.Height, rendered: map[[2]int][]string{}}
}

// render returns the image fitted into cols×rows cells as half blocks.
func (p *preview) render(cols, rows, cellW, cellH int) []string {
	if cellW <= 0 || cellH <= 0 {
		cellW, cellH = 8, 16
	}
	c, r := imgrender.FitCells(p.w, p.h, cols, rows, cellW, cellH)
	key := [2]int{c, r}
	if out, ok := p.rendered[key]; ok {
		return out
	}
	out := imgrender.HalfBlocks(p.img, c, r)
	p.rendered[key] = out
	return out
}

// humanSize formats a byte count.
func humanSize(n int) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%d B", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	}
	return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
}

// choiceLayout wraps a choice screen's explanation and returns it with the
// height left for the preview or content below the options.
func (m Model) choiceLayout(c *choiceState, f File, w, h int) ([]string, int) {
	explain := textutil.Wrap(sanitize(c.explain), max(1, w-2))
	for i := range explain {
		explain[i] = " " + explain[i]
	}
	if f.Kind == PathConflict && f.Original != "" {
		explain = append(explain, m.styles.Muted.Render(" Originally "+sanitize(f.Original)))
	}
	used := len(explain) + 1 + len(c.options) + 1
	return explain, max(0, h-used)
}

// choiceContent renders a choice screen.
func (m Model) choiceContent(it *item, w, h int) []string {
	c := it.choice
	explain, rest := m.choiceLayout(c, it.file, w, h)
	out := append(explain, "")
	for i, o := range c.options {
		row := fmt.Sprintf("   %d  %s", i+1, sanitize(o.label))
		if i == c.selected {
			row = m.styles.SidebarSelectedFocused.Render(textutil.PadLine(fmt.Sprintf(" › %d  %s", i+1, sanitize(o.label)), max(0, w-1)))
		}
		out = append(out, row)
	}
	out = append(out, "")
	switch it.file.Kind {
	case Binary:
		out = append(out, m.binaryPreviews(c, w, rest)...)
	case ModifyDelete:
		top := clampScroll(c.scroll, len(c.content), rest)
		for i := top; i < len(c.content) && i < top+rest; i++ {
			out = append(out, m.styles.Muted.Render(" │ ")+sanitize(c.content[i]))
		}
	}
	return out
}

// binaryPreviews renders the two sides' headers and, for images, their
// half-block previews side by side.
func (m Model) binaryPreviews(c *choiceState, w, h int) []string {
	if h <= 0 {
		return nil
	}
	half := max(0, (w-1-previewGap)/2)
	var cols [2][]string
	for i, name := range []string{"Yours", "Theirs"} {
		head := " " + name
		switch {
		case c.sizes[i] < 0:
			head += " · absent"
		case c.previews[i] != nil:
			head += fmt.Sprintf(" · %s · %d×%d", humanSize(c.sizes[i]), c.previews[i].w, c.previews[i].h)
		default:
			head += " · " + humanSize(c.sizes[i])
		}
		col := []string{m.styles.PaneTitle.Render(head)}
		if p := c.previews[i]; p != nil && m.caps.Inline != imgrender.ProtoOff && h > 1 && half > 1 {
			for _, r := range p.render(half-1, h-1, m.caps.CellW, m.caps.CellH) {
				col = append(col, " "+r)
			}
		}
		cols[i] = textutil.FitBlock(col, half, h)
	}
	out := make([]string, h)
	gap := strings.Repeat(" ", previewGap)
	for i := range h {
		out[i] = cols[0][i] + gap + cols[1][i]
	}
	return out
}
