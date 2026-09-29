// Package tasksview implements Notty's global Tasks view (spec §8 "Tasks
// view"): every task list item across every note, grouped Overdue / Due
// soon / by note, letting the user toggle a task or jump to it.
//
// The view renders only its content; the app's pane renderer draws the
// border and the title (Title). It never touches the vault or the index:
// callers rebuild it from a fresh index.AllTasks() via SetTasks, and every
// action is emitted as a message from package msgs for the app to handle.
package tasksview

import (
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/mathieucroset/notty/internal/index"
	"github.com/mathieucroset/notty/internal/tags"
	"github.com/mathieucroset/notty/internal/ui/msgs"
	"github.com/mathieucroset/notty/internal/ui/theme"
)

// BackMsg asks the app to return focus to the note (esc).
type BackMsg struct{}

// Glyphs for open and done tasks. Plain Unicode, no Nerd Font required.
const (
	glyphOpen = "☐"
	glyphDone = "☑"
)

// row indent, used consistently for headers and tasks.
const indent = "  "

// rowKind is the kind of a flattened row.
type rowKind int

const (
	kindHeader rowKind = iota
	kindBlank
	kindTask
)

// groupKind is which of the three groups a row belongs to.
type groupKind int

const (
	groupOverdue groupKind = iota
	groupDueSoon
	groupNote
)

// item is one flattened row of the tasks view. Every item occupies exactly
// one visual line.
type item struct {
	kind  rowKind
	group groupKind

	header      string // kindHeader
	done, total int    // kindHeader, groupNote only

	ref index.TaskRef // kindTask
}

func (it item) selectable() bool { return it.kind == kindTask }

// id identifies a task row across rebuilds, so the selection survives
// SetTasks calls: it is the (Path, Raw) pair from the spec's API.
func (it item) id() string {
	if it.kind != kindTask {
		return ""
	}
	return it.ref.Path + "\x00" + it.ref.Task.Raw
}

// Model is the Tasks view component.
type Model struct {
	styles      theme.Styles
	dueSoonDays int
	showDone    bool
	width       int
	height      int

	refs  []index.TaskRef
	today time.Time

	items     []item
	cursor    int // index into items, -1 when nothing is selectable
	offset    int // first visible item
	selID     string
	openCount int
}

// New returns an empty Tasks view styled with styles. dueSoonDays is the
// width of the "due soon" window (tasks.due_soon_days); showDone controls
// whether completed tasks are shown (tasks.show_done).
func New(styles theme.Styles, dueSoonDays int, showDone bool) Model {
	m := Model{
		styles:      styles,
		dueSoonDays: dueSoonDays,
		showDone:    showDone,
		cursor:      -1,
	}
	m.rebuild()
	return m
}

// SetStyles re-themes the view (a live theme preview, say).
func (m Model) SetStyles(styles theme.Styles) Model {
	m.styles = styles
	return m
}

// SetTasks rebuilds the groups from refs (typically index.AllTasks()) using
// today as "today" for overdue / due-soon classification (only its calendar
// date matters). The selection stays on the same (Path, Raw) task if it is
// still present; otherwise it moves to the first selectable row.
func (m Model) SetTasks(refs []index.TaskRef, today time.Time) Model {
	m.refs = append([]index.TaskRef(nil), refs...)
	m.today = dateOnly(today)
	m.rebuild()
	return m
}

// SetSize sets the content size (inside the pane border).
func (m Model) SetSize(w, h int) Model {
	m.width, m.height = w, h
	m.ensureVisible()
	return m
}

// SetShowDone shows or hides completed tasks and rebuilds the groups.
func (m Model) SetShowDone(show bool) Model {
	m.showDone = show
	m.rebuild()
	return m
}

// Title returns the pane title, e.g. "Tasks · 12 open".
func (m Model) Title() string {
	return fmt.Sprintf("Tasks · %d open", m.openCount)
}

