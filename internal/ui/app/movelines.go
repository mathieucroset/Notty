package app

import (
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/mathieucroset/notty/internal/index"
	"github.com/mathieucroset/notty/internal/ui/finder"
	"github.com/mathieucroset/notty/internal/ui/msgs"
	"github.com/mathieucroset/notty/internal/vault"
	"github.com/mathieucroset/notty/internal/watcher"
)

// textNoNoteOpen is the toast for a note action without an open note.
const textNoNoteOpen = "No note open"

// lineMove is a "move lines to note" in progress: the open note's lines as
// they were when it started (amendment A7), so only those are deleted.
type lineMove struct {
	path       string
	start, end int // inclusive
	text       []string
}

// moveLinesMsg appends the lines of move to the note target. It changes a
// file, so it waits for the end of a sync merge (isMutation).
type moveLinesMsg struct {
	move   lineMove
	target string
}

// linesAppendedMsg reports the append of moveLinesMsg.
type linesAppendedMsg struct {
	move   lineMove
	target string
	err    error
}

// deleteMovedLinesMsg deletes the lines of move from the open note once
// they are in target. Like the keys typed then, it waits for the end of a
// sync merge (isMutation).
type deleteMovedLinesMsg struct {
	move   lineMove
	target string
}

// isMoveLinesKey reports whether k is alt+m.
func isMoveLinesKey(k tea.KeyPressMsg) bool { return k.Keystroke() == "alt+m" }

// isMoveNoteKey reports whether k is alt+shift+m ("alt+M" in the help):
// terminals report it either way.
func isMoveNoteKey(k tea.KeyPressMsg) bool {
	s := k.Keystroke()
	return s == "alt+shift+m" || s == "alt+M"
}

// startMoveLines captures the lines to move (the selection's lines, or the
// cursor line) and opens the finder to pick the note they go to.
func (m *Model) startMoveLines() tea.Cmd {
	if m.opts.Vault == nil {
		return nil
	}
	p := m.note.path
	if p == "" || p != m.editor.Path() || m.mainView != ViewNote || m.noteView == ViewPreview {
		return m.pushToast(msgs.ToastInfo, textNoNoteOpen)
	}
	if m.isConflicted(p) || m.editor.ModeName() == "READ-ONLY" {
		return m.pushToast(msgs.ToastWarn, conflictRefusal(p))
	}
	start, end, text := m.editor.SelectedLines()
	m.lineMove = &lineMove{path: p, start: start, end: end, text: text}
	var notes []*index.Note
	if m.ix != nil {
		notes = m.ix.Notes()
	}
	f := finder.New(finder.Fuzzy, notes, slices.Clone(m.opts.Local.Recents), m.opts.Styles, m.opts.Palette).
		WithPick("Move to note", p).
		SetSize(m.width, m.height)
	f, cmd := f.Init()
	m.openOverlay(&overlayState{kind: overlayFinder, finder: f})
	return cmd
}

// pickedMoveTarget follows the note picked for the lines being moved.
func (m *Model) pickedMoveTarget(target string) tea.Cmd {
	m.closeOverlayKind(overlayFinder)
	mv := m.lineMove
	m.lineMove = nil
	if mv == nil || target == "" {
		return nil
	}
	return emit(moveLinesMsg{move: *mv, target: target})
}

// moveLines appends the moved lines to the target note on disk, serialised
// with the saves of that note.
func (m *Model) moveLines(msg moveLinesMsg) tea.Cmd {
	v := m.opts.Vault
	if v == nil {
		return nil
	}
	if msg.target == msg.move.path || msg.target == m.editor.Path() {
		return m.pushToast(msgs.ToastWarn, "The lines are already in "+msg.target)
	}
	if cmd, refused := m.refuseConflicted(msg.target); refused {
		return cmd
	}
	return appendLinesCmd(v, m.opts.Watcher, m.ix, msg)
}

// appendLinesCmd appends msg's lines to its target and re-indexes it.
func appendLinesCmd(v *vault.Vault, w *watcher.Watcher, ix *index.Index, msg moveLinesMsg) tea.Cmd {
	text := strings.Join(msg.move.text, "\n") + "\n"
	return func() tea.Msg {
		defer lockFile(v.Abs(msg.target))()
		if err := v.AppendToNote(msg.target, text); err != nil {
			return linesAppendedMsg{move: msg.move, target: msg.target, err: err}
		}
		if w != nil {
			w.NoteSelfWrite(msg.target)
		}
		if ix != nil {
			_ = ix.Update(v, msg.target)
		}
		return linesAppendedMsg{move: msg.move, target: msg.target}
	}
}

// handleLinesAppended tells the syncer about the target, then deletes the
// lines from the source; a failed append leaves the source alone.
func (m *Model) handleLinesAppended(msg linesAppendedMsg) tea.Cmd {
	if msg.err != nil {
		return m.pushToast(msgs.ToastError, fmt.Sprintf("Could not move the lines to %s: %v", msg.target, msg.err))
	}
	m.queueReindex(msg.target)
	m.noteChanged(msg.target)
	m.refreshIndexViews()
	return emit(deleteMovedLinesMsg{move: msg.move, target: msg.target})
}

// deleteMovedLines deletes the moved lines from the open note as one undo
// step, provided it still is their note and they are unchanged; otherwise
// they stay, copied, with a warning.
func (m *Model) deleteMovedLines(msg deleteMovedLinesMsg) tea.Cmd {
	mv := msg.move
	if m.editor.Path() == mv.path {
		ed, cmd, ok := m.editor.DeleteLines(mv.start, mv.end, mv.text)
		if ok {
			m.editor = ed
			n := len(mv.text)
			what := "1 line"
			if n != 1 {
				what = fmt.Sprintf("%d lines", n)
			}
			return tea.Batch(cmd, m.pushToast(msgs.ToastInfo, fmt.Sprintf("Moved %s to %s", what, msg.target)))
		}
	}
	return m.pushToast(msgs.ToastWarn, fmt.Sprintf("Copied to %s; left here because the note changed", msg.target))
}
