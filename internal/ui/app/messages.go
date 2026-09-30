package app

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/mathieucroset/notty/internal/localstate"
	"github.com/mathieucroset/notty/internal/ui/dialog"
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
// force replaces even a dirty buffer ("Reload from disk").
type noteReloadedMsg struct {
	path    string
	content string
	err     error
	force   bool
}

// dlgExternalChange asks what to do when the open note changed on disk
// while its buffer has unsaved edits (spec §9).
const dlgExternalChange = "external-change"

// External change choices, in dialog order: keeping the edits is the
// default.
const (
	choiceKeepMine = iota
	choiceReload
)

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
// keeps its buffer. When the buffer has unsaved edits a dialog asks
// whether to reload from disk or keep them (spec §9).
func (m *Model) handleNoteReloaded(msg noteReloadedMsg) tea.Cmd {
	if msg.force && msg.path == m.extConflict {
		m.extConflict = "" // the "Reload from disk" answer arrived
		if msg.err != nil {
			return m.pushToast(msgs.ToastError, fmt.Sprintf("Could not reload %s: %v", msg.path, msg.err))
		}
	}
	if msg.err != nil || msg.path != m.note.path || msg.path != m.editor.Path() {
		return nil
	}
	if !msg.force {
		if msg.content == m.baseline {
			return nil // unchanged since Notty last read or wrote it
		}
		if m.editor.Dirty() && msg.content != m.editor.Content() {
			return m.askExternalChange(msg.path, msg.content)
		}
	}
	m.editor = m.editor.Reload(msg.content)
	m.setBaseline(msg.content)
	m.note.title = vault.Title(msg.content, msg.path)
	m.note.words = len(strings.Fields(msg.content))
	m.sidebar.SetDirty(m.dirtyPath())
	return m.syncPreview()
}

// setBaseline records content as read from the open note's file.
func (m *Model) setBaseline(content string) {
	m.baseline = content
	m.baselineGen++
}

// askExternalChange opens the "changed on disk" dialog for the open note,
// whose file now holds disk, and holds its saves until the user chooses.
func (m *Model) askExternalChange(p, disk string) tea.Cmd {
	m.extDisk = disk
	if m.extConflict == p {
		if o := m.topOverlay(); o != nil && o.kind == overlayDialog && o.dialog.ID() == dlgExternalChange {
			return nil // already asking; the choice re-reads the file
		}
	}
	m.extConflict = p
	d := dialog.NewChoice(dlgExternalChange, "Changed on disk",
		"'"+displayName(p)+"' changed on disk, and you have unsaved edits.",
		[]string{"Keep mine", "Reload from disk"}, m.opts.Styles)
	// Stacked on top: whatever was open (a rename dialog, the finder)
	// comes back once this is answered.
	m.pushOverlay(&overlayState{kind: overlayDialog, dialog: d, pending: pendingOp{kind: opExternalChange, path: p}})
	return nil
}

// resolveExternalChange acts on the dialog: reload the file into the
// buffer, or keep the buffer, which then overwrites the file as the dialog
// saw it (a file changed again since asks again).
func (m *Model) resolveExternalChange(p string, choice int) tea.Cmd {
	if choice == choiceReload && m.opts.Vault != nil && p == m.editor.Path() {
		v := m.opts.Vault
		return func() tea.Msg {
			content, err := v.Read(p)
			return noteReloadedMsg{path: p, content: content, err: err, force: true}
		}
	}
	m.extConflict = ""
	if p != m.editor.Path() {
		return nil
	}
	m.setBaseline(m.extDisk)
	return m.saveRequest()
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