// dateOnly converts t to local time and truncates it to midnight, so only
// its calendar date matters for comparisons. Both today and due dates
// (themselves parsed in time.Local by tasks.DueDate) go through this, so
// "today" is always read as a local calendar date even when the caller
// passes it in another location (e.g. UTC).
func dateOnly(t time.Time) time.Time {
	t = t.In(time.Local)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.Local)
}

// classify returns which group an open task with due date due belongs to.
// due must be non-nil.
func (m Model) classify(due *time.Time) groupKind {
	d := dateOnly(*due)
	if d.Before(m.today) {
		return groupOverdue
	}
	cutoff := m.today.AddDate(0, 0, m.dueSoonDays)
	if !d.After(cutoff) {
		return groupDueSoon
	}
	return groupNote
}

// noteHeader formats a note group header as "Folder / Title", or just
// "Title" for a note at the vault root.
func noteHeader(p, title string) string {
	dir := path.Dir(p)
	if dir == "." || dir == "" {
		return title
	}
	return dir + " / " + title
}

// displayPath renders a vault-relative note path for display, dropping a
// trailing ".md" (any case). Paths no longer than the extension itself
// (e.g. ".md") are left untouched rather than being truncated to "".
func displayPath(p string) string {
	const ext = ".md"
	if len(p) > len(ext) && strings.HasSuffix(strings.ToLower(p), ext) {
		return p[:len(p)-len(ext)]
	}
	return p
}

// rebuild recomputes items from refs, today, dueSoonDays and showDone,
// keeping the selection on the same task when possible.
func (m *Model) rebuild() {
	prevIdx := m.cursor

	// First pass: per-note done/total across every task (open and done),
	// independent of what is actually displayed.
	noteTotal := map[string]int{}
	noteDone := map[string]int{}
	noteTitle := map[string]string{}
	for _, r := range m.refs {
		noteTotal[r.Path]++
		if r.Task.Done {
			noteDone[r.Path]++
		}
		noteTitle[r.Path] = r.Title
	}

	// Second pass: bucket displayed tasks into Overdue / Due soon / by
	// note, and count every open task for the title.
	var overdue, duesoon []index.TaskRef
	noteTasks := map[string][]index.TaskRef{}
	m.openCount = 0
	for _, r := range m.refs {
		if !r.Task.Done {
			m.openCount++
		}
		if r.Task.Done {
			if !m.showDone {
				continue
			}
			noteTasks[r.Path] = append(noteTasks[r.Path], r)
			continue
		}
		if r.Task.Due == nil {
			noteTasks[r.Path] = append(noteTasks[r.Path], r)
			continue
		}
		switch m.classify(r.Task.Due) {
		case groupOverdue:
			overdue = append(overdue, r)
		case groupDueSoon:
			duesoon = append(duesoon, r)
		default:
			noteTasks[r.Path] = append(noteTasks[r.Path], r)
		}
	}

	sortByDue := func(list []index.TaskRef) {
		sort.SliceStable(list, func(i, j int) bool {
			di, dj := *list[i].Task.Due, *list[j].Task.Due
			if !di.Equal(dj) {
				return di.Before(dj)
			}
			if list[i].Path != list[j].Path {
				return list[i].Path < list[j].Path
			}
			return list[i].Task.Line < list[j].Task.Line
		})
	}
	sortByDue(overdue)
	sortByDue(duesoon)

	var notePaths []string
	for p, list := range noteTasks {
		sort.SliceStable(list, func(i, j int) bool { return list[i].Task.Line < list[j].Task.Line })
		noteTasks[p] = list
		notePaths = append(notePaths, p)
	}
	sort.Slice(notePaths, func(i, j int) bool {
		hi := strings.ToLower(noteHeader(notePaths[i], noteTitle[notePaths[i]]))
		hj := strings.ToLower(noteHeader(notePaths[j], noteTitle[notePaths[j]]))
		if hi != hj {
			return hi < hj
		}
		return notePaths[i] < notePaths[j]
	})

	var items []item
	if len(overdue) > 0 {
		items = append(items, item{kind: kindHeader, group: groupOverdue, header: "OVERDUE"})
		for _, r := range overdue {
			items = append(items, item{kind: kindTask, group: groupOverdue, ref: r})
		}
	}
	if len(duesoon) > 0 {
		if len(items) > 0 {
			items = append(items, item{kind: kindBlank})
		}
		items = append(items, item{kind: kindHeader, group: groupDueSoon, header: "DUE SOON"})
		for _, r := range duesoon {
			items = append(items, item{kind: kindTask, group: groupDueSoon, ref: r})
		}
	}
	for _, p := range notePaths {
		if len(items) > 0 {
			items = append(items, item{kind: kindBlank})
		}
		items = append(items, item{
			kind:   kindHeader,
			group:  groupNote,
			header: noteHeader(p, noteTitle[p]),
			done:   noteDone[p],
			total:  noteTotal[p],
		})
		for _, r := range noteTasks[p] {
			items = append(items, item{kind: kindTask, group: groupNote, ref: r})
		}
	}

	m.items = items
	m.cursor = -1
	if m.selID == "" {
		m.cursor = m.nextSelectable(-1, 1)
		if m.cursor >= 0 {
			m.selID = m.items[m.cursor].id()
		}
		m.ensureVisible()
		return
	}
	if m.selectID(m.selID) {
		return
	}
	// The selected task is gone: stay near its old position.
	if prevIdx >= len(m.items) {
		prevIdx = len(m.items) - 1
	}
	if prevIdx < 0 {
		prevIdx = 0
	}
	if i := m.nextSelectable(prevIdx-1, 1); i >= 0 {
		m.cursor = i
	} else {
		m.cursor = m.nextSelectable(prevIdx+1, -1)
	}
	if m.cursor >= 0 {
		m.selID = m.items[m.cursor].id()
	} else {
		m.selID = ""
	}
	m.ensureVisible()
}

