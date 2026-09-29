package buffer

import (
	"math/rand/v2"
	"testing"
)

func TestUndoSingleOp(t *testing.T) {
	tests := []struct {
		name    string
		initial string
		edit    func(b *Buffer)
		wantPos Pos
	}{
		{"insert", "hello", func(b *Buffer) { b.Insert(P(0, 2), "XY") }, P(0, 2)},
		{"multi-line insert", "hello\n", func(b *Buffer) { b.Insert(P(0, 5), "\na\nb") }, P(0, 5)},
		{"delete across lines", "ab\ncd\nef", func(b *Buffer) { b.Delete(R(0, 1, 2, 1)) }, P(0, 1)},
		{"replace", "hello world", func(b *Buffer) { b.Replace(R(0, 6, 0, 11), "there\nfriend") }, P(0, 6)},
		{"delete emoji", "a" + thumbsUp + "b", func(b *Buffer) { b.Delete(R(0, 1, 0, 2)) }, P(0, 1)},
		{"combining mark merges then undoes", "e", func(b *Buffer) { b.Insert(P(0, 1), "́") }, P(0, 1)},
		{"set text", "a\nb\n", func(b *Buffer) { b.SetText("zzz") }, P(0, 0)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := New(tt.initial)
			tt.edit(b)
			pos, ok := b.Undo()
			if !ok {
				t.Fatal("Undo() ok = false, want true")
			}
			if got := b.String(); got != tt.initial {
				t.Errorf("after undo String() = %q, want %q", got, tt.initial)
			}
			if pos != tt.wantPos {
				t.Errorf("Undo() pos = %v, want %v", pos, tt.wantPos)
			}
			if b.Cursor() != pos {
				t.Errorf("Cursor() = %v, want %v", b.Cursor(), pos)
			}
			if _, ok := b.Undo(); ok {
				t.Errorf("second Undo() ok = true, want false")
			}
		})
	}
}

func TestUndoRedoEmpty(t *testing.T) {
	b := New("abc")
	if _, ok := b.Undo(); ok {
		t.Error("Undo() on fresh buffer ok = true")
	}
	if _, ok := b.Redo(); ok {
		t.Error("Redo() on fresh buffer ok = true")
	}
	b.Insert(P(0, 0), "")
	if _, ok := b.Undo(); ok {
		t.Error("no-op edit created an undo step")
	}
}

func TestGroupUndoesAsOne(t *testing.T) {
	b := New("abc")
	b.BeginGroup()
	b.Insert(P(0, 3), "d")
	b.Insert(P(0, 4), "e")
	b.Insert(P(0, 5), "f")
	b.EndGroup()
	if got := b.String(); got != "abcdef" {
		t.Fatalf("String() = %q", got)
	}
	pos, ok := b.Undo()
	if !ok || b.String() != "abc" {
		t.Fatalf("Undo() = %v, %v; String() = %q, want abc", pos, ok, b.String())
	}
	if pos != P(0, 3) {
		t.Errorf("Undo() pos = %v, want %v", pos, P(0, 3))
	}
	if _, ok := b.Undo(); ok {
		t.Error("group produced more than one undo step")
	}
}

func TestGroupUndoPosIsStartOfChange(t *testing.T) {
	// Three backspaces walking left: the change starts at the leftmost one.
	b := New("abcdef")
	b.BeginGroup()
	b.Delete(R(0, 4, 0, 5))
	b.Delete(R(0, 3, 0, 4))
	b.Delete(R(0, 2, 0, 3))
	b.EndGroup()
	if b.String() != "abf" {
		t.Fatalf("String() = %q", b.String())
	}
	pos, _ := b.Undo()
	if b.String() != "abcdef" {
		t.Fatalf("after undo String() = %q", b.String())
	}
	if pos != P(0, 2) {
		t.Errorf("Undo() pos = %v, want %v", pos, P(0, 2))
	}
	pos, _ = b.Redo()
	if b.String() != "abf" {
		t.Fatalf("after redo String() = %q", b.String())
	}
	if pos != P(0, 2) {
		t.Errorf("Redo() pos = %v, want %v", pos, P(0, 2))
	}
}

func TestNestedGroups(t *testing.T) {
	b := New("")
	b.BeginGroup()
	b.Insert(P(0, 0), "a")
	b.BeginGroup()
	b.Insert(P(0, 1), "b")
	b.EndGroup() // inner end must not close the outer group
	b.Insert(P(0, 2), "c")
	b.EndGroup()
	b.Insert(P(0, 3), "d") // separate step

	b.Undo()
	if got := b.String(); got != "abc" {
		t.Fatalf("after first undo String() = %q, want abc", got)
	}
	b.Undo()
	if got := b.String(); got != "" {
		t.Fatalf("after second undo String() = %q, want empty", got)
	}
	if _, ok := b.Undo(); ok {
		t.Error("expected exactly two undo steps")
	}
}

