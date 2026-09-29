package app

import (
	"fmt"
	"path"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/mathieucroset/notty/internal/gitsync"
	"github.com/mathieucroset/notty/internal/ui/history"
	"github.com/mathieucroset/notty/internal/ui/msgs"
	"github.com/mathieucroset/notty/internal/vault"
)

// historyLoadedMsg carries a note's git history.
type historyLoadedMsg struct {
	path    string
	current string
	entries []gitsync.LogEntry
	noGit   bool
	err     error
}

// historyVersionMsg carries a note's content at one revision.
type historyVersionMsg struct {
	rev     string
	content string
	err     error
}

// openHistory loads the git history of the note at p (spec §8).
func (m *Model) openHistory(p string) tea.Cmd {
	if m.opts.Vault == nil || p == "" {
		return nil
	}
	if !strings.EqualFold(path.Ext(p), ".md") {
		return m.pushToast(msgs.ToastInfo, "History is only available for notes")
	}
	v, current, open := m.opts.Vault, m.editor.Content(), m.note.path == p
	return func() tea.Msg {
		if !gitsync.Available() {
			return historyLoadedMsg{path: p, noGit: true}
		}
		repo := gitsync.Open(v.Root)
		if !repo.IsRepo() {
			return historyLoadedMsg{path: p, noGit: true}
		}
		entries, err := repo.Log(p)
		if err != nil {
			return historyLoadedMsg{path: p, err: err}
		}
		if !open {
			// The diff compares against the file on disk.
			current, _ = v.Read(p)
		}
		return historyLoadedMsg{path: p, current: current, entries: entries}
	}
}

// showAtCmd loads the content of p at revision rev.
func showAtCmd(v *vault.Vault, rev, p string) tea.Cmd {
	return func() tea.Msg {
		b, err := gitsync.Open(v.Root).ShowAt(rev, p)
		return historyVersionMsg{rev: rev, content: string(b), err: err}
	}
}

// handleHistoryLoaded opens the full-screen History view and loads the
// newest revision.
func (m *Model) handleHistoryLoaded(msg historyLoadedMsg) tea.Cmd {
	switch {
	case msg.noGit:
		return m.pushToast(msgs.ToastInfo, "History needs git: the vault is not a git repository")
	case msg.err != nil:
		return m.pushToast(msgs.ToastError, fmt.Sprintf("Could not read the history of %s: %v", msg.path, msg.err))
	}
	h := history.New(msg.path, msg.current, m.opts.Styles, m.opts.Palette).
		SetEntries(msg.entries).
		SetSize(m.width, m.height)
	m.history = &h
	m.historyPath = msg.path
	if len(msg.entries) == 0 {
		return nil
	}
	return showAtCmd(m.opts.Vault, msg.entries[0].Rev, msg.path)
}

// restoreVersion writes an old version back as a new edit, then closes
// the History view. When the note is open the version replaces the buffer
// as one undoable edit, which is then saved.
func (m *Model) restoreVersion(msg history.RestoreVersionMsg) tea.Cmd {
	if m.isConflicted(msg.Path) {
		return m.pushToast(msgs.ToastWarn, "Resolve the conflict in "+msg.Path+" first")
	}
	m.history = nil
	rev := msg.Rev
	if len(rev) > 7 {
		rev = rev[:7]
	}
	var edit tea.Cmd
	content, version := msg.Content, uint64(0)
	if msg.Path == m.editor.Path() {
		m.editor, edit = m.editor.ReplaceAll(msg.Content)
		if m.editor.Content() != msg.Content {
			return m.pushToast(msgs.ToastWarn, "Could not restore "+displayName(msg.Path)+": the note cannot be edited right now")
		}
		_, content, version = m.editor.Snapshot()
	}
	save := m.saveNoteCmd(msg.Path, content, version)
	if save == nil {
		return nil
	}
	return tea.Batch(edit, func() tea.Msg {
		res, _ := save().(savedMsg)
		res.restoredFrom = rev
		return res
	})
}

// updateHistoryMsg handles the History view's messages.
func (m *Model) updateHistoryMsg(msg tea.Msg) (tea.Cmd, bool) {
	switch msg := msg.(type) {
	case msgs.OpenHistoryMsg:
		return m.openHistory(msg.Path), true
	case historyLoadedMsg:
		return m.handleHistoryLoaded(msg), true
	case history.SelectionChangedMsg:
		if m.history == nil || m.opts.Vault == nil {
			return nil, true
		}
		return showAtCmd(m.opts.Vault, msg.Rev, m.historyPath), true
	case historyVersionMsg:
		if m.history == nil {
			return nil, true
		}
		content := msg.content
		if msg.err != nil {
			content = fmt.Sprintf("Could not load this version: %v", msg.err)
		}
		h := m.history.SetVersion(msg.rev, content)
		m.history = &h
	case history.RestoreVersionMsg:
		return m.restoreVersion(msg), true
	case history.CloseMsg:
		m.history = nil
	default:
		return nil, false
	}
	return nil, true
}