// selectID moves the cursor to the task with the given id.
func (m *Model) selectID(id string) bool {
	for i, it := range m.items {
		if it.kind == kindTask && it.id() == id {
			m.cursor = i
			m.selID = id
			m.ensureVisible()
			return true
		}
	}
	return false
}

// nextSelectable returns the first selectable item after from in direction
// dir (+1 or -1), or -1.
func (m Model) nextSelectable(from, dir int) int {
	for i := from + dir; i >= 0 && i < len(m.items); i += dir {
		if m.items[i].selectable() {
			return i
		}
	}
	return -1
}

// ensureVisible scrolls so the cursor is in view. When nothing selectable
// lies above the cursor, it scrolls to the top so the first header shows.
func (m *Model) ensureVisible() {
	h := max(m.height, 1)
	if m.cursor >= 0 {
		if m.nextSelectable(m.cursor, -1) < 0 && m.cursor < h {
			m.offset = 0
		}
		if m.cursor < m.offset {
			m.offset = m.cursor
		}
		if m.cursor >= m.offset+h {
			m.offset = m.cursor - h + 1
		}
	}
	m.offset = min(m.offset, max(len(m.items)-h, 0))
	m.offset = max(m.offset, 0)
}

func (m Model) selected() (item, bool) {
	if m.cursor < 0 || m.cursor >= len(m.items) {
		return item{}, false
	}
	return m.items[m.cursor], true
}

func (m *Model) move(dir int) {
	if m.cursor < 0 {
		return
	}
	if i := m.nextSelectable(m.cursor, dir); i >= 0 {
		m.cursor = i
		m.selID = m.items[i].id()
	}
	m.ensureVisible()
}

func (m *Model) top() {
	if i := m.nextSelectable(-1, 1); i >= 0 {
		m.cursor = i
		m.selID = m.items[i].id()
	}
	m.ensureVisible()
}

