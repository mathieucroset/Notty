package app

import (
	tea "charm.land/bubbletea/v2"

	"github.com/mathieucroset/notty/internal/ui/dialog"
)

// opKind is the action a dialog's result feeds.
type opKind int

// Dialog actions.
const (
	opNone opKind = iota
)

// pendingOp is what to do once a dialog is confirmed, with the data the
// action needs. It is plain data (no closures) so the model stays
// comparable.
type pendingOp struct {
	kind opKind
}

// runPending performs op with the confirmed dialog result res.
func (m *Model) runPending(op pendingOp, res dialog.ResultMsg) tea.Cmd {
	if op.kind == opNone || !res.OK {
		return nil
	}
	return nil
}
