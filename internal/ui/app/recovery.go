package app

import (
	"fmt"
	"os"
	"reflect"
	"runtime/debug"

	tea "charm.land/bubbletea/v2"

	"github.com/mathieucroset/notty/internal/index"
	"github.com/mathieucroset/notty/internal/recovery"
	"github.com/mathieucroset/notty/internal/ui/dialog"
	"github.com/mathieucroset/notty/internal/ui/msgs"
	"github.com/mathieucroset/notty/internal/vault"
)

// Crash recovery (spec §9): the app keeps Options.Snapshot holding the open
// buffer, and Guard records a panic in it, for main to save the buffer to
// .notty/recovery/ and log the stack once the terminal is restored. On the
// next start each recovery file is offered back: restored as a new note,
// or discarded.

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

// guard wraps the root model so a panic in Init, Update or a command
// (batched and sequenced ones included) is recorded in the snapshot with
// its stack and ends the program with tea.Quit: Bubble Tea then restores
// the terminal as on any quit, and prints nothing. A panic in View is
// recorded and passed on to Bubble Tea's own recovery, which restores the
// terminal and makes Run fail with tea.ErrProgramPanic.
type guard struct {
	m       tea.Model
	snap    *recovery.Snapshot
	crashed bool
}

// panicMsg reports a panic caught in a command.
type panicMsg struct{}

// Guard returns m guarded against panics, recorded in snap.
func Guard(m tea.Model, snap *recovery.Snapshot) tea.Model {
	return &guard{m: m, snap: snap}
}

// record keeps the panic value r with the current stack, which, called
// from a deferred recover, still holds the panicking frames.
func (g *guard) record(r any) {
	g.snap.RecordPanic(r, debug.Stack())
	g.crashed = true
}

func (g *guard) Init() (cmd tea.Cmd) {
	defer func() {
		if r := recover(); r != nil {
			g.record(r)
			cmd = tea.Quit
		}
	}()
	return g.wrap(g.m.Init())
}

func (g *guard) Update(msg tea.Msg) (res tea.Model, cmd tea.Cmd) {
	if g.crashed {
		return g, nil
	}
	if _, ok := msg.(panicMsg); ok {
		g.crashed = true
		return g, tea.Quit
	}
	defer func() {
		if r := recover(); r != nil {
			g.record(r)
			res, cmd = g, tea.Quit
		}
	}()
	next, cmd := g.m.Update(msg)
	g.m = next
	return g, g.wrap(cmd)
}

func (g *guard) View() tea.View {
	if g.crashed {
		return tea.View{}
	}
	defer func() {
		if r := recover(); r != nil {
			g.record(r)
			panic(r)
		}
	}()
	return g.m.View()
}

// cmdType is tea.Cmd, the element type of batches and sequences.
var cmdType = reflect.TypeFor[tea.Cmd]()

// wrap returns cmd recording a panic as a panicMsg. The commands of a
// batch or sequence it returns are wrapped in turn; they are recognized by
// their type, a slice of commands (tea's sequence type is unexported).
func (g *guard) wrap(cmd tea.Cmd) tea.Cmd {
	if cmd == nil {
		return nil
	}
	snap := g.snap
	return func() (msg tea.Msg) {
		defer func() {
			if r := recover(); r != nil {
				snap.RecordPanic(r, debug.Stack())
				msg = panicMsg{}
			}
		}()
		msg = cmd()
		v := reflect.ValueOf(msg)
		if v.Kind() != reflect.Slice || v.Type().Elem() != cmdType {
			return msg
		}
		out := reflect.MakeSlice(v.Type(), v.Len(), v.Len())
		for i := range v.Len() {
			c, _ := v.Index(i).Interface().(tea.Cmd)
			out.Index(i).Set(reflect.ValueOf(g.wrap(c)))
		}
		return out.Interface()
	}
}

// dlgRecovered offers back the unsaved changes a crash left in a recovery
// file (spec §9).
const dlgRecovered = "recovered"

// Recovered changes choices, in dialog order; esc decides later, keeping
// the file for the next start.
const (
	choiceRestore = iota
	choiceDiscard
)

// recoveredListMsg carries the recovery files found at start.
type recoveredListMsg struct {
	files []recovery.File
	err   error
}

// recoveredDoneMsg reports a recovery file restored as the note path, or
// discarded.
type recoveredDoneMsg struct {
	file      recovery.File
	path      string
	discarded bool
	err       error
}

// listRecoveredCmd lists the vault's recovery files.
func listRecoveredCmd(v *vault.Vault) tea.Cmd {
	if v == nil {
		return nil
	}
	return func() tea.Msg {
		files, err := recovery.List(v.Root)
		return recoveredListMsg{files: files, err: err}
	}
}

