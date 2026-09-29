package merge

// BlockKind classifies a block of a three-way (or two-way) merge.
type BlockKind int

const (
	// Stable lines are unchanged on both sides.
	Stable BlockKind = iota
	// Ours marks a region changed only on our side; ours is auto-applied.
	Ours
	// Theirs marks a region changed only on their side; theirs is auto-applied.
	Theirs
	// Both marks a region changed identically on both sides.
	Both
	// Conflict marks a region changed differently on both sides.
	Conflict
)

func (k BlockKind) String() string {
	switch k {
	case Stable:
		return "Stable"
	case Ours:
		return "Ours"
	case Theirs:
		return "Theirs"
	case Both:
		return "Both"
	case Conflict:
		return "Conflict"
	}
	return "BlockKind(?)"
}

// Block is one region of a merge. Base, Ours and Theirs hold that region's
// lines in each version, so concatenating a field over all blocks rebuilds
// the corresponding input (Base is nil for blocks from Diff2).
//
// The slices are sub-slices of the inputs, not copies. Where a side is
// unchanged, its field aliases the base slice rather than that side's input:
// in a Stable block from Diff3, Base, Ours and Theirs all share base's
// backing array; in an Ours block, Theirs is the Base slice; in a Theirs
// block, Ours is the Base slice. Changed sides (and every Diff2 field)
// alias their own input. Every slice is capacity-capped, so appending to
// one never overwrites an input, but callers must not modify elements in
// place if they still need the inputs intact.
type Block struct {
	Kind   BlockKind
	Base   []string
	Ours   []string
	Theirs []string
}

// hunk is a changed region: base[a0:a1] became side[s0:s1].
type hunk struct{ a0, a1, s0, s1 int }

func (h hunk) empty() bool { return h.a0 == h.a1 }

// hunks groups a diff's non-Equal ops into change regions.
func hunks(ops []Op) []hunk {
	var out []hunk
	for i := 0; i < len(ops); i++ {
		if ops[i].Kind == Equal {
			continue
		}
		h := hunk{ops[i].A0, ops[i].A1, ops[i].B0, ops[i].B1}
		for i+1 < len(ops) && ops[i+1].Kind != Equal {
			i++
			h.a1, h.s1 = ops[i].A1, ops[i].B1
		}
		out = append(out, h)
	}
	return out
}

// before reports whether hunk h must be considered before hunk g when both
// are candidates: lower base start first, and at equal starts a pure
// insertion goes before a change (it sits before that base line).
func before(h, g hunk) bool {
	if h.a0 != g.a0 {
		return h.a0 < g.a0
	}
	return h.empty() && !g.empty()
}

// overlaps reports whether hunk h collides with the region [lo, hi).
// Non-empty ranges collide when they share a base line; an insertion
// collides with a change it falls strictly inside, and with another
// insertion at the same position. Merely adjacent edits do not collide.
func overlaps(lo, hi int, h hunk) bool {
	if h.a0 < hi && lo < h.a1 {
		return true
	}
	return lo == hi && h.empty() && h.a0 == lo
}

func capped(s []string, lo, hi int) []string { return s[lo:hi:hi] }

// sideRange maps base[lo:hi] to the side's line range, given the side's
// hunks inside that region (outside them, lines map one-to-one).
func sideRange(lo, hi int, hs []hunk) (int, int) {
	first, last := hs[0], hs[len(hs)-1]
	return first.s0 - (first.a0 - lo), last.s1 + (hi - last.a1)
}

