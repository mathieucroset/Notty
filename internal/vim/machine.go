// Package vim is Notty's modal editing engine: a pure state machine that
// takes key events and a *buffer.Buffer, applies motions and edits, and
// returns an Effect describing side effects for the UI (spec §5 "vim",
// §4.4). It contains no rendering code. Plain is the non-modal editor used
// when vim mode is off.
package vim

import (
	"math"
	"strings"

	"github.com/mathieucroset/notty/internal/buffer"
)

// Mode is the vim editing mode.
type Mode int

const (
	Normal Mode = iota
	Insert
	Visual
	VisualLine
	Command
)

// Effect describes side effects of a key that the UI must perform.
type Effect struct {
	Save, Quit, OpenNote, Help bool
	NoteArg, ImgArg            string
	// Clipboard is text to put on the system clipboard ("+y, plain ctrl+c).
	Clipboard *string
	// NeedClipboard asks the UI to read the clipboard and call
	// PasteClipboard ("+p, ctrl+v).
	NeedClipboard bool
	// PasteBefore is passed back to PasteClipboard ("+P).
	PasteBefore bool
	// ToggleTask reports that a task toggle was applied to the buffer.
	ToggleTask              bool
	FocusSidebar, FocusMain bool
	// Blocked reports an edit attempted in read-only mode.
	Blocked bool
	Message string
}

// WrapNavigator moves by screen rows for gj / gk; the editor component
// provides one that knows the soft-wrap layout.
type WrapNavigator interface {
	Down(b *buffer.Buffer, p buffer.Pos, n int) buffer.Pos
	Up(b *buffer.Buffer, p buffer.Pos, n int) buffer.Pos
}

// Editor is implemented by the vim Machine and the non-modal Plain editor.
type Editor interface {
	Handle(b *buffer.Buffer, k Key) Effect
	PasteClipboard(b *buffer.Buffer, text string, before bool)
	ModeName() string
	SetReadOnly(bool)
	// Selection returns the selection to highlight. End is exclusive; a
	// selection that includes a line break ends at the start of the next
	// line (which may be one past the last line for a linewise selection
	// of the last line).
	Selection(b *buffer.Buffer) (buffer.Range, bool)
}

// wantEOL is the desired column after $: stick to the end of the line.
const wantEOL = math.MaxInt

// Machine is the vim state machine. The zero value is not usable; call New.
type Machine struct {
	mode     Mode
	readOnly bool
	pending  []string   // normal/visual keys typed so far
	curswant int        // desired column for vertical motions
	lastPos  buffer.Pos // cursor when the last key was handled
	nav      WrapNavigator
	lastFind findState

	eff Effect // effect of the key being handled

	anchor buffer.Pos // visual selection start

	unnamed    register
	pasteCount int // count of a "+p waiting for PasteClipboard

	// undo cursor restoration: buffer version after a change -> cursor
	// before it (and the mirror for redo)
	undoCur, redoCur map[uint64]buffer.Pos
	chgCursor        buffer.Pos
	chgVersion       uint64

	// dot-repeat
	last      *lastChange
	recording bool // the insert session belongs to m.last
	replaying bool

	// insert session
	insertRepeat int   // count for i/a/I/A (3ifoo<esc>)
	sessionKeys  []Key // keys typed in the current insert session
}

var (
	_ Editor = (*Machine)(nil)
)

// New returns a Machine in normal mode.
func New() *Machine {
	return &Machine{undoCur: map[uint64]buffer.Pos{}, redoCur: map[uint64]buffer.Pos{}}
}

// Mode returns the current mode.
func (m *Machine) Mode() Mode { return m.mode }

// SetWrapNavigator sets the navigator used by gj / gk (nil: like j / k).
func (m *Machine) SetWrapNavigator(w WrapNavigator) { m.nav = w }

// Pending returns the keys of a partially typed command, e.g. `2d`.
func (m *Machine) Pending() string { return strings.Join(m.pending, "") }