// handleRecoveredList queues the recovery files found at start and offers
// the first.
func (m *Model) handleRecoveredList(msg recoveredListMsg) tea.Cmd {
	if msg.err != nil {
		return m.pushToast(msgs.ToastWarn, fmt.Sprintf("Could not look for recovered changes: %v", msg.err))
	}
	m.recovered, m.recoveredTotal, m.recoveredShown = msg.files, len(msg.files), 0
	return m.offerNextRecovered()
}

// recoveredTitle is the title of the note a recovery file is restored as.
func recoveredTitle(f recovery.File) string {
	return displayName(f.Note) + " (recovered)"
}

// offerNextRecovered opens the dialog for the next queued recovery file.
// It is stacked on top, so a dialog already open comes back once it is
// answered.
func (m *Model) offerNextRecovered() tea.Cmd {
	if len(m.recovered) == 0 {
		return nil
	}
	f := m.recovered[0]
	m.recovered = m.recovered[1:]
	m.recoveredShown++
	title := "Unsaved changes recovered"
	if m.recoveredTotal > 1 {
		title += fmt.Sprintf(" (%d of %d)", m.recoveredShown, m.recoveredTotal)
	}
	text := fmt.Sprintf("Notty stopped unexpectedly on %s with unsaved changes to '%s'.",
		f.Time.Format("Jan 2 at 15:04"), f.Note)
	d := dialog.NewChoice(dlgRecovered, title, text,
		[]string{"Restore as '" + recoveredTitle(f) + "'", "Discard them"}, m.opts.Styles)
	m.pushOverlay(&overlayState{kind: overlayDialog, dialog: d, pending: pendingOp{kind: opRecovered, recovered: f}})
	return nil
}

// answerRecovered acts on the recovered changes dialog: restore the file
// as a new note, discard it, or (esc) leave it for the next start.
func (m *Model) answerRecovered(f recovery.File, res dialog.ResultMsg) tea.Cmd {
	v := m.opts.Vault
	switch {
	case v == nil:
		return nil
	case !res.OK:
		return m.offerNextRecovered()
	case res.Choice == choiceDiscard:
		return func() tea.Msg {
			return recoveredDoneMsg{file: f, discarded: true, err: recovery.Remove(v.Root, f)}
		}
	}
	return restoreRecoveredCmd(v, m.ix, f)
}

// restoreRecoveredCmd creates the note "<name> (recovered)" in the folder
// of the note the text came from (the vault root when that folder is gone)
// holding the recovered text, then deletes the recovery file.
func restoreRecoveredCmd(v *vault.Vault, ix *index.Index, f recovery.File) tea.Cmd {
	return func() tea.Msg {
		content, err := recovery.Read(v.Root, f)
		if err != nil {
			return recoveredDoneMsg{file: f, err: err}
		}
		folder := parentOf(f.Note)
		if info, err := os.Stat(v.Abs(folder)); err != nil || !info.IsDir() {
			folder = ""
		}
		rel, err := v.CreateNote(folder, recoveredTitle(f))
		if err != nil && folder != "" {
			rel, err = v.CreateNote("", recoveredTitle(f))
		}
		if err != nil {
			return recoveredDoneMsg{file: f, err: err}
		}
		if err := v.Save(rel, content); err != nil {
			return recoveredDoneMsg{file: f, path: rel, err: err}
		}
		if ix != nil {
			_ = ix.Update(v, rel)
		}
		return recoveredDoneMsg{file: f, path: rel, err: recovery.Remove(v.Root, f)}
	}
}

// handleRecoveredDone opens a restored note, reports the outcome and
// offers the next recovery file.
func (m *Model) handleRecoveredDone(msg recoveredDoneMsg) tea.Cmd {
	var cmds []tea.Cmd
	if msg.path != "" {
		// The new note is in the vault: open it like any new note.
		cmds = append(cmds, m.handleFileOp(fileOpMsg{op: opNewNote, path: msg.path}))
	}
	switch {
	case msg.err != nil && msg.path != "" && !msg.discarded:
		cmds = append(cmds, m.pushToast(msgs.ToastWarn,
			fmt.Sprintf("Restored the recovered changes as '%s', but: %v", displayName(msg.path), msg.err)))
	case msg.err != nil:
		cmds = append(cmds, m.pushToast(msgs.ToastError,
			fmt.Sprintf("Could not %s the recovered changes to '%s': %v", recoveredVerb(msg), msg.file.Note, msg.err)))
	case msg.discarded:
		cmds = append(cmds, m.pushToast(msgs.ToastInfo,
			fmt.Sprintf("Discarded the recovered changes to '%s'", msg.file.Note)))
	default:
		cmds = append(cmds, m.pushToast(msgs.ToastInfo,
			fmt.Sprintf("Restored the recovered changes as '%s'", displayName(msg.path))))
	}
	return tea.Batch(append(cmds, m.offerNextRecovered())...)
}

func recoveredVerb(msg recoveredDoneMsg) string {
	if msg.discarded {
		return "discard"
	}
	return "restore"
}
