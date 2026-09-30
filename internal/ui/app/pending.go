package app

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/mathieucroset/notty/internal/recovery"
	"github.com/mathieucroset/notty/internal/ui/dialog"
	"github.com/mathieucroset/notty/internal/ui/msgs"
	"github.com/mathieucroset/notty/internal/vault"
)

// opDone names a finished rename, move or trash for messages.
func opDone(op opKind) string {
	switch op {
	case opRename:
		return "renamed"
	case opMove:
		return "moved"
	}
	return "moved to trash"
}

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
	opDeleteForever
	opEmptyTrash
	opCleanAttachments
	opExternalChange
	opImportImage
	opRecovered
	// opMoveConfirm is the confirmation of a move into a folder that does
	// not exist yet (amendment A9).
	opMoveConfirm
)

// pendingOp is what to do once a dialog is confirmed, with the data the
// action needs. It is plain data (no closures) so the model stays
// comparable.
type pendingOp struct {
	kind opKind
	// path is the note, file or folder the action applies to (the target
	// folder for new notes and folders).
	path string
	// dest is a move's destination folder, validated, once skipExistCheck
	// is set; before that the dialog's value is used.
	dest string
	// skipExistCheck is set once a move's destination was validated and,
	// when it did not exist, its creation confirmed (amendment B3).
	skipExistCheck bool
	// item is the trash item to delete forever.
	item vault.TrashItem
	// files are the attachments to delete.
	files []string
	// note is the note an image is imported for; data and ext are the
	// clipboard image bytes (nil when importing the file at path).
	note string
	data []byte
	ext  string
	// recovered is the recovery file offered back.
	recovered recovery.File
}

// runPendingMsg runs a confirmed operation once the buffer it touches is
// saved.
type runPendingMsg struct {
	op  pendingOp
	res dialog.ResultMsg
}

// runPending performs op with the confirmed dialog result res. Renaming,
// moving or trashing the open note (or a folder holding it) saves its
// buffer first, and is abandoned if that save fails.
func (m *Model) runPending(op pendingOp, res dialog.ResultMsg) tea.Cmd {
	v, value := m.opts.Vault, strings.TrimSpace(res.Value)
	if v == nil {
		return nil
	}
	if op.kind == opMove && !op.skipExistCheck {
		// A folder that does not exist yet is created, once confirmed.
		dest, err := m.moveDest(op.path, value)
		if err != nil {
			return m.pushToast(msgs.ToastWarn, fmt.Sprintf("Could not move '%s': %v", displayName(op.path), err))
		}
		if dest == parentOf(op.path) {
			return nil
		}
		op.dest, op.skipExistCheck = dest, true
		if _, err := os.Lstat(v.Abs(dest)); errors.Is(err, fs.ErrNotExist) {
			return m.confirmMoveToNewFolder(op)
		}
	}
	switch op.kind {
	case opRename, opMove, opTrash:
		// The path may have become conflicted while the dialog was open.
		if cmd, refused := m.refuseConflicted(op.path); refused {
			return cmd
		}
		if m.note.path != "" && isUnder(m.note.path, op.path) {
			if cmd := m.saveThen(runPendingMsg{op: op, res: res}, "it was not "+opDone(op.kind)); cmd != nil {
				return cmd
			}
		}
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
		return renameCmd(v, m.ix, opMove, op.path, op.dest)
	case opMoveConfirm:
		// Through runPending again, so the save-first and conflict checks
		// run at the time of the move.
		return m.runPending(pendingOp{kind: opMove, path: op.path, dest: op.dest, skipExistCheck: true}, res)
	case opTrash:
		return trashCmd(v, m.ix, op.path)
	case opDeleteForever:
		return deleteForeverCmd(v, op.item)
	case opEmptyTrash:
		return emptyTrashCmd(v)
	case opCleanAttachments:
		return deleteFilesCmd(v, op.files)
	case opExternalChange:
		return m.resolveExternalChange(op.path, res.Choice)
	case opImportImage:
		return m.runImport(op)
	}
	return nil
}