// ModeName returns the mode for the status bar.
func (m *Machine) ModeName() string {
	switch m.mode {
	case Insert:
		return "INSERT"
	case Visual:
		return "VISUAL"
	case VisualLine:
		return "V-LINE"
	case Command:
		return "COMMAND"
	}
	if m.readOnly {
		return "READ-ONLY"
	}
	return "NORMAL"
}

func (m *Machine) isVisual() bool { return m.mode == Visual || m.mode == VisualLine }

// Handle processes one key.
func (m *Machine) Handle(b *buffer.Buffer, k Key) Effect {
	m.eff = Effect{}
	if b.Cursor() != m.lastPos {
		// Moved from outside (mouse, reload): forget the desired column.
		m.curswant = b.Cursor().Col
	}
	defer func() { m.lastPos = b.Cursor() }()
	if m.mode == Insert {
		m.insertKey(b, k)
		return m.eff
	}
	if m.mode == Command {
		m.commandKey(b, k)
		return m.eff
	}
	toks := keyTokens(k)
	for i, tok := range toks {
		if m.mode == Insert {
			// A multi-grapheme key entered insert mode part way (e.g. a
			// paste of "ifoo" in normal mode): the rest is typed text.
			rest := strings.Join(toks[i:], "")
			rest = strings.ReplaceAll(rest, "<space>", " ")
			m.insertKey(b, Key{Text: rest})
			break
		}
		m.normalToken(b, tok)
	}
	return m.eff
}

// normalToken handles one token in normal or visual mode.
func (m *Machine) normalToken(b *buffer.Buffer, tok string) {
	if tok == "<esc>" || tok == "<c-c>" {
		if len(m.pending) > 0 {
			m.pending = nil
			return
		}
		if m.isVisual() {
			m.exitVisual(b)
		}
		return
	}
	m.pending = append(m.pending, tok)
	c, st := parse(m.pending, m.isVisual())
	switch st {
	case parseMore:
		return
	case parseBad:
		m.pending = nil
		return
	}
	m.pending = nil
	m.exec(b, c)
	if m.mode == Normal || m.isVisual() {
		m.clampNormal(b)
	}
}

// exec runs a complete command.
func (m *Machine) exec(b *buffer.Buffer, c cmd) {
	if m.isVisual() {
		m.execVisual(b, c)
		return
	}
	if c.op == "" && isMotion(c.name) {
		m.moveCursor(b, c)
		return
	}
	if isChange(c) {
		m.recordChange(c)
		m.beginChange(b)
		if c.op != "" {
			m.execOperator(b, c)
		} else {
			m.execChange(b, c)
		}
		if m.mode != Insert {
			m.endChange(b)
		}
		return
	}
	if c.op != "" {
		m.execOperator(b, c) // yank
		return
	}
	m.execOther(b, c)
}

// isChange reports whether c modifies the buffer (or enters insert mode).
func isChange(c cmd) bool {
	if c.op != "" {
		return c.op != "y"
	}
	return changeActions[c.name]
}

// changeActions are the normal-mode commands that modify the buffer.
var changeActions = map[string]bool{
	"i": true, "a": true, "I": true, "A": true, "o": true, "O": true,
	"x": true, "X": true, "<del>": true, "s": true, "S": true, "J": true,
	"p": true, "P": true, "D": true, "C": true, "~": true, "r": true,
}

