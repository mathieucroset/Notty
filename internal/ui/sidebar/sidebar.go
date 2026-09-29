// Package sidebar implements Notty's sidebar: pinned notes, the folder tree,
// tag chips, and the Tasks, Conflicts, and Trash entries (spec §4.1, §4.4).
//
// The sidebar renders only its content; the app draws the pane border
// around it. It never touches the vault: every action is emitted as a
// message from package msgs for the app to handle.
package sidebar

import (
	"maps"
	"path"
	"sort"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/mathieucroset/notty/internal/ui/msgs"
	"github.com/mathieucroset/notty/internal/ui/theme"
	"github.com/mathieucroset/notty/internal/vault"
)

// Glyphs are the symbols the sidebar draws. They are plain Unicode so they
// render without a Nerd Font.
type Glyphs struct {
	FolderOpen, FolderClosed string
	Note, File, Pin          string
	Tasks, Conflicts, Trash  string
	Dirty                    string
}

// DefaultGlyphs is the default glyph set.
var DefaultGlyphs = Glyphs{
	FolderOpen:   "▾",
	FolderClosed: "▸",
	Note:         "•",
	File:         "·",
	Pin:          "★",
	Tasks:        "☐",
	Conflicts:    "⚠",
	Trash:        "⌫",
	Dirty:        "●",
}

type itemKind int

const (
	kindHeader itemKind = iota
	kindBlank
	kindHint
	kindPin
	kindNode
	kindTag
	kindEntry
)

// item is one element of the flattened sidebar. Every item occupies its own
// line except tag chips, which flow several to a line.
type item struct {
	kind  itemKind
	text  string // header or hint text
	path  string // pin and node path
	node  *vault.Node
	depth int
	tag   msgs.TagCount
	entry msgs.Entry
}

func (it item) selectable() bool {
	switch it.kind {
	case kindPin, kindNode, kindTag, kindEntry:
		return true
	}
	return false
}

// id identifies an item across rebuilds, so the selection survives changes.
func (it item) id() string {
	switch it.kind {
	case kindPin:
		return "pin:" + it.path
	case kindNode:
		return "node:" + it.path
	case kindTag:
		return "tag:" + it.tag.Tag
	case kindEntry:
		return "entry:" + entryName(it.entry)
	}
	return ""
}

func entryName(e msgs.Entry) string {
	switch e {
	case msgs.EntryTasks:
		return "tasks"
	case msgs.EntryConflicts:
		return "conflicts"
	default:
		return "trash"
	}
}

// Model is the sidebar component.
type Model struct {
	styles theme.Styles
	glyphs Glyphs

	root                    *vault.Node
	pins                    []string
	tags                    []msgs.TagCount
	tasks, conflicts, trash int
	dirty                   string
	filter                  map[string]bool
	expanded                map[string]bool // persisted expanded folders
	filterOpen              map[string]bool // folders opened only to reveal filter matches
	focused                 bool
	width, height           int

	items  []item
	lines  [][]int // item indexes on each visual line
	lineOf []int   // visual line of each item
	cursor int     // selected item index, -1 when nothing is selectable
	selID  string  // id of the explicitly chosen item, "" before any choice
	offset int     // first visible line

	picker *tagPicker
}

// New returns an empty sidebar styled with styles.
func New(styles theme.Styles) Model {
	m := Model{
		styles:   styles,
		glyphs:   DefaultGlyphs,
		expanded: map[string]bool{},
		cursor:   -1,
	}
	m.rebuild()
	return m
}

// SetTree replaces the folder tree.
func (m *Model) SetTree(root *vault.Node) {
	m.root = root
	m.rebuild()
}

// SetPins sets the pinned note paths, in pin order.
func (m *Model) SetPins(pins []string) {
	m.pins = append([]string(nil), pins...)
	m.rebuild()
}

// SetTags sets the tag chips. The TAGS section is hidden when empty.
func (m *Model) SetTags(tags []msgs.TagCount) {
	m.tags = append([]msgs.TagCount(nil), tags...)
	if m.picker != nil {
		m.picker.refilter(m.tags)
	}
	m.rebuild()
}

// SetCounts sets the counts shown on the Tasks, Conflicts, and Trash
// entries. The Conflicts entry is shown only when conflicts > 0.
func (m *Model) SetCounts(tasks, conflicts, trash int) {
	m.tasks, m.conflicts, m.trash = tasks, conflicts, trash
	m.rebuild()
}

// SetDirty marks the note at path as having unsaved changes ("" for none).
func (m *Model) SetDirty(path string) { m.dirty = path }