// Diff3 performs a block-level three-way merge of ours and theirs against
// base. Regions changed on one side only become Ours or Theirs blocks,
// identical changes become Both, and different changes to overlapping base
// regions (including different insertions at the same position) become
// Conflict. Edits that are merely adjacent merge cleanly.
//
// Diff3 works on lines only and knows nothing about the files' final
// newlines: reconciling them is the caller's job. Split each version with
// SplitLines, record HasTrailingNewline for each, pick the result's
// trailing newline (e.g. keep the base's unless exactly one side changed
// it), and write the result with JoinLines.
func Diff3(base, ours, theirs []string) []Block {
	ho := hunks(Diff(base, ours))
	ht := hunks(Diff(base, theirs))
	var out []Block
	pos, io, it := 0, 0, 0
	for io < len(ho) || it < len(ht) {
		var first hunk
		takeOurs := it == len(ht) || (io < len(ho) && !before(ht[it], ho[io]))
		if takeOurs {
			first = ho[io]
		} else {
			first = ht[it]
		}
		if pos < first.a0 {
			s := capped(base, pos, first.a0)
			out = append(out, Block{Kind: Stable, Base: s, Ours: s, Theirs: s})
		}
		lo, hi := first.a0, first.a1
		var so, st []hunk
		if takeOurs {
			so, io = append(so, first), io+1
		} else {
			st, it = append(st, first), it+1
		}
		for grew := true; grew; {
			grew = false
			if io < len(ho) && overlaps(lo, hi, ho[io]) {
				hi = max(hi, ho[io].a1)
				so, io, grew = append(so, ho[io]), io+1, true
			}
			if it < len(ht) && overlaps(lo, hi, ht[it]) {
				hi = max(hi, ht[it].a1)
				st, it, grew = append(st, ht[it]), it+1, true
			}
		}
		b := Block{Base: capped(base, lo, hi)}
		switch {
		case len(st) == 0:
			b.Kind, b.Ours, b.Theirs = Ours, capped(ours, first.s0, first.s1), b.Base
		case len(so) == 0:
			b.Kind, b.Ours, b.Theirs = Theirs, b.Base, capped(theirs, first.s0, first.s1)
		default:
			o0, o1 := sideRange(lo, hi, so)
			t0, t1 := sideRange(lo, hi, st)
			b.Ours, b.Theirs = capped(ours, o0, o1), capped(theirs, t0, t1)
			b.Kind = Conflict
			if equalLines(b.Ours, b.Theirs) {
				b.Kind = Both
			}
		}
		out = append(out, b)
		pos = hi
	}
	if pos < len(base) {
		s := capped(base, pos, len(base))
		out = append(out, Block{Kind: Stable, Base: s, Ours: s, Theirs: s})
	}
	return out
}

func equalLines(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Diff2 compares two versions that share no base (a file created on both
// sides). Equal runs become Stable blocks and differing runs become Conflict
// blocks; Base is nil throughout.
func Diff2(ours, theirs []string) []Block {
	var out []Block
	ops := Diff(ours, theirs)
	for i := 0; i < len(ops); i++ {
		op := ops[i]
		if op.Kind == Equal {
			out = append(out, Block{Kind: Stable, Ours: capped(ours, op.A0, op.A1), Theirs: capped(theirs, op.B0, op.B1)})
			continue
		}
		a0, a1, b0, b1 := op.A0, op.A1, op.B0, op.B1
		for i+1 < len(ops) && ops[i+1].Kind != Equal {
			i++
			a1, b1 = ops[i].A1, ops[i].B1
		}
		out = append(out, Block{Kind: Conflict, Ours: capped(ours, a0, a1), Theirs: capped(theirs, b0, b1)})
	}
	return out
}

// Resolve builds the merged lines. Non-conflict blocks use their
// auto-applied content (Stable and Both: the shared lines, Ours: ours,
// Theirs: theirs); for each Conflict block, choose is called with the
// block's index and the block, and its return value is used. The result is
// a fresh slice that never aliases the blocks or the inputs.
//
// Like Diff3, Resolve returns lines without terminators; the caller decides
// the trailing newline (see HasTrailingNewline) and writes with JoinLines.
func Resolve(blocks []Block, choose func(i int, b Block) []string) []string {
	out := []string{}
	for i, b := range blocks {
		switch b.Kind {
		case Theirs:
			out = append(out, b.Theirs...)
		case Conflict:
			out = append(out, choose(i, b)...)
		default: // Stable, Both, Ours
			out = append(out, b.Ours...)
		}
	}
	return out
}

// HasConflicts reports whether any block is a Conflict.
func HasConflicts(blocks []Block) bool {
	for _, b := range blocks {
		if b.Kind == Conflict {
			return true
		}
	}
	return false
}
