package buffer

// change is one recorded primitive edit, in byte coordinates. Applying it
// forward replaces removed (at start) with inserted; its inverse replaces
// inserted (at start) with removed.
type change struct {
	start                   bpos
	removed, inserted       string
	trailBefore, trailAfter bool // trailing-newline flag around the edit
}

// step is one undo unit: the changes of a single primitive or of an
// outermost BeginGroup/EndGroup pair. before and after identify the buffer
// states on either side of the step, which is how Dirty detects a return to
// the saved state.
type step struct {
	changes       []change
	before, after uint64
}

// history holds the undo machinery; it is embedded in Buffer.
type history struct {
	undo, redo []*step
	pending    *step // open group's step, nil until its first edit
	groupDepth int

	state     uint64 // id of the current state
	saved     uint64 // id of the state last marked saved
	lastState uint64 // highest state id handed out
}

// BeginGroup opens an undo group. Groups nest; all edits until the matching
// outermost EndGroup undo as a single step.
func (b *Buffer) BeginGroup() { b.groupDepth++ }

// EndGroup closes an undo group. Unbalanced calls are ignored.
func (b *Buffer) EndGroup() {
	if b.groupDepth == 0 {
		return
	}
	b.groupDepth--
	if b.groupDepth == 0 {
		b.commitPending()
	}
}

// commitPending pushes the open group's step, if it has any edits.
func (b *Buffer) commitPending() {
	if b.pending != nil {
		b.undo = append(b.undo, b.pending)
		b.pending = nil
	}
}

// newStep starts a step leading from the current state to a fresh one.
func (b *Buffer) newStep() *step {
	b.lastState++
	s := &step{before: b.state, after: b.lastState}
	b.state = s.after
	return s
}

// record stores a primitive edit for undo and clears the redo stack. Every
// edit gets a fresh state id, even inside a group, so that a MarkSaved
// between two edits of one group is not mistaken for the group's end state.
func (b *Buffer) record(c change) {
	b.redo = nil
	if b.groupDepth > 0 {
		if b.pending == nil {
			b.pending = b.newStep()
		} else {
			b.lastState++
			b.pending.after = b.lastState
			b.state = b.lastState
		}
		b.pending.changes = append(b.pending.changes, c)
		return
	}
	s := b.newStep()
	s.changes = []change{c}
	b.undo = append(b.undo, s)
}

// Undo reverts the most recent step and returns the start of the change,
// which also becomes the cursor. It reports false if there is nothing to
// undo. Calling Undo inside an open group first closes off that group's
// edits as their own step.
func (b *Buffer) Undo() (Pos, bool) {
	b.commitPending()
	if len(b.undo) == 0 {
		return b.cursor, false
	}
	s := b.undo[len(b.undo)-1]
	b.undo = b.undo[:len(b.undo)-1]
	for i := len(s.changes) - 1; i >= 0; i-- {
		c := s.changes[i]
		b.replaceBytes(c.start, endOf(c.start, c.inserted), c.removed)
		b.trailingNewline = c.trailBefore
	}
	b.state = s.before
	b.redo = append(b.redo, s)
	return b.restoreCursor(s), true
}

// Redo reapplies the most recently undone step and returns the start of the
// change, which also becomes the cursor. It reports false if there is
// nothing to redo.
func (b *Buffer) Redo() (Pos, bool) {
	if len(b.redo) == 0 {
		return b.cursor, false
	}
	s := b.redo[len(b.redo)-1]
	b.redo = b.redo[:len(b.redo)-1]
	for _, c := range s.changes {
		b.replaceBytes(c.start, endOf(c.start, c.removed), c.inserted)
		b.trailingNewline = c.trailAfter
	}
	b.state = s.after
	b.undo = append(b.undo, s)
	return b.restoreCursor(s), true
}

// restoreCursor moves the cursor to the earliest start among the step's
// changes. Every change in the step lies at or after that point, so the text
// before it is identical in every state the step passes through and the
// position is valid both after undo and after redo.
func (b *Buffer) restoreCursor(s *step) Pos {
	first := s.changes[0].start
	for _, c := range s.changes[1:] {
		if c.start.line < first.line || (c.start.line == first.line && c.start.off < first.off) {
			first = c.start
		}
	}
	b.cursor = b.Clamp(b.toPos(first))
	return b.cursor
}

// Dirty reports whether the text differs from the last saved state (the
// initial text counts as saved). Undoing or redoing back to the saved state
// makes the buffer clean again.
func (b *Buffer) Dirty() bool { return b.state != b.saved }

// MarkSaved records the current state as saved.
func (b *Buffer) MarkSaved() { b.saved = b.state }