// SetFilter limits the tree to the note paths in paths; folders containing
// a match stay visible and are shown expanded to reveal it. That expansion
// is display-only: Expanded does not report it and clearing the filter
// (nil) restores the previous tree.
func (m *Model) SetFilter(paths map[string]bool) {
	m.filter = paths
	m.filterOpen = nil
	if paths != nil {
		m.filterOpen = map[string]bool{}
		for p := range paths {
			for d := parentDir(p); d != ""; d = parentDir(d) {
				m.filterOpen[d] = true
			}
		}
	}
	m.rebuild()
}

// SetExpanded sets which folders are expanded.
func (m *Model) SetExpanded(paths []string) {
	m.expanded = make(map[string]bool, len(paths))
	for _, p := range paths {
		m.expanded[p] = true
	}
	m.rebuild()
}

// Expanded returns the expanded folders, sorted.
func (m Model) Expanded() []string {
	out := make([]string, 0, len(m.expanded))
	for p := range m.expanded {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// Select moves the cursor to the tree row for path, expanding its ancestor
// folders so it is visible. It falls back to a pin with that path. When the
// cursor is already on a pin or row for path, it stays there.
func (m *Model) Select(p string) {
	if it, ok := m.selected(); ok && (it.kind == kindPin || it.kind == kindNode) && it.path == p {
		m.ensureVisible()
		return
	}
	m.expandAncestors(p)
	m.rebuild()
	if !m.selectID("node:" + p) {
		m.selectID("pin:" + p)
	}
}

// SetFocused sets whether the sidebar has keyboard focus.
func (m *Model) SetFocused(f bool) { m.focused = f }

// SetSize sets the content size (inside the pane border).
func (m *Model) SetSize(w, h int) {
	m.width, m.height = w, h
	m.rebuild()
}

// PickerOpen reports whether the inline tag picker is open. While it is,
// the sidebar behaves like an overlay for key routing.
func (m Model) PickerOpen() bool { return m.picker != nil }

// cloneSet copies a set, never returning nil.
func cloneSet(s map[string]bool) map[string]bool {
	if s == nil {
		return map[string]bool{}
	}
	return maps.Clone(s)
}

// isOpen reports whether folder p is shown expanded.
func (m Model) isOpen(p string) bool { return m.expanded[p] || m.filterOpen[p] }

// expandAncestors expands every folder above p. Like every write to the
// expanded set it copies the map first, so earlier Model values (Update has
// a value receiver) never see the change.
func (m *Model) expandAncestors(p string) {
	m.expanded = cloneSet(m.expanded)
	for d := parentDir(p); d != ""; d = parentDir(d) {
		m.expanded[d] = true
	}
}

// selectID moves the cursor to the item with the given id.
func (m *Model) selectID(id string) bool {
	for i, it := range m.items {
		if it.selectable() && it.id() == id {
			m.cursor = i
			m.selID = id
			m.ensureVisible()
			return true
		}
	}
	return false
}

func parentDir(p string) string {
	if d := path.Dir(p); d != "." && d != "/" {
		return d
	}
	return ""
}

// rebuild flattens the sections into items and lays them out into lines,
// keeping the selection on the same item when it still exists.
func (m *Model) rebuild() {
	prevIdx := m.cursor

	var items []item
	section := func(title string) {
		if len(items) > 0 {
			items = append(items, item{kind: kindBlank})
		}
		items = append(items, item{kind: kindHeader, text: title})
	}

	if len(m.pins) > 0 {
		section("PINNED")
		for _, p := range m.pins {
			items = append(items, item{kind: kindPin, path: p})
		}
	}

	section("NOTES")
	nTree := len(items)
	if m.root != nil {
		visible := map[*vault.Node]bool{}
		m.markVisible(m.root, visible)
		m.walk(m.root.Children, 0, visible, &items)
	}
	if len(items) == nTree {
		hint := "No notes yet — press n"
		if m.filter != nil {
			hint = "No matching notes"
		}
		items = append(items, item{kind: kindHint, text: hint})
	}

	if len(m.tags) > 0 {
		section("TAGS")
		for _, t := range m.tags {
			items = append(items, item{kind: kindTag, tag: t})
		}
	}

	items = append(items, item{kind: kindBlank})
	items = append(items, item{kind: kindEntry, entry: msgs.EntryTasks})
	if m.conflicts > 0 {
		items = append(items, item{kind: kindEntry, entry: msgs.EntryConflicts})
	}
	items = append(items, item{kind: kindEntry, entry: msgs.EntryTrash})

	m.items = items
	m.layout()

	m.cursor = -1
	if m.selID == "" {
		// Nothing chosen yet: start on the first selectable row.
		m.cursor = m.nextSelectable(-1, 1)
		m.ensureVisible()
		return
	}
	if m.selectID(m.selID) {
		return
	}
	// The selected item is gone: stay near its old position.
	defer func() {
		if m.cursor >= 0 {
			m.selID = m.items[m.cursor].id()
		}
	}()
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
	m.ensureVisible()
}

// markVisible records which nodes pass the filter: notes in the filter set
// and folders containing one. Without a filter everything is visible.
func (m *Model) markVisible(n *vault.Node, visible map[*vault.Node]bool) bool {
	var v bool
	switch {
	case n.IsDir:
		for _, c := range n.Children {
			if m.markVisible(c, visible) {
				v = true
			}
		}
		if m.filter == nil {
			v = true
		}
	case m.filter == nil:
		v = true
	default:
		v = n.IsNote && m.filter[n.Path]
	}
	visible[n] = v
	return v
}

func (m *Model) walk(nodes []*vault.Node, depth int, visible map[*vault.Node]bool, items *[]item) {
	for _, n := range nodes {
		if !visible[n] {
			continue
		}
		*items = append(*items, item{kind: kindNode, path: n.Path, node: n, depth: depth})
		if n.IsDir && m.isOpen(n.Path) {
			m.walk(n.Children, depth+1, visible, items)
		}
	}
}

// chipIndent starts each line of tag chips.
const chipIndent = "  "

// chipText is a tag chip's text without padding.
func chipText(t msgs.TagCount) string {
	return "#" + t.Tag + " " + strconv.Itoa(t.Count)
}

// layout assigns items to visual lines. Tag chips flow left to right and
// wrap at the sidebar width; every other item takes a whole line.
func (m *Model) layout() {
	m.lines = nil
	m.lineOf = make([]int, len(m.items))
	lineW := 0
	for i, it := range m.items {
		if it.kind == kindTag {
			w := ansi.StringWidth(chipText(it.tag)) + 2 // chip padding
			prevTag := i > 0 && m.items[i-1].kind == kindTag
			if prevTag && lineW+1+w <= m.width {
				last := len(m.lines) - 1
				m.lines[last] = append(m.lines[last], i)
				m.lineOf[i] = last
				lineW += 1 + w
				continue
			}
			lineW = len(chipIndent) + w
		}
		m.lines = append(m.lines, []int{i})
		m.lineOf[i] = len(m.lines) - 1
	}
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

func (m Model) listHeight() int {
	h := m.height
	if m.picker != nil {
		h -= m.picker.height()
	}
	return max(h, 1)
}

// ensureVisible scrolls so the cursor's line is in view. When nothing
// selectable lies above the cursor, it scrolls to the top so the first
// section header shows.
func (m *Model) ensureVisible() {
	h := m.listHeight()
	if m.cursor >= 0 {
		line := m.lineOf[m.cursor]
		if m.nextSelectable(m.cursor, -1) < 0 && line < h {
			m.offset = 0
		}
		if line < m.offset {
			m.offset = line
		}
		if line >= m.offset+h {
			m.offset = line - h + 1
		}
	}
	m.offset = min(m.offset, max(len(m.lines)-h, 0))
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
		m.selectID(m.items[i].id())
	}
	m.ensureVisible()
}

func (m *Model) toggle(p string) {
	m.expanded = cloneSet(m.expanded)
	if m.isOpen(p) {
		delete(m.expanded, p)
		if m.filterOpen[p] {
			m.filterOpen = cloneSet(m.filterOpen)
			delete(m.filterOpen, p)
		}
	} else {
		m.expanded[p] = true
	}
	m.rebuild()
}

// selectionFolder is the folder a new note or folder goes into: the
// selected folder, or the folder containing the selected note.
func (m Model) selectionFolder() string {
	it, ok := m.selected()
	if !ok {
		return ""
	}
	switch it.kind {
	case kindNode:
		if it.node.IsDir {
			return it.path
		}
		return parentDir(it.path)
	case kindPin:
		return parentDir(it.path)
	}
	return ""
}

// selectionPath returns the vault path of the selected tree row or pin.
// notesOnly restricts it to notes.
func (m Model) selectionPath(notesOnly bool) (string, bool) {
	it, ok := m.selected()
	if !ok {
		return "", false
	}
	switch it.kind {
	case kindPin:
		return it.path, true
	case kindNode:
		if notesOnly && !it.node.IsNote {
			return "", false
		}
		return it.path, true
	}
	return "", false
}

func emit(msg tea.Msg) tea.Cmd {
	return func() tea.Msg { return msg }
}

// Update handles key presses.
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	k, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	if m.picker != nil {
		return m.updatePicker(k)
	}

	switch k.String() {
	case "j", "down":
		m.move(1)
	case "k", "up":
		m.move(-1)
	case "g", "home":
		if i := m.nextSelectable(-1, 1); i >= 0 {
			m.selectID(m.items[i].id())
			m.ensureVisible()
		}
	case "G", "end":
		if i := m.nextSelectable(len(m.items), -1); i >= 0 {
			m.selectID(m.items[i].id())
			m.ensureVisible()
		}
	case "h", "left":
		it, ok := m.selected()
		if !ok || it.kind != kindNode {
			break
		}
		if it.node.IsDir && m.isOpen(it.path) {
			m.toggle(it.path)
		} else if parent := parentDir(it.path); parent != "" {
			m.selectID("node:" + parent)
		}
	case "l", "right":
		if it, ok := m.selected(); ok && it.kind == kindNode && it.node.IsDir && !m.isOpen(it.path) {
			m.toggle(it.path)
		}
	case "enter":
		return m, m.activate()
	case "tab":
		return m, emit(msgs.FocusMainMsg{})
	case "n":
		return m, emit(msgs.RequestNewNote{Folder: m.selectionFolder()})
	case "N":
		return m, emit(msgs.RequestNewFolder{Parent: m.selectionFolder()})
	case "r":
		if p, ok := m.selectionPath(false); ok {
			return m, emit(msgs.RequestRename{Path: p})
		}
	case "m":
		if p, ok := m.selectionPath(false); ok {
			return m, emit(msgs.RequestMove{Path: p})
		}
	case "d":
		if p, ok := m.selectionPath(false); ok {
			return m, emit(msgs.RequestTrash{Path: p})
		}
	case "p":
		if p, ok := m.selectionPath(true); ok {
			return m, emit(msgs.TogglePinMsg{Path: p})
		}
	case "H":
		if p, ok := m.selectionPath(true); ok {
			return m, emit(msgs.OpenHistoryMsg{Path: p})
		}
	case "#":
		m.picker = newTagPicker(m.tags)
		m.ensureVisible()
	case "esc":
		return m, emit(msgs.ClearFilterMsg{})
	case "c":
		return m, emit(msgs.OpenResolverMsg{})
	case "!":
		return m, emit(msgs.OpenErrorLogMsg{})
	case "?":
		return m, emit(msgs.OpenHelpMsg{})
	case "q":
		return m, emit(msgs.QuitMsg{})
	}
	return m, nil
}

// activate handles enter on the selected item.
func (m *Model) activate() tea.Cmd {
	it, ok := m.selected()
	if !ok {
		return nil
	}
	switch it.kind {
	case kindPin:
		return emit(msgs.OpenNoteMsg{Path: it.path, Line: -1})
	case kindNode:
		switch {
		case it.node.IsDir:
			m.toggle(it.path)
			return nil
		case it.node.IsNote:
			return emit(msgs.OpenNoteMsg{Path: it.path, Line: -1})
		default:
			return emit(msgs.OpenFileExternalMsg{Path: it.path})
		}
	case kindTag:
		return emit(msgs.FilterTagMsg{Tag: it.tag.Tag})
	case kindEntry:
		return emit(msgs.ActivateEntryMsg{Entry: it.entry})
	}
	return nil
}

func (m Model) updatePicker(k tea.KeyPressMsg) (Model, tea.Cmd) {
	cp := *m.picker // copy, so earlier Model values keep their picker state
	p := &cp
	m.picker = p
	switch k.String() {
	case "esc":
		m.picker = nil
	case "enter":
		m.picker = nil
		if t, ok := p.current(); ok {
			m.ensureVisible()
			return m, emit(msgs.FilterTagMsg{Tag: t})
		}
	case "down", "ctrl+j", "ctrl+n":
		p.move(1)
	case "up", "ctrl+k", "ctrl+p":
		p.move(-1)
	case "backspace":
		r := []rune(p.query)
		if len(r) == 0 {
			m.picker = nil // backspace on an empty query closes the picker
			break
		}
		p.query = string(r[:len(r)-1])
		p.refilter(m.tags)
	default:
		// Tags never contain spaces, so they are ignored.
		if text := strings.ReplaceAll(k.Text, " ", ""); text != "" {
			p.query += text
			p.refilter(m.tags)
		}
	}
	m.ensureVisible()
	return m, nil
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

func noteName(name string) string {
	if strings.HasSuffix(strings.ToLower(name), ".md") {
		return name[:len(name)-3]
	}
	return name
}

func (m Model) selStyle() lipgloss.Style {
	if m.focused {
		return m.styles.SidebarSelectedFocused
	}
	return m.styles.SidebarSelected
}

// row renders a single-item line. When selected, the whole row is drawn
// in the selection style.
func (m Model) row(it item, selected bool) string {
	var glyph, label, suffix string
	glyphStyle, labelStyle := m.styles.Muted, m.styles.SidebarItem
	indent := "  "

	switch it.kind {
	case kindHeader:
		s := m.styles.SidebarSection.Render("  " + it.text)
		if it.text == "NOTES" && m.filter != nil {
			s += m.styles.Muted.Render(" · filtered")
		}
		return padLine(s, m.width)
	case kindBlank:
		return strings.Repeat(" ", m.width)
	case kindHint:
		return padLine(m.styles.Muted.Render("    "+it.text), m.width)
	case kindPin:
		glyph, label = m.glyphs.Pin, noteName(path.Base(it.path))
		glyphStyle = m.styles.Accent
	case kindNode:
		indent += strings.Repeat("  ", it.depth)
		n := it.node
		switch {
		case n.IsDir:
			glyph = m.glyphs.FolderClosed
			if m.isOpen(n.Path) {
				glyph = m.glyphs.FolderOpen
			}
			glyphStyle, label = m.styles.Accent, n.Name
		case n.IsNote:
			glyph, label = m.glyphs.Note, noteName(n.Name)
		default:
			glyph, label = m.glyphs.File, n.Name
			labelStyle = m.styles.SidebarDim
		}
	case kindEntry:
		switch it.entry {
		case msgs.EntryTasks:
			glyph, label = m.glyphs.Tasks, "Tasks ("+strconv.Itoa(m.tasks)+")"
		case msgs.EntryConflicts:
			glyph, label = m.glyphs.Conflicts, "Conflicts ("+strconv.Itoa(m.conflicts)+")"
			glyphStyle, labelStyle = m.styles.Warning, m.styles.Warning
		case msgs.EntryTrash:
			glyph, label = m.glyphs.Trash, "Trash ("+strconv.Itoa(m.trash)+")"
		}
	}
	if (it.kind == kindPin || it.kind == kindNode) && it.path == m.dirty && m.dirty != "" {
		suffix = " " + m.glyphs.Dirty
	}

	prefix := indent + glyph + " "
	avail := m.width - ansi.StringWidth(prefix) - ansi.StringWidth(suffix)
	if avail < 1 {
		return padLine(prefix+label, m.width)
	}
	label = ansi.Truncate(label, avail, "…")

	if selected {
		return m.selStyle().Render(padLine(prefix+label+suffix, m.width))
	}
	s := indent + glyphStyle.Render(glyph) + " " + labelStyle.Render(label)
	if suffix != "" {
		s += m.styles.Accent.Render(suffix)
	}
	return padLine(s, m.width)
}

// chipLine renders a line of tag chips.
func (m Model) chipLine(idx []int) string {
	parts := make([]string, 0, len(idx))
	for _, i := range idx {
		st := m.styles.Chip
		if i == m.cursor {
			st = m.selStyle().Padding(0, 1)
		}
		parts = append(parts, st.Render(chipText(m.items[i].tag)))
	}
	return padLine(chipIndent+strings.Join(parts, " "), m.width)
}

// View renders the sidebar content at exactly the configured size.
func (m Model) View() string {
	if m.width <= 0 || m.height <= 0 {
		return ""
	}
	h := m.listHeight()
	out := make([]string, 0, m.height)
	for l := m.offset; l < len(m.lines) && len(out) < h; l++ {
		idx := m.lines[l]
		if m.items[idx[0]].kind == kindTag {
			out = append(out, m.chipLine(idx))
			continue
		}
		out = append(out, m.row(m.items[idx[0]], idx[0] == m.cursor))
	}
	for len(out) < h {
		out = append(out, strings.Repeat(" ", m.width))
	}
	if m.picker != nil {
		out = append(out, m.picker.view(m)...)
	}
	if len(out) > m.height {
		out = out[len(out)-m.height:]
	}
	return strings.Join(out, "\n")
}
