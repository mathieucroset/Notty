package app

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/mathieucroset/notty/internal/localstate"
	"github.com/mathieucroset/notty/internal/ui/msgs"
	"github.com/mathieucroset/notty/internal/vault"
)

// treeLoadedMsg carries a freshly read vault tree.
type treeLoadedMsg struct {
	root *vault.Node
	err  error
}

// noteLoadedMsg carries a note read from disk, or the read error. seq
// identifies the open request, so a slow read that a newer request has
// superseded is dropped.
type noteLoadedMsg struct {
	seq     int
	path    string
	content string
	// line is the line to put the cursor on, or -1 for the saved cursor.
	line int
	err  error
}

func emit(msg tea.Msg) tea.Cmd {
	return func() tea.Msg { return msg }
}

func errorToast(format string, args ...any) tea.Cmd {
	return emit(msgs.ToastMsg{Level: msgs.ToastError, Text: fmt.Sprintf(format, args...)})
}

// noteReloadedMsg carries the open note re-read after it changed on disk.
type noteReloadedMsg struct {
	path    string
	content string
	err     error
}

// reloadNoteIf re-reads the open note when it is one of paths (or inside
// one of them).
func (m *Model) reloadNoteIf(paths ...string) tea.Cmd {
	if m.opts.Vault == nil || m.note.path == "" {
		return nil
	}
	for _, p := range paths {
		if isUnder(m.note.path, p) {
			v, rel := m.opts.Vault, m.note.path
			return func() tea.Msg {
				content, err := v.Read(rel)
				return noteReloadedMsg{path: rel, content: content, err: err}
			}
		}
	}
	return nil
}

// handleNoteReloaded shows the re-read content of the open note, keeping
// the cursor on its line. A note that could not be read (deleted, say)
// keeps its buffer.
// TODO(external changes): ask before replacing a dirty buffer.
func (m *Model) handleNoteReloaded(msg noteReloadedMsg) tea.Cmd {
	if msg.err != nil || msg.path != m.note.path || msg.path != m.editor.Path() {
		return nil
	}
	if msg.content == m.editor.Content() || m.editor.Dirty() {
		return nil
	}
	m.editor = m.editor.Reload(msg.content)
	m.note.title = vault.Title(msg.content, msg.path)
	m.note.words = len(strings.Fields(msg.content))
	m.sidebar.SetDirty(m.dirtyPath())
	return m.syncPreview()
}

// loadTreeCmd reads the vault tree off the UI goroutine.
func loadTreeCmd(v *vault.Vault) tea.Cmd {
	return func() tea.Msg {
		root, err := v.Tree()
		return treeLoadedMsg{root: root, err: err}
	}
}

// loadNoteCmd reads a note off the UI goroutine.
func loadNoteCmd(v *vault.Vault, path string, line, seq int) tea.Cmd {
	return func() tea.Msg {
		content, err := v.Read(path)
		return noteLoadedMsg{seq: seq, path: path, content: content, line: line, err: err}
	}
}

// saveLocalCmd writes a snapshot of the local state, so the command never
// races with later changes made in Update; snapshots land in order.
func (m *Model) saveLocalCmd() tea.Cmd {
	s, path, saver := m.opts.Local, m.opts.LocalPath, m.localSaver
	if s == nil || path == "" {
		return nil
	}
	snap := &localstate.State{
		Recents:  append([]string{}, s.Recents...),
		LastNote: s.LastNote,
		Cursor:   make(map[string][2]int, len(s.Cursor)),
		Expanded: append([]string{}, s.Expanded...),
	}
	for k, v := range s.Cursor {
		snap.Cursor[k] = v
	}
	seq := saver.ticket()
	return func() tea.Msg {
		if err := saver.save(seq, func() error { return snap.Save(path) }); err != nil {
			return msgs.ToastMsg{Level: msgs.ToastWarn, Text: fmt.Sprintf("Could not save local state: %v", err)}
		}
		return nil
	}
}
