package merge

import (
	"math/rand"
	"reflect"
	"testing"
)

func eqL(text string, a, b int) DiffLine {
	return DiffLine{Kind: Equal, Text: text, ALine: a, BLine: b}
}
func delL(text string, a int) DiffLine { return DiffLine{Kind: Delete, Text: text, ALine: a} }
func insL(text string, b int) DiffLine { return DiffLine{Kind: Insert, Text: text, BLine: b} }
func sepL() DiffLine                   { return DiffLine{Kind: Equal, Text: "…"} }

func TestUnified(t *testing.T) {
	tests := []struct {
		name    string
		a, b    string
		context int
		want    []DiffLine
	}{
		{"both empty", "", "", 3, nil},
		{"identical", "abc", "abc", 3, nil},
		{"single change with context", "abcdefghij", "abcdeXghij", 2, []DiffLine{
			eqL("d", 4, 4), eqL("e", 5, 5), delL("f", 6), insL("X", 6), eqL("g", 7, 7), eqL("h", 8, 8),
		}},
		{"context larger than file", "abc", "aXc", 5, []DiffLine{
			eqL("a", 1, 1), delL("b", 2), insL("X", 2), eqL("c", 3, 3),
		}},
		{"zero context", "abc", "aXc", 0, []DiffLine{delL("b", 2), insL("X", 2)}},
		{"negative context acts as zero", "abc", "aXc", -1, []DiffLine{delL("b", 2), insL("X", 2)}},
		{"distant changes are separate hunks", "abcdefghijklmn", "aBcdefghijklMn", 1, []DiffLine{
			eqL("a", 1, 1), delL("b", 2), insL("B", 2), eqL("c", 3, 3),
			sepL(),
			eqL("l", 12, 12), delL("m", 13), insL("M", 13), eqL("n", 14, 14),
		}},
		{"gap wider than twice context splits", "abcdefg", "aBcdeFg", 1, []DiffLine{
			eqL("a", 1, 1), delL("b", 2), insL("B", 2), eqL("c", 3, 3),
			sepL(),
			eqL("e", 5, 5), delL("f", 6), insL("F", 6), eqL("g", 7, 7),
		}},
		{"gap equal to twice context merges", "abcdef", "aBcdEf", 1, []DiffLine{
			eqL("a", 1, 1), delL("b", 2), insL("B", 2), eqL("c", 3, 3), eqL("d", 4, 4),
			delL("e", 5), insL("E", 5), eqL("f", 6, 6),
		}},
		{"insert at start", "ab", "Xab", 1, []DiffLine{insL("X", 1), eqL("a", 1, 2)}},
		{"delete at end", "abc", "ab", 1, []DiffLine{eqL("b", 2, 2), delL("c", 3)}},
		{"all new", "", "ab", 3, []DiffLine{insL("a", 1), insL("b", 2)}},
		{"shifted line numbers", "abcdefgh", "XYabcdefgZ", 1, []DiffLine{
			insL("X", 1), insL("Y", 2), eqL("a", 1, 3),
			sepL(),
			eqL("g", 7, 9), delL("h", 8), insL("Z", 10),
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Unified(lines(tt.a), lines(tt.b), tt.context)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("Unified(%q, %q, %d)\n got %v\nwant %v", tt.a, tt.b, tt.context, got, tt.want)
			}
		})
	}
}

// With unlimited context, Unified lists every line of a and b in order.
func TestUnifiedFullContextProperty(t *testing.T) {
	r := rand.New(rand.NewSource(4))
	for iter := 0; iter < 500; iter++ {
		a := randLines(r, 15, "abc")
		b := randLines(r, 15, "abc")
		var gotA, gotB []string
		for _, l := range Unified(a, b, len(a)+len(b)) {
			if l.ALine != 0 {
				if l.ALine != len(gotA)+1 || a[l.ALine-1] != l.Text {
					t.Fatalf("bad a line %v for a=%v b=%v", l, a, b)
				}
				gotA = append(gotA, l.Text)
			}
			if l.BLine != 0 {
				if l.BLine != len(gotB)+1 || b[l.BLine-1] != l.Text {
					t.Fatalf("bad b line %v for a=%v b=%v", l, a, b)
				}
				gotB = append(gotB, l.Text)
			}
		}
		if !eq(a, b) && (!eq(gotA, a) || !eq(gotB, b)) {
			t.Fatalf("full-context Unified lost lines: a=%v b=%v", a, b)
		}
	}
}
