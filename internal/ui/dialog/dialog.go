// Package dialog implements Notty's modal dialogs: a labeled text input
// (with optional autocomplete suggestions), a yes/no confirmation, and a
// numbered choice list (spec §4.2, §4.4).
//
// A Model renders only the dialog box itself; the caller composites it over
// a dimmed background with Overlay. Every result — confirmation or
// cancellation — is reported through ResultMsg rather than a direct
// callback, so the app stays in control of what happens next.
package dialog

import (
	"fmt"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/mathieucroset/notty/internal/ui/textutil"
	"github.com/mathieucroset/notty/internal/ui/theme"
)

// Kind is the kind of dialog a Model presents.
type Kind int

// Dialog kinds.
const (
	KindInput Kind = iota
	KindConfirm
	KindChoice
)

// ResultMsg is emitted when a dialog is confirmed (OK=true, on enter, a y/n
// shortcut, or a digit choice) or cancelled (OK=false, on esc). Value holds
// the entered text for Input dialogs; Choice holds the 0-based selected
// option index for Choice dialogs.
type ResultMsg struct {
	ID     string
	OK     bool
	Value  string
	Choice int
}

const (
	minWidth = 40
	maxWidth = 64

	// borderAndPadding is what styles.Dialog adds around the content: a
	// 1-cell border plus 2 cells of padding on each side.
	borderAndPadding = 6

	maxSuggestions = 6
)

// SuggestFunc returns the suggestions matching prefix, for Input dialogs
// built with WithSuggestions.
type SuggestFunc func(prefix string) []string

// Model is a modal dialog: an input field, a yes/no confirm, or a numbered
// choice list. It renders only the box (see package doc); the caller
// composites it over the rest of the screen with Overlay.
type Model struct {
	kind   Kind
	id     string
	title  string
	styles theme.Styles

	// Input.
	input     textinput.Model
	validate  func(string) error
	errMsg    string
	attempted bool // an enter (or programmatic change after one) has run validate

	suggestFn   SuggestFunc
	suggestions []string
	sugIndex    int // -1 when nothing is highlighted
	hint        string

	// Confirm.
	message  string
	yesLabel string
	noLabel  string
	danger   bool
	yesSel   bool // true when the yes button is highlighted

	// Choice.
	options   []string
	choiceSel int
}

// NewInput returns an Input dialog. initial pre-fills the field; validate
// may be nil to accept anything.
func NewInput(id, title, placeholder, initial string, validate func(string) error, styles theme.Styles) Model {
	ti := textinput.New()
	ti.Placeholder = placeholder
	ti.SetVirtualCursor(false)
	// Focus's returned tea.Cmd only starts the virtual cursor's blink
	// ticker; since virtual cursor rendering is off (renderInputLine draws
	// a static reverse-video cursor instead), there's no blink to start and
	// discarding it is safe. Focus itself still matters: it's what makes
	// the underlying textinput.Model accept key input at all.
	ti.Focus()
	ti.SetValue(initial)

	return Model{
		kind:     KindInput,
		id:       id,
		title:    title,
		styles:   styles,
		input:    ti,
		validate: validate,
		sugIndex: -1,
	}
}

// WithSuggestions enables an autocomplete dropdown for an Input dialog,
// showing up to 6 matches under the field. tab completes the highlighted
// suggestion and enter confirms it; up/down or ctrl+j/ctrl+k move the
// highlight. With no suggestion shown, enter confirms the typed text.
func (m Model) WithSuggestions(fn SuggestFunc) Model {
	m.suggestFn = fn
	m.refreshSuggestions()
	return m
}

// WithHint adds a dim line of help under an Input dialog's field.
func (m Model) WithHint(hint string) Model {
	m.hint = hint
	return m
}

// NewConfirm returns a yes/no confirmation dialog. When danger is true the
// yes button is drawn in the Error color and starts unselected, so a
// destructive action is never the default.
func NewConfirm(id, title, message, yesLabel, noLabel string, danger bool, styles theme.Styles) Model {
	return Model{
		kind:     KindConfirm,
		id:       id,
		title:    title,
		styles:   styles,
		message:  message,
		yesLabel: yesLabel,
		noLabel:  noLabel,
		danger:   danger,
		yesSel:   !danger,
	}
}

// NewChoice returns a numbered choice list, 1..9. Digits pick and confirm an
// option directly; enter confirms the highlighted one.
func NewChoice(id, title, message string, options []string, styles theme.Styles) Model {
	return Model{
		kind:    KindChoice,
		id:      id,
		title:   title,
		styles:  styles,
		message: message,
		options: append([]string(nil), options...),
	}
}

