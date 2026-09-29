package vim

import "github.com/mathieucroset/notty/internal/buffer"

// lastChange is what "." repeats: the command, the keys typed in the insert
// session it started (if any) and, for a visual change, the shape of the
// selection.
type lastChange struct {
	c    cmd
	keys []Key

	visual        bool
	vmode         Mode
	vlines, vcols int // selection height (lines below the start) and end column
}

// recordChange remembers a normal-mode change for ".".
func (m *Machine) recordChange(c cmd) {
	if m.replaying {
		return
	}
	m.last = &lastChange{c: c}
	m.recording = true
}

// recordVisual remembers a visual-mode change and its selection shape.
func (m *Machine) recordVisual(b *buffer.Buffer, c cmd) {
	if m.replaying {
		return
	}
	s, e := m.anchor, b.Cursor()
	if e.Less(s) {
		s, e = e, s
	}
	lc := &lastChange{c: c, visual: true, vmode: m.mode, vlines: e.Line - s.Line, vcols: e.Col}
	if lc.vlines == 0 {
		lc.vcols = e.Col - s.Col
	}
	m.last = lc
	m.recording = true
}

// endInsertSession stores the keys of the insert session started by the
// change being recorded.
func (m *Machine) endInsertSession(keys []Key) {
	if m.recording && m.last != nil && !m.replaying {
		m.last.keys = append([]Key(nil), keys...)
	}
	m.recording = false
}

// repeat implements ".": replay the last change, with count (when given)
// replacing the original count.
func (m *Machine) repeat(b *buffer.Buffer, c cmd) {
	lc := m.last
	if lc == nil {
		return
	}
	if c.hasCount() && !lc.visual {
		lc.c.count1, lc.c.count2 = c.count(), 0
	}
	m.replaying = true
	defer func() { m.replaying = false }()
	if lc.visual {
		s := b.Cursor()
		e := pos(s.Line+lc.vlines, lc.vcols)
		if lc.vlines == 0 {
			e = pos(s.Line, s.Col+lc.vcols)
		}
		m.mode = lc.vmode
		m.anchor = s
		b.SetCursor(e)
		m.clampNormal(b)
		m.execVisual(b, lc.c)
	} else {
		m.exec(b, lc.c)
	}
	if m.mode == Insert {
		for _, k := range lc.keys {
			m.insertKey(b, k)
		}
		m.leaveInsert(b)
	}
}