func (m *Model) bottom() {
	if i := m.nextSelectable(len(m.items), -1); i >= 0 {
		m.cursor = i
		m.selID = m.items[i].id()
	}
	m.ensureVisible()
}

func emit(msg tea.Msg) tea.Cmd {
	return func() tea.Msg { return msg }
}

// Update handles key presses (spec §4.4 "Tasks view").
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	switch k.String() {
	case "j", "down":
		m.move(1)
	case "k", "up":
		m.move(-1)
	case "g":
		m.top()
	case "G":
		m.bottom()
	case "space":
		if it, ok := m.selected(); ok {
			return m, emit(msgs.ToggleTaskMsg{Path: it.ref.Path, Line: it.ref.Task.Line, Text: it.ref.Task.Raw})
		}
	case "enter":
		if it, ok := m.selected(); ok {
			return m, emit(msgs.OpenNoteMsg{Path: it.ref.Path, Line: it.ref.Task.Line})
		}
	case "tab":
		return m, emit(msgs.FocusSidebarMsg{})
	case "esc":
		return m, emit(BackMsg{})
	}
	return m, nil
}

// dueTokenRe matches an inline "@YYYY-MM-DD" due date marker.
var dueTokenRe = regexp.MustCompile(`@\d{4}-\d{2}-\d{2}`)

// spaceRunRe collapses the whitespace run left behind after stripping a due
// token.
var spaceRunRe = regexp.MustCompile(`[ \t]{2,}`)

