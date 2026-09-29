package sidebar

import (
	"strings"

	"github.com/sahilm/fuzzy"

	"github.com/mathieucroset/notty/internal/ui/msgs"
)

// pickerRows is the maximum number of matches the tag picker lists.
const pickerRows = 5

// tagPicker is the small inline fuzzy tag picker opened with '#'.
type tagPicker struct {
	query   string
	matches []msgs.TagCount
	cursor  int
}

func newTagPicker(tags []msgs.TagCount) *tagPicker {
	p := &tagPicker{}
	p.refilter(tags)
	return p
}

// refilter recomputes the matches for the current query. An empty query
// lists every tag in its given order; otherwise matches are ranked by fuzzy
// score. A leading '#' in the query is ignored.
func (p *tagPicker) refilter(tags []msgs.TagCount) {
	q := strings.TrimPrefix(p.query, "#")
	if q == "" {
		p.matches = append([]msgs.TagCount(nil), tags...)
	} else {
		names := make([]string, len(tags))
		for i, t := range tags {
			names[i] = t.Tag
		}
		p.matches = nil
		for _, fm := range fuzzy.Find(q, names) {
			p.matches = append(p.matches, tags[fm.Index])
		}
	}
	p.cursor = min(p.cursor, max(len(p.matches)-1, 0))
}

func (p *tagPicker) move(dir int) {
	if len(p.matches) == 0 {
		return
	}
	p.cursor = min(max(p.cursor+dir, 0), len(p.matches)-1)
}

func (p *tagPicker) current() (string, bool) {
	if p.cursor < 0 || p.cursor >= len(p.matches) {
		return "", false
	}
	return p.matches[p.cursor].Tag, true
}

// height is the number of lines the picker occupies: a separator, the
// input line, and up to pickerRows matches (or a "no match" line).
func (p *tagPicker) height() int {
	return 2 + max(min(len(p.matches), pickerRows), 1)
}

func (p *tagPicker) view(m Model) []string {
	out := []string{
		padLine(m.styles.SidebarSection.Render("  Filter by tag"), m.width),
		padLine("  "+m.styles.Accent.Render("#")+m.styles.SidebarItem.Render(strings.TrimPrefix(p.query, "#"))+m.styles.Accent.Render("▏"), m.width),
	}
	if len(p.matches) == 0 {
		return append(out, padLine(m.styles.Muted.Render("    no matching tags"), m.width))
	}
	// Scroll the match list so the cursor stays visible.
	start := max(p.cursor-pickerRows+1, 0)
	end := min(start+pickerRows, len(p.matches))
	for i := start; i < end; i++ {
		text := "    " + chipText(p.matches[i])
		if i == p.cursor {
			out = append(out, m.selStyle().Render(padLine(text, m.width)))
			continue
		}
		out = append(out, padLine(m.styles.SidebarItem.Render(text), m.width))
	}
	return out
}