// execChange runs a buffer-modifying command inside an undo group.
func (m *Machine) execChange(b *buffer.Buffer, c cmd) {
	cur := b.Cursor()
	switch c.name {
	case "i":
		m.startInsert(b, cur, c.count())
	case "a":
		if b.LineLen(cur.Line) > 0 {
			cur.Col++
		}
		m.startInsert(b, cur, c.count())
	case "I":
		m.startInsert(b, pos(cur.Line, firstNonBlankOrEnd(b, cur.Line)), c.count())
	case "A":
		m.startInsert(b, lineEnd(b, cur.Line), c.count())
	case "o":
		m.openLine(b, cur.Line, false)
	case "O":
		m.openLine(b, cur.Line, true)
	case "x", "<del>":
		m.execOperator(b, cmd{reg: c.reg, count1: c.count(), op: "d", name: "l"})
	case "X":
		m.execOperator(b, cmd{reg: c.reg, count1: c.count(), op: "d", name: "h"})
	case "D":
		m.execOperator(b, cmd{reg: c.reg, count1: c.count(), op: "d", name: "$"})
	case "C":
		m.execOperator(b, cmd{reg: c.reg, count1: c.count(), op: "c", name: "$"})
	case "S":
		m.execOperator(b, cmd{reg: c.reg, count1: c.count(), op: "c", name: "c"})
	case "s":
		if b.LineLen(cur.Line) == 0 {
			m.startInsert(b, cur, 1)
		} else {
			m.execOperator(b, cmd{reg: c.reg, count1: c.count(), op: "c", name: "l"})
		}
	case "r":
		replaceChars(b, c.arg, c.count())
	case "J":
		joinLines(b, cur.Line, c.count())
	case "~":
		toggleCase(b, c.count())
	case "p", "P":
		if c.reg == "+" {
			m.eff.NeedClipboard = true
			m.eff.PasteBefore = c.name == "P"
			m.pasteCount = c.count()
			return
		}
		m.put(b, m.unnamed, c.name == "P", c.count())
	}
}

// execOther runs commands that do not modify the buffer through a change
// group (yank, undo, redo, focus, mode switches).
func (m *Machine) execOther(b *buffer.Buffer, c cmd) {
	switch c.name {
	case "Y":
		m.execOperator(b, cmd{reg: c.reg, count1: c.count(), op: "y", name: "y"})
	case "u":
		m.undo(b, c.count())
	case "<c-r>":
		m.redo(b, c.count())
	case "v":
		m.enterVisual(b, Visual)
	case "V":
		m.enterVisual(b, VisualLine)
	case ".":
		m.repeat(b, c)
	}
}

// firstNonBlankOrEnd is like firstNonBlank, but returns the end of line on
// an all-blank line (vim's I on "   " appends).
func firstNonBlankOrEnd(b *buffer.Buffer, l int) int {
	line := b.Line(l)
	ws := leadingWS(line)
	return buffer.ByteToCol(line, len(ws))
}

// clampNormal keeps the cursor on a character: in normal and visual modes
// the column is at most LineLen-1 (0 on an empty line).
func (m *Machine) clampNormal(b *buffer.Buffer) {
	p := b.Cursor()
	if n := b.LineLen(p.Line); p.Col >= n {
		p.Col = max(0, n-1)
	}
	b.SetCursor(p)
}

// normalCol clamps col to a character position on line l.
func normalCol(b *buffer.Buffer, l, col int) int {
	return max(0, min(col, b.LineLen(l)-1))
}

// Stubs filled in by later features.

// execVisualSpecial handles visual commands added by later features.
func (m *Machine) execVisualSpecial(b *buffer.Buffer, c cmd) bool { return false }

func (m *Machine) commandKey(b *buffer.Buffer, k Key) { m.mode = Normal }

// PasteClipboard pastes text the UI read from the system clipboard after a
// NeedClipboard effect: at the cursor in insert mode, otherwise like p / P
// (linewise if text ends with a newline).
func (m *Machine) PasteClipboard(b *buffer.Buffer, text string, before bool) {
	defer func() { m.lastPos = b.Cursor() }()
	if text == "" {
		return
	}
	if m.mode == Insert {
		m.insertKey(b, Key{Text: text})
		return
	}
	count := max(1, m.pasteCount)
	m.pasteCount = 0
	m.beginChange(b)
	m.put(b, register{text: text, linewise: strings.HasSuffix(text, "\n")}, before, count)
	m.endChange(b)
	m.clampNormal(b)
}

// SetReadOnly toggles read-only mode.
func (m *Machine) SetReadOnly(ro bool) { m.readOnly = ro }

func (m *Machine) searchMotion(b *buffer.Buffer, p buffer.Pos, reverse bool, n int) motionRes {
	return motionRes{}
}

// CommandLine returns the command line while in command mode.
func (m *Machine) CommandLine() string { return "" }