// stripDue removes the "@YYYY-MM-DD" due date token from text, if any, and
// tidies up the resulting whitespace.
func stripDue(text string) string {
	s := dueTokenRe.ReplaceAllString(text, "")
	s = spaceRunRe.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

// dueColor picks the color for a due-date badge by the group its task is
// displayed in.
func (m Model) dueColor(g groupKind) lipgloss.Style {
	switch g {
	case groupOverdue:
		return m.styles.Error
	case groupDueSoon:
		return m.styles.Warning
	default:
		return m.styles.Muted
	}
}

// tagStyle is the inline chip style used for #tags inside task text: like
// Styles.Chip but without padding, so it does not disturb row widths.
func (m Model) tagStyle() lipgloss.Style {
	return m.styles.Chip.Padding(0, 0)
}

// styledLabel renders label (already truncated to fit) with its #tag spans
// colored via st.
func styledLabel(label string, st lipgloss.Style) string {
	ranges := tags.FindTags(label)
	if len(ranges) == 0 {
		return label
	}
	var b strings.Builder
	pos := 0
	for _, r := range ranges {
		if r[0] > pos {
			b.WriteString(label[pos:r[0]])
		}
		b.WriteString(st.Render(label[r[0]:r[1]]))
		pos = r[1]
	}
	if pos < len(label) {
		b.WriteString(label[pos:])
	}
	return b.String()
}

// padLine truncates or pads s (which may contain ANSI codes) to width w.
func padLine(s string, w int) string {
	if sw := ansi.StringWidth(s); sw > w {
		return ansi.Truncate(s, w, "")
	} else if sw < w {
		return s + strings.Repeat(" ", w-sw)
	}
	return s
}

// renderHeader renders a group or note header row.
func (m Model) renderHeader(it item) string {
	switch it.group {
	case groupOverdue:
		return padLine(m.styles.Error.Bold(true).Render(indent+it.header), m.width)
	case groupDueSoon:
		return padLine(m.styles.Warning.Bold(true).Render(indent+it.header), m.width)
	default:
		label := indent + it.header
		count := fmt.Sprintf("%d/%d", it.done, it.total)
		avail := m.width - ansi.StringWidth(count) - 1
		if avail < 0 {
			avail = 0
		}
		label = ansi.Truncate(label, avail, "…")
		gap := m.width - ansi.StringWidth(label) - ansi.StringWidth(count)
		if gap < 1 {
			gap = 1
		}
		line := m.styles.SidebarSection.Render(label) + strings.Repeat(" ", gap) + m.styles.Muted.Render(count)
		return padLine(line, m.width)
	}
}

// renderTask renders a single task row.
func (m Model) renderTask(it item, selected bool) string {
	t := it.ref.Task
	glyph := glyphOpen
	if t.Done {
		glyph = glyphDone
	}
	prefix := indent + glyph + " "
	label := stripDue(t.Text)

	if t.Done {
		avail := max(m.width-ansi.StringWidth(prefix), 0)
		label = ansi.Truncate(label, avail, "…")
		if selected {
			return padLine(m.styles.Selection.Render(padLine(prefix+label, m.width)), m.width)
		}
		return padLine(prefix+m.styles.Muted.Strikethrough(true).Render(label), m.width)
	}

	var suffix, notePath string
	if t.Due != nil {
		suffix = "@" + t.Due.Format("01-02")
	}
	if it.group == groupOverdue || it.group == groupDueSoon {
		notePath = displayPath(it.ref.Path)
	}
	var rightPlain string
	switch {
	case suffix != "" && notePath != "":
		rightPlain = suffix + "  " + notePath
	case suffix != "":
		rightPlain = suffix
	case notePath != "":
		rightPlain = notePath
	}

	avail := m.width - ansi.StringWidth(prefix)
	if rightPlain != "" {
		avail -= 2 + ansi.StringWidth(rightPlain)
	}
	avail = max(avail, 0)
	label = ansi.Truncate(label, avail, "…")

	if selected {
		plain := prefix + label
		if rightPlain != "" {
			gap := m.width - ansi.StringWidth(plain) - ansi.StringWidth(rightPlain)
			if gap < 1 {
				gap = 1
			}
			plain += strings.Repeat(" ", gap) + rightPlain
		}
		return padLine(m.styles.Selection.Render(padLine(plain, m.width)), m.width)
	}

	line := prefix + styledLabel(label, m.tagStyle())
	visible := ansi.StringWidth(prefix + label)
	if rightPlain != "" {
		gap := m.width - visible - ansi.StringWidth(rightPlain)
		if gap < 1 {
			gap = 1
		}
		line += strings.Repeat(" ", gap)
		if suffix != "" {
			line += m.dueColor(it.group).Render(suffix)
			if notePath != "" {
				line += "  " + m.styles.Muted.Render(notePath)
			}
		} else if notePath != "" {
			line += m.styles.Muted.Render(notePath)
		}
	}
	return padLine(line, m.width)
}

// emptyView renders the "no open tasks" placeholder, centered.
func (m Model) emptyView() string {
	const msg = "No open tasks 🎉"
	h := max(m.height, 1)
	out := make([]string, h)
	mid := h / 2
	for i := range out {
		if i == mid {
			pad := max((m.width-ansi.StringWidth(msg))/2, 0)
			plain := strings.Repeat(" ", pad) + msg
			out[i] = padLine(m.styles.Muted.Render(plain), m.width)
		} else {
			out[i] = strings.Repeat(" ", m.width)
		}
	}
	return strings.Join(out, "\n")
}

// View renders the tasks view content at exactly the configured size.
func (m Model) View() string {
	if m.width <= 0 || m.height <= 0 {
		return ""
	}
	if len(m.items) == 0 {
		return m.emptyView()
	}
	h := m.height
	out := make([]string, 0, h)
	for i := m.offset; i < len(m.items) && len(out) < h; i++ {
		it := m.items[i]
		switch it.kind {
		case kindHeader:
			out = append(out, m.renderHeader(it))
		case kindBlank:
			out = append(out, strings.Repeat(" ", m.width))
		case kindTask:
			out = append(out, m.renderTask(it, i == m.cursor))
		}
	}
	for len(out) < h {
		out = append(out, strings.Repeat(" ", m.width))
	}
	return strings.Join(out, "\n")
}
