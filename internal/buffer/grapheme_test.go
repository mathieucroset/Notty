package buffer

import (
	"slices"
	"testing"
)

const (
	thumbsUp = "👍🏽"      // U+1F44D U+1F3FD: one cluster, width 2
	eAcute   = "e\u0301" // e + combining acute: one cluster, width 1
	cjk      = "日本"      // two clusters, width 2 each
)

func TestGraphemes(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{"empty", "", nil},
		{"ascii", "abc", []string{"a", "b", "c"}},
		{"emoji with skin tone", "a" + thumbsUp + "b", []string{"a", thumbsUp, "b"}},
		{"combining accent", eAcute + "x", []string{eAcute, "x"}},
		{"cjk", cjk, []string{"日", "本"}},
		{"zwj family", "👨‍👩‍👧", []string{"👨‍👩‍👧"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Graphemes(tt.in)
			if !slices.Equal(got, tt.want) {
				t.Errorf("Graphemes(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestDisplayWidth(t *testing.T) {
	tests := []struct {
		in   string
		want int
	}{
		{"", 0},
		{"abc", 3},
		{thumbsUp, 2},
		{eAcute, 1},
		{cjk, 4},
		{"a日b", 4},
	}
	for _, tt := range tests {
		if got := DisplayWidth(tt.in); got != tt.want {
			t.Errorf("DisplayWidth(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func TestColToByte(t *testing.T) {
	s := "a" + thumbsUp + eAcute + "日" // bytes: a=1, thumbs=8, eAcute=3, 日=3
	tests := []struct {
		name string
		s    string
		col  int
		want int
	}{
		{"negative", s, -1, 0},
		{"zero", s, 0, 0},
		{"after ascii", s, 1, 1},
		{"after emoji", s, 2, 9},
		{"after combining", s, 3, 12},
		{"at end", s, 4, 15},
		{"past end", s, 99, 15},
		{"empty string", "", 3, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ColToByte(tt.s, tt.col); got != tt.want {
				t.Errorf("ColToByte(%q, %d) = %d, want %d", tt.s, tt.col, got, tt.want)
			}
		})
	}
}

func TestByteToCol(t *testing.T) {
	s := "a" + thumbsUp + eAcute + "日"
	tests := []struct {
		name string
		off  int
		want int
	}{
		{"negative", -5, 0},
		{"zero", 0, 0},
		{"cluster boundary", 1, 1},
		{"inside emoji floors", 4, 1},
		{"after emoji", 9, 2},
		{"inside combining floors", 10, 2},
		{"after combining", 12, 3},
		{"at end", 15, 4},
		{"past end", 100, 4},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ByteToCol(s, tt.off); got != tt.want {
				t.Errorf("ByteToCol(%q, %d) = %d, want %d", s, tt.off, got, tt.want)
			}
		})
	}
}

func TestColByteRoundTrip(t *testing.T) {
	s := "x" + thumbsUp + eAcute + cjk + "👨‍👩‍👧z"
	n := len(Graphemes(s))
	for col := 0; col <= n; col++ {
		if got := ByteToCol(s, ColToByte(s, col)); got != col {
			t.Errorf("round trip col %d -> %d", col, got)
		}
	}
}
