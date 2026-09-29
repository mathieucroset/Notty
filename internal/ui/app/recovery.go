package app

// Crash recovery (spec §9): the app keeps Options.Snapshot holding the open
// buffer, which main saves to .notty/recovery/ after a panic.

// bufferMark identifies a buffer state: the snapshot's text is refreshed
// only when it changes, so messages that leave the buffer alone cost no
// copy of the text.
type bufferMark struct {
	root, path string
	version    uint64
	dirty      bool
}

// recordBuffer copies the open buffer to the recovery snapshot when it
// changed since the last copy.
func (m *Model) recordBuffer() {
	s := m.opts.Snapshot
	if s == nil {
		return
	}
	mark := bufferMark{root: vaultRoot(m.opts), path: m.editor.Path(), version: m.editor.Version(), dirty: m.editor.Dirty()}
	if mark == m.recorded {
		return
	}
	m.recorded = mark
	s.Set(mark.root, mark.path, m.editor.Content(), mark.dirty)
}
