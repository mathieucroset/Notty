package app

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/mathieucroset/notty/internal/ui/dialog"
)

// opKind is the action a dialog's result feeds.
type opKind int

// Dialog actions.
const (
	opNone opKind = iota
	opNewNote
	opNewFolder
	opRename
	opMove
	opTrash
)

// pendingOp is what to do once a dialog is confirmed, with the data the
// action needs. It is plain data (no closures) so the model stays
// comparable.
type pendingOp struct {
	kind opKind
	// path is the note, file or folder the action applies to (the target
	// folder for new notes and folders).
	path string
}

// runPending performs op with the confirmed dialog result res.
func (m *Model) runPending(op pendingOp, res dialog.ResultMsg) tea.Cmd {
	v, value := m.opts.Vault, strings.TrimSpace(res.Value)
	if v == nil {
		return nil
	}
	switch op.kind {
	case opNewNote:
		return createNoteCmd(v, m.ix, op.path, value)
	case opNewFolder:
		return createFolderCmd(v, op.path, value)
	case opRename:
		if value == displayName(op.path) {
			return nil
		}
		return renameCmd(v, m.ix, opRename, op.path, value)
	case opMove:
		dest := strings.Trim(value, "/")
		if dest == parentOf(op.path) {
			return nil
		}
		return renameCmd(v, m.ix, opMove, op.path, dest)
	case opTrash:
		return trashCmd(v, m.ix, op.path)
	}
	return nil
}