func TestGroupEdgeCases(t *testing.T) {
	t.Run("empty group adds no step", func(t *testing.T) {
		b := New("x")
		b.Insert(P(0, 1), "y")
		b.BeginGroup()
		b.EndGroup()
		b.Undo()
		if got := b.String(); got != "x" {
			t.Errorf("String() = %q, want x", got)
		}
	})
	t.Run("unbalanced EndGroup is harmless", func(t *testing.T) {
		b := New("")
		b.EndGroup()
		b.EndGroup()
		b.BeginGroup()
		b.Insert(P(0, 0), "a")
		b.Insert(P(0, 1), "b")
		b.EndGroup()
		b.Undo()
		if got := b.String(); got != "" {
			t.Errorf("String() = %q, want empty", got)
		}
	})
	t.Run("undo inside open group commits it", func(t *testing.T) {
		b := New("")
		b.BeginGroup()
		b.Insert(P(0, 0), "a")
		b.Insert(P(0, 1), "b")
		if _, ok := b.Undo(); !ok {
			t.Fatal("Undo() ok = false")
		}
		if got := b.String(); got != "" {
			t.Fatalf("String() = %q, want empty", got)
		}
		b.Insert(P(0, 0), "c")
		b.Insert(P(0, 1), "d")
		b.EndGroup()
		b.Undo()
		if got := b.String(); got != "" {
			t.Errorf("String() = %q, want empty", got)
		}
	})
}

func TestRedo(t *testing.T) {
	b := New("one\n")
	b.Insert(P(0, 3), " two")
	b.Delete(R(0, 0, 0, 4))
	if b.String() != "two\n" {
		t.Fatalf("String() = %q", b.String())
	}
	b.Undo()
	b.Undo()
	if b.String() != "one\n" {
		t.Fatalf("after undos String() = %q", b.String())
	}
	pos, ok := b.Redo()
	if !ok || b.String() != "one two\n" || pos != P(0, 3) {
		t.Fatalf("Redo() = %v, %v; String() = %q", pos, ok, b.String())
	}
	if b.Cursor() != pos {
		t.Errorf("Cursor() = %v, want %v", b.Cursor(), pos)
	}
	pos, ok = b.Redo()
	if !ok || b.String() != "two\n" || pos != P(0, 0) {
		t.Fatalf("Redo() = %v, %v; String() = %q", pos, ok, b.String())
	}
	if _, ok := b.Redo(); ok {
		t.Error("Redo() past end ok = true")
	}
}

func TestNewEditClearsRedo(t *testing.T) {
	b := New("a")
	b.Insert(P(0, 1), "b")
	b.Undo()
	b.Insert(P(0, 1), "c")
	if _, ok := b.Redo(); ok {
		t.Error("Redo() after new edit ok = true")
	}
	if got := b.String(); got != "ac" {
		t.Errorf("String() = %q, want ac", got)
	}
}

func TestDirty(t *testing.T) {
	b := New("a")
	if b.Dirty() {
		t.Fatal("fresh buffer is dirty")
	}
	b.Insert(P(0, 1), "b")
	if !b.Dirty() {
		t.Fatal("not dirty after edit")
	}
	b.Undo()
	if b.Dirty() {
		t.Fatal("dirty after undoing back to initial state")
	}
	b.Redo()
	if !b.Dirty() {
		t.Fatal("not dirty after redo")
	}
	b.MarkSaved()
	if b.Dirty() {
		t.Fatal("dirty after MarkSaved")
	}
	b.Insert(P(0, 2), "c")
	b.Insert(P(0, 3), "d")
	if !b.Dirty() {
		t.Fatal("not dirty after edits past save")
	}
	b.Undo()
	if !b.Dirty() {
		t.Fatal("clean one step away from saved")
	}
	b.Undo()
	if b.Dirty() {
		t.Fatalf("dirty after undoing back to saved version (%q)", b.String())
	}
	b.Undo()
	if !b.Dirty() {
		t.Fatal("clean after undoing past saved version")
	}
	b.Redo()
	if b.Dirty() {
		t.Fatal("dirty after redoing back to saved version")
	}
}

func TestDirtySavedStateLostAfterBranch(t *testing.T) {
	b := New("a")
	b.Insert(P(0, 1), "b")
	b.MarkSaved()
	b.Undo()
	b.Insert(P(0, 1), "c") // saved state is now unreachable
	if !b.Dirty() {
		t.Fatal("not dirty after diverging edit")
	}
	b.Undo()
	if !b.Dirty() {
		t.Error("clean at a state that differs from the saved one")
	}
}

func TestDirtyInsideGroup(t *testing.T) {
	b := New("")
	b.BeginGroup()
	b.Insert(P(0, 0), "a")
	if !b.Dirty() {
		t.Error("not dirty mid-group")
	}
	b.EndGroup()
}

func TestVersionIncrementsOnUndoRedo(t *testing.T) {
	b := New("a")
	b.Insert(P(0, 1), "b")
	v := b.Version()
	b.Undo()
	if b.Version() <= v {
		t.Fatalf("Version after Undo = %d, want > %d", b.Version(), v)
	}
	v = b.Version()
	b.Redo()
	if b.Version() <= v {
		t.Fatalf("Version after Redo = %d, want > %d", b.Version(), v)
	}
	v = b.Version()
	b.Redo() // nothing to redo
	if b.Version() != v {
		t.Errorf("Version changed on empty Redo")
	}
}

