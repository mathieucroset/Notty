package wizard

import (
	"image/color"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/mathieucroset/notty/internal/ui/icons"
	"github.com/mathieucroset/notty/internal/ui/theme"
)

// themeState is the theme step (first run only).
type themeState struct {
	names []string
	idx   int
	// cur is the highlighted theme, resolved once when the cursor lands on
	// it: the sample card and enter use it without reading its file again.
	cur theme.Palette
	// err is why the highlighted theme does not load; enter is refused
	// while it is set, so a broken theme is never chosen.
	err error
	// base is the styles the wizard was opened with, restored when esc
	// leaves the step.
	base theme.Styles
}

// enterTheme shows the theme list after a successful first-run setup, with
// the cursor on the current theme.
func (m Model) enterTheme() (Model, tea.Cmd) {
	m.stage = StageTheme
	m.theme.names = m.cat.Names()
	m.theme.idx = max(slices.Index(m.theme.names, m.current.Name), 0)
	m.theme.cur, m.theme.err = m.current, nil
	if len(m.theme.names) > 0 && m.theme.names[m.theme.idx] != m.current.Name {
		m.theme.cur, m.theme.err = m.resolve(m.theme.names[m.theme.idx])
	}
	m.focus()
	return m, nil
}

// resolve returns the named palette: the current one from memory, any
// other through the catalog (a user theme is read from its file).
func (m Model) resolve(name string) (theme.Palette, error) {
	if name == m.current.Name {
		return m.current, nil
	}
	return m.cat.Resolve(name)
}

func (m Model) themeKey(msg tea.KeyPressMsg) (Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k", "ctrl+k", "shift+tab":
		if m.theme.idx > 0 {
			m.theme.idx--
			return m.preview(m.theme.names[m.theme.idx])
		}
	case "down", "j", "ctrl+j", "tab":
		if m.theme.idx < len(m.theme.names)-1 {
			m.theme.idx++
			return m.preview(m.theme.names[m.theme.idx])
		}
	case "enter":
		if m.theme.err != nil {
			return m, nil
		}
		return m, emit(m.done(m.theme.cur))
	case "esc":
		m.styles = m.theme.base
		m.applyStyles()
		m.theme.err = nil
		m.stage = StageSync
		m.sub = subList
		m.focus()
		return m, emit(ThemePreviewMsg{Palette: m.current})
	}
	return m, nil
}

// preview resolves the named theme, re-styles the wizard in it and asks
// the app to do the same. A theme that does not load keeps the colors and
// shows its error in the step instead.
func (m Model) preview(name string) (Model, tea.Cmd) {
	p, err := m.resolve(name)
	m.theme.cur, m.theme.err = p, err
	if err != nil {
		return m, nil
	}
	m.styles = theme.NewStyles(p).WithIcons(m.styles.Icons)
	m.applyStyles()
	return m, emit(ThemePreviewMsg{Palette: p})
}

const (
	sampleInner = 24 // text columns inside the sample card
	themeGap    = 3
)

func (m Model) themeView(w int) string {
	var head []string
	if m.run.conflicted {
		head = append(head, m.wrap(m.styles.Warning, "Your notes and the remote both changed some files. You'll resolve the conflicts right after setup.", w), "")
	} else {
		head = append(head, m.styles.Success.Render(m.styles.Icons.Check+" ")+m.styles.StatusText.Render("Your vault is ready"), "")
	}
	head = append(head, m.styles.StatusText.Render("Pick a theme"), "")

	list := m.themeList()
	if m.theme.err != nil {
		return strings.Join(head, "\n") + "\n" + list + "\n\n" + m.note(m.styles.Error, "Could not load "+m.theme.err.Error(), w)
	}
	card := sampleCard(m.theme.cur, m.styles.Icons)
	listW := lipgloss.Width(list)
	var body string
	if listW+themeGap+lipgloss.Width(card) <= w {
		body = lipgloss.JoinHorizontal(lipgloss.Top, list, strings.Repeat(" ", themeGap), card)
	} else {
		body = list + "\n\n" + card
	}
	return strings.Join(head, "\n") + "\n" + body
}

func (m Model) themeList() string {
	lines := make([]string, len(m.theme.names))
	for i, n := range m.theme.names {
		var line string
		if i == m.theme.idx {
			line = m.styles.Accent.Render("› ") + m.styles.Accent.Bold(true).Render(n)
		} else {
			line = "  " + m.styles.StatusText.Render(n)
		}
		if n == m.current.Name {
			line += m.styles.Muted.Render(" · current")
		}
		lines[i] = line
	}
	return strings.Join(lines, "\n")
}

// sampleCard renders a mini note in palette p: a heading, a bullet, an open
// and a done task, a wiki link and a code line, on the theme's background.
func sampleCard(p theme.Palette, set icons.Set) string {
	bg := lipgloss.NewStyle().Background(p.Base)
	fg := func(c color.Color) lipgloss.Style { return bg.Foreground(c) }
	text := fg(p.Text)

	type span struct {
		st lipgloss.Style
		s  string
	}
	rows := [][]span{
		{{fg(p.Headings[0]).Bold(true), "# Weekend plans"}},
		{},
		{{fg(p.Accent), "• "}, {text, "Farmers market"}},
		{{fg(p.Accent2), set.TaskOpen + " "}, {text, "Call the bakery"}},
		{{fg(p.Muted), set.TaskDone + " "}, {fg(p.Muted).Strikethrough(true), "Water the plants"}},
		{{text, "See "}, {fg(p.Accent).Underline(true), "[[Recipes]]"}},
		{},
		{{lipgloss.NewStyle().Background(p.Surface).Foreground(p.Accent2), " git pull "}},
	}
	lines := make([]string, len(rows))
	for i, row := range rows {
		var b strings.Builder
		b.WriteString(bg.Render(" "))
		used := 1
		for _, sp := range row {
			s := ansi.Truncate(sp.s, max(0, sampleInner+1-used), "")
			b.WriteString(sp.st.Render(s))
			used += ansi.StringWidth(s)
		}
		b.WriteString(bg.Render(strings.Repeat(" ", max(0, sampleInner+2-used))))
		lines[i] = b.String()
	}
	blank := bg.Render(strings.Repeat(" ", sampleInner+2))
	body := blank + "\n" + strings.Join(lines, "\n") + "\n" + blank
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(p.Muted).
		Render(body)
}
