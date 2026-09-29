package app

import (
	"fmt"

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
	err     error
}

func emit(msg tea.Msg) tea.Cmd {
	return func() tea.Msg { return msg }
}

func errorToast(format string, args ...any) tea.Cmd {
	return emit(msgs.ToastMsg{Level: msgs.ToastError, Text: fmt.Sprintf(format, args...)})
}

// loadTreeCmd reads the vault tree off the UI goroutine.
func loadTreeCmd(v *vault.Vault) tea.Cmd {
	return func() tea.Msg {
		root, err := v.Tree()
		return treeLoadedMsg{root: root, err: err}
	}
}

// loadNoteCmd reads a note off the UI goroutine.
func loadNoteCmd(v *vault.Vault, path string, seq int) tea.Cmd {
	return func() tea.Msg {
		content, err := v.Read(path)
		return noteLoadedMsg{seq: seq, path: path, content: content, err: err}
	}
}

// saveLocalCmd writes a snapshot of the local state, so the command never
// races with later changes made in Update.
func saveLocalCmd(s *localstate.State, path string) tea.Cmd {
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
	return func() tea.Msg {
		if err := snap.Save(path); err != nil {
			return msgs.ToastMsg{Level: msgs.ToastWarn, Text: fmt.Sprintf("Could not save local state: %v", err)}
		}
		return nil
	}
}
