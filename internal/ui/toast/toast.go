// Package toast implements Notty's toast notifications and error log (spec
// §4.3, §9). Info and warning toasts expire on their own after 4s; error
// toasts are sticky and stay until dismissed. Every toast, regardless of
// level, is kept in a capped log that the "!" error log overlay shows.
package toast

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/mathieucroset/notty/internal/ui/msgs"
	"github.com/mathieucroset/notty/internal/ui/textutil"
	"github.com/mathieucroset/notty/internal/ui/theme"
)

const (
	maxVisible = 3
	maxLog     = 100
	ttl        = 4 * time.Second
	boxWidth   = 48
)

// Entry is one logged toast.
type Entry struct {
	Time  time.Time
	Level msgs.ToastLevel
	Text  string
}

type toastItem struct {
	id    int
	level msgs.ToastLevel
	text  string
}

// expireMsg is delivered ttl after an info/warn toast is pushed, asking to
// remove it (by id, since others may have been pushed or dismissed since).
type expireMsg struct{ id int }

// Model is the toast stack and its log.
type Model struct {
	styles theme.Styles
	nextID int
	active []toastItem // oldest first
	log    []Entry     // newest first, capped at maxLog
}

// New returns an empty toast stack styled with styles.
func New(styles theme.Styles) Model {
	return Model{styles: styles}
}

// Push adds a toast and logs it. Info and warning toasts return a tea.Tick
// command that expires them after 4s; error toasts are sticky (nil command)
// and stay until Dismiss.
func (m Model) Push(level msgs.ToastLevel, text string) (Model, tea.Cmd) {
	id := m.nextID
	m.nextID++
	m.active = append(append([]toastItem(nil), m.active...), toastItem{id: id, level: level, text: text})

	log := make([]Entry, 0, min(len(m.log)+1, maxLog))
	log = append(log, Entry{Time: time.Now(), Level: level, Text: text})
	log = append(log, m.log...)
	if len(log) > maxLog {
		log = log[:maxLog]
	}
	m.log = log

	if level == msgs.ToastError {
		return m, nil
	}
	return m, tea.Tick(ttl, func(time.Time) tea.Msg { return expireMsg{id: id} })
}

// Update handles a toast's expiry.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	em, ok := msg.(expireMsg)
	if !ok {
		return m, nil
	}
	active := make([]toastItem, 0, len(m.active))
	for _, t := range m.active {
		if t.id != em.id {
			active = append(active, t)
		}
	}
	m.active = active
	return m, nil
}

// Dismiss removes the newest sticky (error) toast. The app binds this to
// esc while toasts are visible.
func (m Model) Dismiss() Model {
	for i := len(m.active) - 1; i >= 0; i-- {
		if m.active[i].level == msgs.ToastError {
			active := append([]toastItem(nil), m.active[:i]...)
			m.active = append(active, m.active[i+1:]...)
			return m
		}
	}
	return m
}

// Visible reports whether any toast is currently shown.
func (m Model) Visible() bool { return len(m.active) > 0 }

// Log returns the last 100 logged toasts, newest first.
func (m Model) Log() []Entry {
	return append([]Entry(nil), m.log...)
}

// visible returns at most the last maxVisible active toasts, oldest first.
func (m Model) visible() []toastItem {
	if len(m.active) <= maxVisible {
		return m.active
	}
	return m.active[len(m.active)-maxVisible:]
}

func icon(level msgs.ToastLevel) string {
	switch level {
	case msgs.ToastWarn:
		return "⚠"
	case msgs.ToastError:
		return "✗"
	default:
		return "ℹ"
	}
}

func (m Model) styleFor(level msgs.ToastLevel) lipgloss.Style {
	switch level {
	case msgs.ToastWarn:
		return m.styles.ToastWarn
	case msgs.ToastError:
		return m.styles.ToastError
	default:
		return m.styles.ToastInfo
	}
}

// View renders the visible toasts stacked, each a rounded box up to 48
// columns wide (or width, if narrower), for the app to place bottom-right
// as a layer.
func (m Model) View(width int) string {
	w := boxWidth
	if width > 0 && width < w {
		w = width
	}
	items := m.visible()
	boxes := make([]string, 0, len(items))
	for _, t := range items {
		boxes = append(boxes, m.renderToast(t, w))
	}
	return strings.Join(boxes, "\n")
}

func (m Model) renderToast(t toastItem, outerWidth int) string {
	style := m.styleFor(t.level).Border(lipgloss.RoundedBorder())

	// outerWidth includes a 1-cell border and Padding(0, 1) on each side.
	contentWidth := max(outerWidth-4, 1)
	textWidth := max(contentWidth-2, 1) // icon + space

	wrapped := textutil.Wrap(t.text, textWidth)
	for i, l := range wrapped {
		prefix := "  "
		if i == 0 {
			prefix = icon(t.level) + " "
		}
		wrapped[i] = textutil.PadLine(prefix+l, contentWidth)
	}

	return style.Width(outerWidth).Render(strings.Join(wrapped, "\n"))
}
