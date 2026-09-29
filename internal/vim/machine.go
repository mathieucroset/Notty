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

	// insert session
	insertRepeat int   // count for i/a/I/A (3ifoo<esc>)
	sessionKeys  []Key // keys typed in the current insert session
}

var (
	_ Editor = (*Machine)(nil)
)

// New returns a Machine in normal mode.
func New() *Machine { return &Machine{} }

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
		b.BeginGroup()
		m.execChange(b, c)
		if m.mode != Insert {
			b.EndGroup()
		}
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
	}
}

// execOther runs commands that neither move nor modify.
func (m *Machine) execOther(b *buffer.Buffer, c cmd) {}

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

func (m *Machine) execVisual(b *buffer.Buffer, c cmd) {}

func (m *Machine) exitVisual(b *buffer.Buffer) { m.mode = Normal }

func (m *Machine) commandKey(b *buffer.Buffer, k Key) { m.mode = Normal }

// PasteClipboard pastes text the UI read from the system clipboard.
func (m *Machine) PasteClipboard(b *buffer.Buffer, text string, before bool) {}

// SetReadOnly toggles read-only mode.
func (m *Machine) SetReadOnly(ro bool) { m.readOnly = ro }

// Selection returns the visual selection.
func (m *Machine) Selection(b *buffer.Buffer) (buffer.Range, bool) {
	return buffer.Range{}, false
}

func (m *Machine) searchMotion(b *buffer.Buffer, p buffer.Pos, reverse bool, n int) motionRes {
	return motionRes{}
}

// CommandLine returns the command line while in command mode.
func (m *Machine) CommandLine() string { return "" }
