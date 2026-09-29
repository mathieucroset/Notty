package finder

import (
	"strings"

	"charm.land/glamour/v2"
	"charm.land/lipgloss/v2"

	"github.com/mathieucroset/notty/internal/index"
	"github.com/mathieucroset/notty/internal/ui/theme"
)

// previewKey caches a note's rendered preview by path and pane width, since
// the same note is very likely to be re-rendered on the next keystroke.
type previewKey struct {
	path  string
	width int
}

// previewLines returns note's Glamour-rendered content as lines exactly
// width columns wide, from the cache when available.
func (m Model) previewLines(note *index.Note, width int) []string {
	if width <= 0 {
		return nil
	}
	key := previewKey{path: note.Path, width: width}
	if lines, ok := m.previewCache[key]; ok {
		return lines
	}
	lines := renderMarkdown(note.Content, width, m.palette)
	m.previewCache[key] = lines
	return lines
}

// renderMarkdown renders content with Glamour at wordWrap width, styled from
// p, split into individually width-padded lines. Render errors fall back to
// a single placeholder line rather than failing the whole overlay.
func renderMarkdown(content string, width int, p theme.Palette) []string {
	r, err := glamour.NewTermRenderer(
		glamour.WithStyles(theme.GlamourStyle(p)),
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

// previewPaneLines builds the preview pane's body: the selected note's
// rendered content, from the top.
func (m Model) previewPaneLines(width, height int) []string {
	blank := m.bgStyle(lipgloss.NewStyle(), false).Render(strings.Repeat(" ", max(width, 0)))
	if height <= 0 {
		return nil
	}
	note, ok := m.selectedNote()
	if !ok || width <= 0 {
		out := make([]string, height)
		for i := range out {
			out[i] = blank
		}
		return out
	}

	full := m.previewLines(note, width)
	out := make([]string, height)
	for i := range out {
		if i >= len(full) {
			out[i] = blank
			continue
		}
		out[i] = full[i]
	}
	return out
}
