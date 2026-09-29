package app

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/mathieucroset/notty/internal/index"
	"github.com/mathieucroset/notty/internal/meta"
	"github.com/mathieucroset/notty/internal/ui/msgs"
	"github.com/mathieucroset/notty/internal/vault"
)

// indexBuiltMsg carries the index built at startup.
type indexBuiltMsg struct {
	ix  *index.Index
	err error
}

// indexChangedMsg reports that the index was updated off the UI goroutine,
// so the views derived from it (tags, filter, tasks) must be refreshed.
type indexChangedMsg struct{}

// buildIndexCmd indexes the whole vault off the UI goroutine.
func buildIndexCmd(v *vault.Vault) tea.Cmd {
	return func() tea.Msg {
		ix, err := index.Build(v)
		return indexBuiltMsg{ix: ix, err: err}
	}
}

// handleIndexBuilt installs the startup index and warns once about
// per-file problems.
func (m *Model) handleIndexBuilt(msg indexBuiltMsg) tea.Cmd {
	m.indexing = false
	if msg.err != nil {
		return m.pushToast(msgs.ToastError, fmt.Sprintf("Could not index the vault: %v", msg.err))
	}
	m.ix = msg.ix
	m.refreshIndexViews()
	replay := reindexCmd(m.opts.Vault, m.ix, m.pendingIndex)
	m.pendingIndex = nil
	problems := m.ix.Problems()
	switch len(problems) {
	case 0:
		return replay
	case 1:
		return tea.Batch(replay, m.pushToast(msgs.ToastWarn, fmt.Sprintf("1 note has a problem: %v", problems[0])))
	}
	return tea.Batch(replay, m.pushToast(msgs.ToastWarn, fmt.Sprintf("%d notes have problems, first: %v", len(problems), problems[0])))
}

// queueReindex remembers paths changed while the startup index builds (the
// build may have read them before the change), to re-read once it lands.
func (m *Model) queueReindex(paths ...string) {
	if m.ix != nil || !m.indexing {
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
