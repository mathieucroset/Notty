package merge

import (
	"fmt"
	"math/rand"
	"slices"
	"strings"
	"testing"
)

func blk(k BlockKind, base, ours, theirs string) Block {
	return Block{Kind: k, Base: lines(base), Ours: lines(ours), Theirs: lines(theirs)}
}

func fmtBlocks(bs []Block) string {
	var sb strings.Builder
	for _, b := range bs {
		fmt.Fprintf(&sb, "%v{base=%q ours=%q theirs=%q} ",
			b.Kind, strings.Join(b.Base, ""), strings.Join(b.Ours, ""), strings.Join(b.Theirs, ""))
	}
	return sb.String()
}

func blocksEqual(a, b []Block) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Kind != b[i].Kind || !slices.Equal(a[i].Base, b[i].Base) ||
			!slices.Equal(a[i].Ours, b[i].Ours) || !slices.Equal(a[i].Theirs, b[i].Theirs) {
			return false
		}
	}
	return true
}

func pickOurs(_ int, b Block) []string { return b.Ours }

func TestDiff3(t *testing.T) {
	tests := []struct {
		name               string
		base, ours, theirs string
		want               []Block
		result             string // Resolve with conflicts taking ours
	}{
		{"all empty", "", "", "", nil, ""},
		{"unchanged", "abc", "abc", "abc", []Block{blk(Stable, "abc", "abc", "abc")}, "abc"},
		{"non-overlapping edits", "abcde", "aXcde", "abcdY", []Block{
			blk(Stable, "a", "a", "a"),
			blk(Ours, "b", "X", "b"),
			blk(Stable, "cd", "cd", "cd"),
			blk(Theirs, "e", "e", "Y"),
		}, "aXcdY"},
		{"adjacent edits do not conflict", "abcd", "aXcd", "abYd", []Block{
			blk(Stable, "a", "a", "a"),
			blk(Ours, "b", "X", "b"),
			blk(Theirs, "c", "c", "Y"),
			blk(Stable, "d", "d", "d"),
		}, "aXYd"},
		{"overlapping different edits conflict", "abc", "aXc", "aYc", []Block{
			blk(Stable, "a", "a", "a"),
			blk(Conflict, "b", "X", "Y"),
			blk(Stable, "c", "c", "c"),
		}, "aXc"},
		{"identical edits are Both", "abc", "aXc", "aXc", []Block{
			blk(Stable, "a", "a", "a"),
			blk(Both, "b", "X", "X"),
			blk(Stable, "c", "c", "c"),
		}, "aXc"},
		{"deletion on one side", "abc", "ac", "abc", []Block{
			blk(Stable, "a", "a", "a"),
			blk(Ours, "b", "", "b"),
			blk(Stable, "c", "c", "c"),
		}, "ac"},
		{"deletion on theirs", "abc", "abc", "ab", []Block{
			blk(Stable, "ab", "ab", "ab"),
			blk(Theirs, "c", "c", ""),
		}, "ab"},
		{"addition at end by one side", "ab", "ab", "abcd", []Block{
			blk(Stable, "ab", "ab", "ab"),
			blk(Theirs, "", "", "cd"),
		}, "abcd"},
		{"addition at start by one side", "ab", "Xab", "ab", []Block{
			blk(Ours, "", "X", ""),
			blk(Stable, "ab", "ab", "ab"),
		}, "Xab"},
		{"different additions at end conflict", "ab", "abX", "abY", []Block{
			blk(Stable, "ab", "ab", "ab"),
			blk(Conflict, "", "X", "Y"),
		}, "abX"},
		{"identical additions at end are Both", "ab", "abX", "abX", []Block{
			blk(Stable, "ab", "ab", "ab"),
			blk(Both, "", "X", "X"),
		}, "abX"},
		{"different insertions at same position conflict", "ac", "abc", "axc", []Block{
			blk(Stable, "a", "a", "a"),
			blk(Conflict, "", "b", "x"),
			blk(Stable, "c", "c", "c"),
		}, "abc"},
		{"delete vs modify conflicts", "abc", "ac", "aXc", []Block{
			blk(Stable, "a", "a", "a"),
			blk(Conflict, "b", "", "X"),
			blk(Stable, "c", "c", "c"),
		}, "ac"},
		{"both delete same line", "abc", "ac", "ac", []Block{
			blk(Stable, "a", "a", "a"),
			blk(Both, "b", "", ""),
			blk(Stable, "c", "c", "c"),
		}, "ac"},
		{"empty base both add differently", "", "ab", "cd", []Block{
			blk(Conflict, "", "ab", "cd"),
		}, "ab"},
		{"empty base both add identically", "", "ab", "ab", []Block{
			blk(Both, "", "ab", "ab"),
		}, "ab"},
		{"empty base one side adds", "", "ab", "", []Block{
			blk(Ours, "", "ab", ""),
		}, "ab"},
		{"insertion after other's modification", "abc", "aXc", "abYc", []Block{
			blk(Stable, "a", "a", "a"),
			blk(Ours, "b", "X", "b"),
			blk(Theirs, "", "", "Y"),
			blk(Stable, "c", "c", "c"),
		}, "aXYc"},
		{"insertion before other's modification", "abc", "aXc", "aYbc", []Block{
			blk(Stable, "a", "a", "a"),
			blk(Theirs, "", "", "Y"),
			blk(Ours, "b", "X", "b"),
			blk(Stable, "c", "c", "c"),
		}, "aYXc"},
		{"insertion inside other's change conflicts", "abcd", "aXYd", "abZcd", []Block{
			blk(Stable, "a", "a", "a"),
			blk(Conflict, "bc", "XY", "bZc"),
			blk(Stable, "d", "d", "d"),
		}, "aXYd"},
		{"chained overlaps form one conflict", "abcdef", "aXYdef", "abZWef", []Block{
			blk(Stable, "a", "a", "a"),
			blk(Conflict, "bcd", "XYd", "bZW"),
			blk(Stable, "ef", "ef", "ef"),
		}, "aXYdef"},
		{"both delete everything", "abc", "", "", []Block{
			blk(Both, "abc", "", ""),
		}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Diff3(lines(tt.base), lines(tt.ours), lines(tt.theirs))
			if !blocksEqual(got, tt.want) {
				t.Fatalf("Diff3\n got %s\nwant %s", fmtBlocks(got), fmtBlocks(tt.want))
			}
			wantConflict := false
			for _, b := range tt.want {
				wantConflict = wantConflict || b.Kind == Conflict
			}
			if HasConflicts(got) != wantConflict {
				t.Fatalf("HasConflicts = %v, want %v", !wantConflict, wantConflict)
			}
			if res := strings.Join(Resolve(got, pickOurs), ""); res != tt.result {
				t.Fatalf("Resolve = %q, want %q", res, tt.result)
			}
		})
	}
}

