package app

import (
	"reflect"
	"runtime/debug"

	tea "charm.land/bubbletea/v2"

	"github.com/mathieucroset/notty/internal/recovery"
)

// Crash recovery (spec §9): the app keeps Options.Snapshot holding the open
// buffer, and Guard records a panic in it, for main to save the buffer to
// .notty/recovery/ and log the stack once the terminal is restored.

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
