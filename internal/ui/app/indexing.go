package app

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/mathieucroset/notty/internal/index"
	"github.com/mathieucroset/notty/internal/meta"
	"github.com/mathieucroset/notty/internal/ui/msgs"
	"github.com/mathieucroset/notty/internal/vault"
)

// indexBuiltMsg carries a freshly built index: the startup one, or a
// rebuild (gen > 0) after the watcher lost events.
type indexBuiltMsg struct {
	ix  *index.Index
	err error
	gen int
}

// indexChangedMsg reports that the index was updated off the UI goroutine,
// so the views derived from it (tags, filter, tasks) must be refreshed.
type indexChangedMsg struct{}

// buildIndexCmd indexes the whole vault off the UI goroutine; gen numbers
// the build (0 at startup).
func buildIndexCmd(v *vault.Vault, gen int) tea.Cmd {
	return func() tea.Msg {
		ix, err := index.Build(v)
		return indexBuiltMsg{ix: ix, err: err, gen: gen}
	}
}

// goneCheckMsg asks to forget the listed notes that no longer exist; it
// waits while a sync merge rewrites the vault (see isMutation).
type goneCheckMsg struct{ paths []string }

// rebuildIndex indexes the whole vault again, after the watcher lost
// events. The current index serves until the new one lands; the changes
// seen meanwhile are replayed on it. While a build (the startup one or a
// rebuild) runs, it only asks for one more once that build lands: the
// running build may have read files before the events were lost.
func (m *Model) rebuildIndex() tea.Cmd {
	if m.opts.Vault == nil {
		return nil
	}
	if m.indexing {
		m.rebuildAgain = true
		return nil
	}
	m.indexGen++
	m.indexing = true
	return buildIndexCmd(m.opts.Vault, m.indexGen)
}

// handleIndexBuilt installs a freshly built index and replays the changes
// seen while it was built. It warns once, at startup, about per-file
// problems; after a rebuild it forgets the notes that are gone. A failed
// rebuild keeps the current index. A rebuild asked for meanwhile starts
// now.
func (m *Model) handleIndexBuilt(msg indexBuiltMsg) tea.Cmd {
	if msg.gen != m.indexGen {
		return nil // not the build in flight
	}
	m.indexing = false
	cmd := m.installIndex(msg)
	if m.rebuildAgain {
		m.rebuildAgain = false
		cmd = tea.Batch(cmd, m.rebuildIndex())
	}
	return cmd
}

// installIndex installs a built index, or reports why it failed.
func (m *Model) installIndex(msg indexBuiltMsg) tea.Cmd {
	if msg.err != nil {
		if msg.gen > 0 {
			m.pendingIndex = nil // the current index was kept up to date
			return m.pushToast(msgs.ToastError, fmt.Sprintf("Could not index the vault again: %v", msg.err))
		}
		return m.pushToast(msgs.ToastError, fmt.Sprintf("Could not index the vault: %v", msg.err))
	}
	old := m.ix
	m.ix = msg.ix
	if p := m.editor.Path(); p != "" && m.editor.Dirty() {
		m.ix.UpdateContent(p, m.editor.Content()) // the index follows the buffer
	}
	m.refreshIndexViews()
	replay := reindexCmd(m.opts.Vault, m.ix, m.pendingIndex)
	m.pendingIndex = nil
	if msg.gen > 0 {
		return tea.Batch(replay, emit(goneCheckMsg{paths: droppedNotes(old, m.ix)}))
	}
	problems := m.ix.Problems()
	switch len(problems) {
	case 0:
		return replay
	case 1:
		return tea.Batch(replay, m.pushToast(msgs.ToastWarn, fmt.Sprintf("1 note has a problem: %v", problems[0])))
	}
	return tea.Batch(replay, m.pushToast(msgs.ToastWarn, fmt.Sprintf("%d notes have problems, first: %v", len(problems), problems[0])))
}

// droppedNotes lists the notes of old that are missing from ix.
func droppedNotes(old, ix *index.Index) []string {
	if old == nil {
		return nil
	}
	var gone []string
	for _, n := range old.Notes() {
		if _, ok := ix.Get(n.Path); !ok {
			gone = append(gone, n.Path)
		}
	}
	return gone
}

// queueReindex remembers paths changed while the index builds (the build
// may have read them before the change), to re-read once it lands.
func (m *Model) queueReindex(paths ...string) {
	if !m.indexing {
		return
	}
	for _, p := range paths {
		if p != "" {
			m.pendingIndex = append(m.pendingIndex, p)
		}
	}
}

// refreshIndexViews recomputes everything the sidebar shows from the index.
func (m *Model) refreshIndexViews() {
	if m.ix == nil {
		return
	}
	counts := m.ix.TagCounts()
	tags := make([]msgs.TagCount, len(counts))
	for i, c := range counts {
		tags[i] = msgs.TagCount{Tag: c.Tag, Count: c.Count}
	}
	m.sidebar.SetTags(tags)
	m.applyFilter()
	m.refreshTasks()
}

// applyFilter limits the sidebar tree to the notes carrying the active tag
// filter, or shows everything when there is none.
func (m *Model) applyFilter() {
	if m.filterTag == "" || m.ix == nil {
		m.sidebar.SetFilter(nil)
		return
	}
	set := map[string]bool{}
	for _, p := range m.ix.NotesWithTag(m.filterTag) {
		set[p] = true
	}
	m.sidebar.SetFilter(set)
}

// togglePin pins or unpins p and saves the pins.
func (m *Model) togglePin(p string) tea.Cmd {
	if cmd, refused := m.refuseConflicted(p); refused {
		return cmd
	}
	m.opts.Pins.Toggle(p)
	m.sidebar.SetPins(m.opts.Pins.Pins)
	m.noteChanged(pinsPath)
	return m.savePinsCmd()
}

// savePinsCmd writes a snapshot of the pins off the UI goroutine.
func (m *Model) savePinsCmd() tea.Cmd {
	if m.opts.Vault == nil {
		return nil
	}
	snap := &meta.State{Pins: append([]string{}, m.opts.Pins.Pins...)}
	root, saver := m.opts.Vault.Root, m.pinsSaver
	seq := saver.ticket()
	return func() tea.Msg {
		if err := saver.save(seq, func() error { return snap.Save(root) }); err != nil {
			return msgs.ToastMsg{Level: msgs.ToastError, Text: fmt.Sprintf("Could not save pins: %v", err)}
		}
		return nil
	}
}