func TestFirstChangedLineUndoRedo(t *testing.T) {
	b := New("l0\nl1\nl2\nl3")
	b.Insert(P(2, 0), "x")
	b.ResetChanged()
	b.Undo()
	if got := b.FirstChangedLine(); got != 2 {
		t.Errorf("after Undo FirstChangedLine() = %d, want 2", got)
	}
	b.ResetChanged()
	b.Redo()
	if got := b.FirstChangedLine(); got != 2 {
		t.Errorf("after Redo FirstChangedLine() = %d, want 2", got)
	}
}

func TestSetTextUndoable(t *testing.T) {
	tests := []struct {
		name    string
		initial string
		text    string
	}{
		{"content change", "a\nb\nc\n", "x\ny"},
		{"only trailing newline added", "abc", "abc\n"},
		{"only trailing newline removed", "abc\n", "abc"},
		{"to empty", "abc\ndef\n", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := New(tt.initial)
			b.SetText(tt.text)
			want := normalizeNewlines(tt.text)
			if got := b.String(); got != want {
				t.Fatalf("String() = %q, want %q", got, want)
			}
			if !b.Dirty() {
				t.Error("not dirty after SetText")
			}
			if _, ok := b.Undo(); !ok {
				t.Fatal("Undo() ok = false")
			}
			if got := b.String(); got != tt.initial {
				t.Errorf("after undo String() = %q, want %q", got, tt.initial)
			}
			if b.Dirty() {
				t.Error("dirty after undoing SetText")
			}
			b.Redo()
			if got := b.String(); got != want {
				t.Errorf("after redo String() = %q, want %q", got, want)
			}
		})
	}
}

// TestUndoRedoRandomWalk applies a scripted mix of edits, then checks that
// undoing everything restores the original and redoing everything restores
// the final text.
func TestUndoRedoRandomWalk(t *testing.T) {
	initial := "first " + thumbsUp + "\nsecond " + eAcute + "\n" + cjk + " third\n"
	b := New(initial)
	b.Insert(P(1, 3), "INS\nERT")
	b.Delete(R(0, 2, 1, 1))
	b.BeginGroup()
	b.Replace(R(2, 0, 2, 1), "日")
	b.Insert(P(0, 0), "\n")
	b.EndGroup()
	b.SetText("brand new\r\n")
	b.Insert(P(0, 5), "́")
	b.Delete(R(0, 0, 9, 9))
	final := b.String()

	steps := 0
	for {
		if _, ok := b.Undo(); !ok {
			break
		}
		steps++
	}
	if steps != 6 {
		t.Errorf("undo steps = %d, want 6", steps)
	}
	if got := b.String(); got != initial {
		t.Fatalf("after undoing all String() = %q, want %q", got, initial)
	}
	for {
		if _, ok := b.Redo(); !ok {
			break
		}
	}
	if got := b.String(); got != final {
		t.Errorf("after redoing all String() = %q, want %q", got, final)
	}
}

// TestUndoRedoRandomized runs seeded random edits (including emoji,
// combining marks and newlines that merge or split clusters at edit
// boundaries) and checks every intermediate state is restored exactly by
// undo and reproduced by redo.
func TestUndoRedoRandomized(t *testing.T) {
	pieces := []string{"a", "xy", "\n", thumbsUp, "\u0301", eAcute, "日", "\r\n", "", "a\nb"}
	for seed := uint64(1); seed <= 20; seed++ {
		r := rand.New(rand.NewPCG(seed, 0))
		b := New("start " + cjk + "\nline two\n")
		states := []string{b.String()}
		randPos := func() Pos { return P(r.IntN(b.LineCount()+1), r.IntN(12)) }
		for range 60 {
			v := b.Version()
			switch r.IntN(4) {
			case 0:
				b.Insert(randPos(), pieces[r.IntN(len(pieces))])
			case 1:
				b.Delete(Range{Start: randPos(), End: randPos()})
			case 2:
				b.Replace(Range{Start: randPos(), End: randPos()}, pieces[r.IntN(len(pieces))])
			case 3:
				b.BeginGroup()
				b.Insert(randPos(), pieces[r.IntN(len(pieces))])
				b.Delete(Range{Start: randPos(), End: randPos()})
				b.EndGroup()
			}
			if b.Version() != v {
				states = append(states, b.String())
			}
		}
		for i := len(states) - 1; i > 0; i-- {
			if _, ok := b.Undo(); !ok {
				t.Fatalf("seed %d: Undo() ran out at state %d", seed, i)
			}
			if got := b.String(); got != states[i-1] {
				t.Fatalf("seed %d: undo to state %d = %q, want %q", seed, i-1, got, states[i-1])
			}
		}
		if b.Dirty() {
			t.Fatalf("seed %d: dirty after undoing everything", seed)
		}
		for i := 1; i < len(states); i++ {
			b.Redo()
			if got := b.String(); got != states[i] {
				t.Fatalf("seed %d: redo to state %d = %q, want %q", seed, i, got, states[i])
			}
		}
	}
}
