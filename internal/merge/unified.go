package merge

// HunkSeparator is the Text of the entry Unified places between hunks.
const HunkSeparator = "…"

// DiffLine is one rendered line of a unified diff. ALine and BLine are
// 1-based line numbers in a and b, 0 when the line is absent from that side
// (and both 0 for a hunk separator).
type DiffLine struct {
	Kind  OpKind
	Text  string
	ALine int
	BLine int
}

// Unified renders the diff from a to b as hunks with up to context
// unchanged lines around each change. Changes separated by at most
// 2*context unchanged lines share a hunk; between hunks it inserts a
// separator entry (Kind Equal, Text HunkSeparator, line numbers 0). It
// returns nil when a and b are equal. A negative context is treated as 0.
func Unified(a, b []string, context int) []DiffLine {
	context = max(context, 0)
	var all []DiffLine
	for _, op := range Diff(a, b) {
		switch op.Kind {
		case Equal:
			for i := 0; i < op.A1-op.A0; i++ {
				all = append(all, DiffLine{Kind: Equal, Text: a[op.A0+i], ALine: op.A0 + i + 1, BLine: op.B0 + i + 1})
			}
		case Delete:
			for i := op.A0; i < op.A1; i++ {
				all = append(all, DiffLine{Kind: Delete, Text: a[i], ALine: i + 1})
			}
		case Insert:
			for j := op.B0; j < op.B1; j++ {
				all = append(all, DiffLine{Kind: Insert, Text: b[j], BLine: j + 1})
			}
		}
	}

	// dist[i] is the distance from line i to the nearest change (0 for a
	// change); lines farther than context are hidden.
	inf := len(all) + 1
	dist := make([]int, len(all))
	d := inf
	for i, l := range all {
		if l.Kind != Equal {
			d = 0
		} else if d < inf {
			d++
		}
		dist[i] = d
	}
	anyChange := false
	d = inf
	for i := len(all) - 1; i >= 0; i-- {
		if all[i].Kind != Equal {
			d = 0
			anyChange = true
		} else if d < inf {
			d++
		}
		dist[i] = min(dist[i], d)
	}
	if !anyChange {
		return nil
	}

	var out []DiffLine
	gap := false
	for i, l := range all {
		if dist[i] > context {
			gap = true
			continue
		}
		if gap && len(out) > 0 {
			out = append(out, DiffLine{Kind: Equal, Text: HunkSeparator})
		}
		gap = false
		out = append(out, l)
	}
	return out
}