// ID is the dialog's identifier, echoed back on ResultMsg.
func (m Model) ID() string { return m.id }

func emit(msg tea.Msg) tea.Cmd {
	return func() tea.Msg { return msg }
}

// Update handles a key press.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	switch m.kind {
	case KindInput:
		return m.updateInput(k)
	case KindConfirm:
		return m.updateConfirm(k)
	case KindChoice:
		return m.updateChoice(k)
	}
	return m, nil
}

func (m *Model) recomputeError() {
	if m.validate == nil {
		m.errMsg = ""
		return
	}
	if err := m.validate(m.input.Value()); err != nil {
		m.errMsg = err.Error()
		return
	}
	m.errMsg = ""
}

func (m *Model) refreshSuggestions() {
	if m.suggestFn == nil {
		m.suggestions = nil
		m.sugIndex = -1
		return
	}
	all := m.suggestFn(m.input.Value())
	if len(all) > maxSuggestions {
		all = all[:maxSuggestions]
	}
	m.suggestions = all
	m.sugIndex = -1
	if len(all) > 0 {
		m.sugIndex = 0
	}
}

func (m Model) updateInput(k tea.KeyPressMsg) (Model, tea.Cmd) {
	switch k.String() {
	case "esc":
		return m, emit(ResultMsg{ID: m.id, OK: false})
	case "enter":
		// A highlighted suggestion is what the user is pointing at: enter
		// takes it, as tab would, and then confirms.
		if len(m.suggestions) > 0 && m.sugIndex >= 0 {
			m.input.SetValue(m.suggestions[m.sugIndex])
			m.input.CursorEnd()
		}
		m.recomputeError()
		m.attempted = true
		if m.errMsg != "" {
			return m, nil
		}
		return m, emit(ResultMsg{ID: m.id, OK: true, Value: m.input.Value()})
	case "tab":
		if len(m.suggestions) > 0 && m.sugIndex >= 0 {
			m.input.SetValue(m.suggestions[m.sugIndex])
			m.input.CursorEnd()
			if m.attempted {
				m.recomputeError()
			}
			m.refreshSuggestions()
		}
		return m, nil
	case "down", "ctrl+j":
		if len(m.suggestions) > 0 {
			m.sugIndex = (m.sugIndex + 1) % len(m.suggestions)
		}
		return m, nil
	case "up", "ctrl+k":
		if len(m.suggestions) > 0 {
			m.sugIndex = (m.sugIndex - 1 + len(m.suggestions)) % len(m.suggestions)
		}
		return m, nil
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(k)
	if m.attempted {
		m.recomputeError()
	}
	m.refreshSuggestions()
	return m, cmd
}

func (m Model) updateConfirm(k tea.KeyPressMsg) (Model, tea.Cmd) {
	switch k.String() {
	case "esc":
		return m, emit(ResultMsg{ID: m.id, OK: false})
	case "enter":
		return m, emit(ResultMsg{ID: m.id, OK: m.yesSel})
	case "y":
		return m, emit(ResultMsg{ID: m.id, OK: true})
	case "n":
		return m, emit(ResultMsg{ID: m.id, OK: false})
	case "tab", "shift+tab", "left", "right":
		m.yesSel = !m.yesSel
	}
	return m, nil
}

func digitKey(s string) (int, bool) {
	if len(s) == 1 && s[0] >= '1' && s[0] <= '9' {
		return int(s[0] - '0'), true
	}
	return 0, false
}

func (m Model) updateChoice(k tea.KeyPressMsg) (Model, tea.Cmd) {
	switch k.String() {
	case "esc":
		return m, emit(ResultMsg{ID: m.id, OK: false})
	case "enter":
		return m, emit(ResultMsg{ID: m.id, OK: true, Choice: m.choiceSel})
	case "j", "down":
		if len(m.options) > 0 {
			m.choiceSel = (m.choiceSel + 1) % len(m.options)
		}
	case "k", "up":
		if len(m.options) > 0 {
			m.choiceSel = (m.choiceSel - 1 + len(m.options)) % len(m.options)
		}
	default:
		if n, ok := digitKey(k.String()); ok && n <= len(m.options) {
			return m, emit(ResultMsg{ID: m.id, OK: true, Choice: n - 1})
		}
	}
	return m, nil
}

// contentWidth is the widest line the dialog needs to show without
// truncation.
func (m Model) contentWidth() int {
	lines := []string{m.title}
	switch m.kind {
	case KindInput:
		lines = append(lines, m.input.Placeholder, m.input.Value(), m.errMsg)
		lines = append(lines, m.suggestions...)
	case KindConfirm:
		lines = append(lines, m.message, m.yesLabel+"    "+m.noLabel)
	case KindChoice:
		lines = append(lines, m.message)
		for i, o := range m.options {
			lines = append(lines, fmt.Sprintf("%d. %s", i+1, o))
		}
	}
	w := 0
	for _, l := range lines {
		if lw := lipgloss.Width(l); lw > w {
			w = lw
		}
	}
	return w
}

// Width returns the box's total rendered width (border and padding
// included), between 40 and 64 columns and fitting the content in between.
func (m Model) Width() int {
	w := m.contentWidth() + borderAndPadding
	if w < minWidth {
		w = minWidth
	}
	if w > maxWidth {
		w = maxWidth
	}
	return w
}

func buttonLabel(label string, selected, danger bool, styles theme.Styles) string {
	style := lipgloss.NewStyle()
	if danger {
		style = styles.Error
	}
	text := label
	if selected {
		text = "[ " + label + " ]"
		style = style.Bold(true).Background(styles.Selection.GetBackground())
	}
	return style.Render(text)
}

func (m Model) buttonsRow() string {
	yes := buttonLabel(m.yesLabel, m.yesSel, m.danger, m.styles)
	no := buttonLabel(m.noLabel, !m.yesSel, false, m.styles)
	return yes + "  " + no
}

// renderInputLine draws the value (or placeholder) with the cursor shown as
// a reverse-video cell.
func (m Model) renderInputLine() string {
	cursor := lipgloss.NewStyle().Reverse(true)
	value := m.input.Value()
	pos := m.input.Position()

	if value == "" {
		ph := m.input.Placeholder
		if ph == "" {
			return cursor.Render(" ")
		}
		r := []rune(ph)
		return cursor.Render(string(r[0])) + m.styles.Muted.Render(string(r[1:]))
	}

	r := []rune(value)
	before := string(r[:pos])
	var at, after string
	if pos < len(r) {
		at, after = string(r[pos]), string(r[pos+1:])
	} else {
		at = " "
	}
	return before + cursor.Render(at) + after
}

// View renders the dialog box only (rounded border, accent color; title
// bold). Overlay composites it over the rest of the screen.
func (m Model) View() string {
	outer := m.Width()
	inner := outer - borderAndPadding

	lines := []string{
		m.styles.DialogTitle.Render(textutil.PadLine(m.title, inner)),
		"",
	}

	switch m.kind {
	case KindInput:
		lines = append(lines, textutil.PadLine(m.renderInputLine(), inner))
		if m.errMsg != "" {
			for _, l := range textutil.Wrap(m.errMsg, inner) {
				lines = append(lines, m.styles.Error.Render(textutil.PadLine(l, inner)))
			}
		}
		if m.hint != "" {
			lines = append(lines, "")
			for _, l := range textutil.Wrap(m.hint, inner) {
				lines = append(lines, m.styles.Muted.Render(textutil.PadLine(l, inner)))
			}
		}
		if len(m.suggestions) > 0 {
			lines = append(lines, "")
			for i, s := range m.suggestions {
				row := "  " + s
				if i == m.sugIndex {
					lines = append(lines, m.styles.Selection.Render(textutil.PadLine(row, inner)))
				} else {
					lines = append(lines, m.styles.Muted.Render(textutil.PadLine(row, inner)))
				}
			}
		}
	case KindConfirm:
		for _, l := range textutil.Wrap(m.message, inner) {
			lines = append(lines, textutil.PadLine(l, inner))
		}
		lines = append(lines, "", textutil.PadLine(m.buttonsRow(), inner))
	case KindChoice:
		for _, l := range textutil.Wrap(m.message, inner) {
			lines = append(lines, textutil.PadLine(l, inner))
		}
		lines = append(lines, "")
		for i, opt := range m.options {
			row := strconv.Itoa(i+1) + ". " + opt
			if i == m.choiceSel {
				lines = append(lines, m.styles.Selection.Render(textutil.PadLine("› "+row, inner)))
			} else {
				lines = append(lines, textutil.PadLine("  "+row, inner))
			}
		}
	}

	return m.styles.Dialog.Width(outer).Render(strings.Join(lines, "\n"))
}