func TestResolveChoose(t *testing.T) {
	blocks := Diff3(lines("abcde"), lines("aXcdP"), lines("aYcdQ"))
	var seen []int
	got := Resolve(blocks, func(i int, b Block) []string {
		seen = append(seen, i)
		if b.Kind != Conflict {
			t.Fatalf("choose called for %v", b.Kind)
		}
		// keep both: ours then theirs
		return append(slices.Clone(b.Ours), b.Theirs...)
	})
	if s := strings.Join(got, ""); s != "aXYcdPQ" {
		t.Fatalf("Resolve = %q", s)
	}
	if !slices.Equal(seen, []int{1, 3}) {
		t.Fatalf("choose indices = %v, want [1 3]", seen)
	}
}

func concat(bs []Block, f func(Block) []string) []string {
	var out []string
	for _, b := range bs {
		out = append(out, f(b)...)
	}
	return out
}

func eq(a, b []string) bool { return slices.Equal(a, b) }

func checkBlockInvariants(t *testing.T, base, ours, theirs []string, bs []Block) {
	t.Helper()
	ctx := func() string {
		return fmt.Sprintf("base=%v ours=%v theirs=%v -> %s", base, ours, theirs, fmtBlocks(bs))
	}
	if !eq(concat(bs, func(b Block) []string { return b.Base }), base) ||
		!eq(concat(bs, func(b Block) []string { return b.Ours }), ours) ||
		!eq(concat(bs, func(b Block) []string { return b.Theirs }), theirs) {
		t.Fatalf("blocks do not reconstruct inputs: %s", ctx())
	}
	for i, b := range bs {
		var ok bool
		switch b.Kind {
		case Stable:
			ok = len(b.Base) > 0 && eq(b.Base, b.Ours) && eq(b.Base, b.Theirs)
			if i > 0 && bs[i-1].Kind == Stable {
				ok = false
			}
		case Ours:
			ok = !eq(b.Ours, b.Base) && eq(b.Theirs, b.Base)
		case Theirs:
			ok = !eq(b.Theirs, b.Base) && eq(b.Ours, b.Base)
		case Both:
			ok = !eq(b.Ours, b.Base) && eq(b.Ours, b.Theirs)
		case Conflict:
			ok = !eq(b.Ours, b.Base) && !eq(b.Theirs, b.Base) && !eq(b.Ours, b.Theirs)
		default:
			ok = false
		}
		if !ok {
			t.Fatalf("block %d violates %v semantics: %s", i, b.Kind, ctx())
		}
	}
}

