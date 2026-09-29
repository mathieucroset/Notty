// Package merge provides line diffs (Myers) and block-level three-way
// merges (diff3) used by the conflict resolver and the history view.
package merge

import "strings"

// OpKind is the kind of an edit operation.
type OpKind int

const (
	// Equal lines are present in both a and b.
	Equal OpKind = iota
	// Insert lines are present only in b.
	Insert
	// Delete lines are present only in a.
	Delete
)

func (k OpKind) String() string {
	switch k {
	case Equal:
		return "Equal"
	case Insert:
		return "Insert"
	case Delete:
		return "Delete"
	}
	return "OpKind(?)"
}

// Op is one edit operation over half-open line ranges a[A0:A1] and b[B0:B1].
// Equal ops have equal-length ranges, Insert ops have A0 == A1, and Delete
// ops have B0 == B1.
type Op struct {
	Kind   OpKind
	A0, A1 int
	B0, B1 int
}

// Diff returns a minimal edit script turning a into b, computed with the
// linear-space variant of Myers' O(ND) algorithm. Consecutive ops of the same
// kind are coalesced; within a changed region, the Delete comes before the
// Insert. The result is deterministic and nil when both inputs are empty.
func Diff(a, b []string) []Op {
	// Intern lines so the core loop compares ints.
	ids := make(map[string]int, len(a)+len(b))
	intern := func(ls []string) []int {
		out := make([]int, len(ls))
		for i, l := range ls {
			id, ok := ids[l]
			if !ok {
				id = len(ids)
				ids[l] = id
			}
			out[i] = id
		}
		return out
	}
	d := &differ{
		a:  intern(a),
		b:  intern(b),
		ca: make([]bool, len(a)),
		cb: make([]bool, len(b)),
	}
	n := len(a) + len(b)
	d.vf = make([]int, n+3)
	d.vb = make([]int, n+3)
	d.compare(0, len(a), 0, len(b))
	return d.ops()
}

// differ marks changed lines in ca (deleted from a) and cb (inserted in b).
type differ struct {
	a, b   []int
	ca, cb []bool
	vf, vb []int // scratch V arrays, indexed by diagonal + offset
}

func (d *differ) compare(aLo, aHi, bLo, bHi int) {
	// Trim the common prefix and suffix.
	for aLo < aHi && bLo < bHi && d.a[aLo] == d.b[bLo] {
		aLo++
		bLo++
	}
	for aLo < aHi && bLo < bHi && d.a[aHi-1] == d.b[bHi-1] {
		aHi--
		bHi--
	}
	switch {
	case aLo == aHi:
		for j := bLo; j < bHi; j++ {
			d.cb[j] = true
		}
		return
	case bLo == bHi:
		for i := aLo; i < aHi; i++ {
			d.ca[i] = true
		}
		return
	}
	x, y, u, v := d.middleSnake(aLo, aHi, bLo, bHi)
	d.compare(aLo, x, bLo, y)
	d.compare(u, aHi, v, bHi)
}

// middleSnake finds the middle snake of the shortest edit path between
// a[aLo:aHi] and b[bLo:bHi] (Myers 1986, section 4b). It returns the snake's
// start (x, y) and end (u, v) in absolute coordinates. Both ranges must be
// non-empty and must differ in their first and last lines, which guarantees
// an edit distance of at least 2 and therefore strictly smaller subproblems.
func (d *differ) middleSnake(aLo, aHi, bLo, bHi int) (x, y, u, v int) {
	n, m := aHi-aLo, bHi-bLo
	delta := n - m
	odd := delta&1 != 0
	maxD := (n + m + 1) / 2
	off := maxD + 1
	vf, vb := d.vf, d.vb
	vf[off+1] = 0
	vb[off+1] = 0
	for dd := 0; dd <= maxD; dd++ {
		// Forward search: vf[k] is the furthest x on diagonal k = x - y.
		for k := -dd; k <= dd; k += 2 {
			var px int
			if k == -dd || (k != dd && vf[off+k-1] < vf[off+k+1]) {
				px = vf[off+k+1]
			} else {
				px = vf[off+k-1] + 1
			}
			py := px - k
			sx, sy := px, py
			for px < n && py < m && d.a[aLo+px] == d.b[bLo+py] {
				px++
				py++
			}
			vf[off+k] = px
			if odd {
				rk := delta - k // matching reverse diagonal
				if rk >= -(dd-1) && rk <= dd-1 && px+vb[off+rk] >= n {
					return aLo + sx, bLo + sy, aLo + px, bLo + py
				}
			}
		}
		// Reverse search over the reversed sequences: vb[k] is the furthest
		// number of lines consumed from the ends on reversed diagonal k.
		for k := -dd; k <= dd; k += 2 {
			var px int
			if k == -dd || (k != dd && vb[off+k-1] < vb[off+k+1]) {
				px = vb[off+k+1]
			} else {
				px = vb[off+k-1] + 1
			}
			py := px - k
			sx, sy := px, py
			for px < n && py < m && d.a[aHi-1-px] == d.b[bHi-1-py] {
				px++
				py++
			}
			vb[off+k] = px
			if !odd {
				fk := delta - k // matching forward diagonal
				if fk >= -dd && fk <= dd && vf[off+fk]+px >= n {
					return aHi - px, bHi - py, aHi - sx, bHi - sy
				}
			}
		}
	}
	// Unreachable: a path of length n+m always exists.
	return aLo, bLo, aHi, bHi
}

// ops converts the change marks into a coalesced edit script.
func (d *differ) ops() []Op {
	var out []Op
	emit := func(k OpKind, a0, a1, b0, b1 int) {
		out = append(out, Op{Kind: k, A0: a0, A1: a1, B0: b0, B1: b1})
	}
	i, j := 0, 0
	n, m := len(d.a), len(d.b)
	for i < n || j < m {
		switch {
		case i < n && d.ca[i]:
			s := i
			for i < n && d.ca[i] {
				i++
			}
			emit(Delete, s, i, j, j)
		case j < m && d.cb[j]:
			s := j
			for j < m && d.cb[j] {
				j++
			}
			emit(Insert, i, i, s, j)
		default:
			si, sj := i, j
			for i < n && j < m && !d.ca[i] && !d.cb[j] {
				i++
				j++
			}
			emit(Equal, si, i, sj, j)
		}
	}
	return out
}

// SplitLines splits s into lines without their "\n" terminators. A final
// newline does not start an extra empty line ("a\nb\n" -> ["a", "b"]); use
// HasTrailingNewline to preserve it. "\r" is kept as part of the line so
// JoinLines round-trips exactly. It returns nil for "".
func SplitLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(s, "\n"), "\n")
}

// HasTrailingNewline reports whether s ends with "\n".
func HasTrailingNewline(s string) bool {
	return strings.HasSuffix(s, "\n")
}

// JoinLines is the inverse of SplitLines: it joins lines with "\n" and
// appends a final "\n" when trailingNewline is set and there is any line.
func JoinLines(lines []string, trailingNewline bool) string {
	if len(lines) == 0 {
		return ""
	}
	s := strings.Join(lines, "\n")
	if trailingNewline {
		s += "\n"
	}
	return s
}
