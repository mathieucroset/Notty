package merge

import (
	"math/rand"
	"reflect"
	"strings"
	"testing"
)

func lines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "")
}

func TestDiff(t *testing.T) {
	tests := []struct {
		name string
		a, b string
		want []Op
	}{
		{"both empty", "", "", nil},
		{"identical", "abc", "abc", []Op{{Equal, 0, 3, 0, 3}}},
		{"all insert", "", "abc", []Op{{Insert, 0, 0, 0, 3}}},
		{"all delete", "abc", "", []Op{{Delete, 0, 3, 0, 0}}},
		{"replace all", "ab", "xy", []Op{{Delete, 0, 2, 0, 0}, {Insert, 2, 2, 0, 2}}},
		{"insert middle", "ac", "abc", []Op{{Equal, 0, 1, 0, 1}, {Insert, 1, 1, 1, 2}, {Equal, 1, 2, 2, 3}}},
		{"delete middle", "abc", "ac", []Op{{Equal, 0, 1, 0, 1}, {Delete, 1, 2, 1, 1}, {Equal, 2, 3, 1, 2}}},
		{"modify middle", "abc", "axc", []Op{{Equal, 0, 1, 0, 1}, {Delete, 1, 2, 1, 1}, {Insert, 2, 2, 1, 2}, {Equal, 2, 3, 2, 3}}},
		{"append", "ab", "abcd", []Op{{Equal, 0, 2, 0, 2}, {Insert, 2, 2, 2, 4}}},
		{"prepend", "cd", "abcd", []Op{{Insert, 0, 0, 0, 2}, {Equal, 0, 2, 2, 4}}},
		{"interleaved", "abcdef", "axcyef", []Op{
			{Equal, 0, 1, 0, 1}, {Delete, 1, 2, 1, 1}, {Insert, 2, 2, 1, 2}, {Equal, 2, 3, 2, 3},
			{Delete, 3, 4, 3, 3}, {Insert, 4, 4, 3, 4}, {Equal, 4, 6, 4, 6},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Diff(lines(tt.a), lines(tt.b))
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("Diff(%q, %q)\n got %v\nwant %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestDiffDeterministic(t *testing.T) {
	a := lines("abcabba")
	b := lines("cbabac")
	first := Diff(a, b)
	for i := 0; i < 10; i++ {
		if got := Diff(a, b); !reflect.DeepEqual(got, first) {
			t.Fatalf("run %d differs: %v vs %v", i, got, first)
		}
	}
}

// applyOps rebuilds b from a and the ops, validating their structure.
func applyOps(t *testing.T, a, b []string, ops []Op) []string {
	t.Helper()
	var out []string
	ai, bi := 0, 0
	for k, op := range ops {
		if op.A0 != ai || op.B0 != bi {
			t.Fatalf("op %d %v not contiguous (ai=%d bi=%d)", k, op, ai, bi)
		}
		if k > 0 && ops[k-1].Kind == op.Kind {
			t.Fatalf("op %d %v not coalesced with previous", k, op)
		}
		switch op.Kind {
		case Equal:
			if op.A1-op.A0 != op.B1-op.B0 || op.A1 == op.A0 {
				t.Fatalf("bad equal op %v", op)
			}
			for i := 0; i < op.A1-op.A0; i++ {
				if a[op.A0+i] != b[op.B0+i] {
					t.Fatalf("equal op %v covers differing lines", op)
				}
			}
			out = append(out, a[op.A0:op.A1]...)
		case Delete:
			if op.B0 != op.B1 || op.A1 <= op.A0 {
				t.Fatalf("bad delete op %v", op)
			}
		case Insert:
			if op.A0 != op.A1 || op.B1 <= op.B0 {
				t.Fatalf("bad insert op %v", op)
			}
			out = append(out, b[op.B0:op.B1]...)
		default:
			t.Fatalf("unknown kind in %v", op)
		}
		ai, bi = op.A1, op.B1
	}
	if ai != len(a) || bi != len(b) {
		t.Fatalf("ops end at (%d,%d), want (%d,%d)", ai, bi, len(a), len(b))
	}
	return out
}

func lcsLen(a, b []string) int {
	dp := make([][]int, len(a)+1)
	for i := range dp {
		dp[i] = make([]int, len(b)+1)
	}
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			if a[i] == b[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else {
				dp[i][j] = max(dp[i+1][j], dp[i][j+1])
			}
		}
	}
	return dp[0][0]
}

func randLines(r *rand.Rand, maxLen int, alphabet string) []string {
	n := r.Intn(maxLen + 1)
	out := make([]string, n)
	for i := range out {
		out[i] = string(alphabet[r.Intn(len(alphabet))])
	}
	return out
}

func TestDiffProperty(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	for iter := 0; iter < 2000; iter++ {
		a := randLines(r, 20, "abcd")
		b := randLines(r, 20, "abcd")
		ops := Diff(a, b)
		got := applyOps(t, a, b, ops)
		if len(got) == 0 && len(b) == 0 {
			continue
		}
		if !reflect.DeepEqual(got, b) {
			t.Fatalf("apply(Diff(%v,%v)) = %v", a, b, got)
		}
		edits := 0
		for _, op := range ops {
			switch op.Kind {
			case Delete:
				edits += op.A1 - op.A0
			case Insert:
				edits += op.B1 - op.B0
			}
		}
		if want := len(a) + len(b) - 2*lcsLen(a, b); edits != want {
			t.Fatalf("Diff(%v,%v) has %d edits, minimal is %d", a, b, edits, want)
		}
	}
}

func TestDiffLarge(t *testing.T) {
	r := rand.New(rand.NewSource(2))
	a := randLines(r, 3000, "abcdefghij")
	b := randLines(r, 3000, "abcdefghij")
	if got := applyOps(t, a, b, Diff(a, b)); !reflect.DeepEqual(got, b) && len(b) > 0 {
		t.Fatal("large diff does not reproduce b")
	}
}

func TestSplitLines(t *testing.T) {
	tests := []struct {
		in       string
		want     []string
		trailing bool
	}{
		{"", nil, false},
		{"a", []string{"a"}, false},
		{"a\n", []string{"a"}, true},
		{"a\nb\n", []string{"a", "b"}, true},
		{"a\nb", []string{"a", "b"}, false},
		{"\n", []string{""}, true},
		{"\n\n", []string{"", ""}, true},
		{"a\n\nb", []string{"a", "", "b"}, false},
		{"a\r\nb\r\n", []string{"a\r", "b\r"}, true},
	}
	for _, tt := range tests {
		got := SplitLines(tt.in)
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("SplitLines(%q) = %q, want %q", tt.in, got, tt.want)
		}
		if tr := HasTrailingNewline(tt.in); tr != tt.trailing {
			t.Errorf("HasTrailingNewline(%q) = %v, want %v", tt.in, tr, tt.trailing)
		}
		if back := JoinLines(got, HasTrailingNewline(tt.in)); back != tt.in {
			t.Errorf("JoinLines(SplitLines(%q)) = %q", tt.in, back)
		}
	}
}