func TestDiff3Property(t *testing.T) {
	r := rand.New(rand.NewSource(3))
	for iter := 0; iter < 2000; iter++ {
		base := randLines(r, 12, "abc")
		other := randLines(r, 12, "abc")
		third := randLines(r, 12, "abc")

		// ours == base: result is theirs, no conflicts.
		bs := Diff3(base, base, other)
		checkBlockInvariants(t, base, base, other, bs)
		if HasConflicts(bs) || !eq(Resolve(bs, pickOurs), other) {
			t.Fatalf("ours==base: base=%v theirs=%v -> %s", base, other, fmtBlocks(bs))
		}
		// theirs == base: result is ours, no conflicts.
		bs = Diff3(base, other, base)
		checkBlockInvariants(t, base, other, base, bs)
		if HasConflicts(bs) || !eq(Resolve(bs, pickOurs), other) {
			t.Fatalf("theirs==base: base=%v ours=%v -> %s", base, other, fmtBlocks(bs))
		}
		// ours == theirs: result is ours, no conflicts.
		bs = Diff3(base, other, other)
		checkBlockInvariants(t, base, other, other, bs)
		if HasConflicts(bs) || !eq(Resolve(bs, pickOurs), other) {
			t.Fatalf("ours==theirs: base=%v ours=%v -> %s", base, other, fmtBlocks(bs))
		}
		// arbitrary triple: structural invariants hold, and choosing the
		// same side everywhere yields a deterministic result.
		bs = Diff3(base, other, third)
		checkBlockInvariants(t, base, other, third, bs)
		if !blocksEqual(bs, Diff3(base, other, third)) {
			t.Fatal("Diff3 not deterministic")
		}
	}
}

func TestDiff2(t *testing.T) {
	tests := []struct {
		name         string
		ours, theirs string
		want         []Block
	}{
		{"both empty", "", "", nil},
		{"identical", "abc", "abc", []Block{blk(Stable, "", "abc", "abc")}},
		{"entirely different", "ab", "xy", []Block{blk(Conflict, "", "ab", "xy")}},
		{"one side empty", "ab", "", []Block{blk(Conflict, "", "ab", "")}},
		{"interleaved", "abcde", "aXcYe", []Block{
			blk(Stable, "", "a", "a"),
			blk(Conflict, "", "b", "X"),
			blk(Stable, "", "c", "c"),
			blk(Conflict, "", "d", "Y"),
			blk(Stable, "", "e", "e"),
		}},
		{"addition only in theirs", "ac", "abc", []Block{
			blk(Stable, "", "a", "a"),
			blk(Conflict, "", "", "b"),
			blk(Stable, "", "c", "c"),
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Diff2(lines(tt.ours), lines(tt.theirs))
			if !blocksEqual(got, tt.want) {
				t.Fatalf("Diff2\n got %s\nwant %s", fmtBlocks(got), fmtBlocks(tt.want))
			}
			for _, b := range got {
				if b.Base != nil {
					t.Fatalf("Diff2 block has non-nil Base: %v", b)
				}
			}
			theirs := Resolve(got, func(_ int, b Block) []string { return b.Theirs })
			if !eq(theirs, lines(tt.theirs)) {
				t.Fatalf("Resolve(theirs) = %v", theirs)
			}
		})
	}
}

func TestBlockSlicesAreCapped(t *testing.T) {
	ours := lines("abcd")
	bs := Diff2(ours, lines("aXcd"))
	_ = append(bs[0].Ours, "Z") // must not clobber ours[1]
	if ours[1] != "b" {
		t.Fatalf("appending to a block slice modified the input: %v", ours)
	}
}
