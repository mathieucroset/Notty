package search

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/mathieucroset/notty/internal/index"
)

func TestParseQuery(t *testing.T) {
	tests := []struct {
		in   string
		want Query
	}{
		{"", Query{}},
		{"   ", Query{}},
		{"Foo", Query{Terms: []string{"foo"}}},
		{`foo "bar baz" #t in:Work`, Query{Terms: []string{"foo"}, Phrases: []string{"bar baz"}, Tags: []string{"t"}, In: "Work"}},
		{`in:"My Folder" Hello  WORLD`, Query{Terms: []string{"hello", "world"}, In: "My Folder"}},
		{`"Exact Phrase"`, Query{Phrases: []string{"exact phrase"}}},
		{`foo "unterminated phrase`, Query{Terms: []string{"foo"}, Phrases: []string{"unterminated phrase"}}},
		{`foo "`, Query{Terms: []string{"foo"}}},
		{`""`, Query{}},
		{`#Work/Client #x`, Query{Tags: []string{"work/client", "x"}}},
		{`# alone`, Query{Terms: []string{"alone"}}},
		{`in:`, Query{}},
		{`in:/Work/Sub/ x`, Query{Terms: []string{"x"}, In: "Work/Sub"}},
		{`IN:a in:b`, Query{In: "b"}},
		{`in:"Unterminated folder`, Query{In: "Unterminated folder"}},
		{`a"b c`, Query{Terms: []string{`a"b`, "c"}}},
		{"tab\tsep\nline", Query{Terms: []string{"tab", "sep", "line"}}},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			if got := ParseQuery(tt.in); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseQuery(%q) = %#v, want %#v", tt.in, got, tt.want)
			}
		})
	}
}

// hit is a comparable projection of Hit.
type hit struct {
	Path    string
	Line    int
	Text    string
	Context string
	Matches [][2]int
}

func project(hs []Hit) []hit {
	var out []hit
	for _, h := range hs {
		out = append(out, hit{h.Path, h.Line, h.Text, h.Context, h.Matches})
	}
	return out
}

func note(path, content string, tags ...string) *index.Note {
	return &index.Note{Path: path, Title: "T " + path, Content: content, Tags: tags}
}

func TestFullText(t *testing.T) {
	ns := []*index.Note{
		note("a.md", "# Alpha\nthe quick brown fox\njumps over\nthe lazy dog\n", "animals"),
		note("b.md", "# Beta\nquick thinking\nno fox here? FOX!\n", "Work/Client"),
		note("Work/c.md", "# Gamma\nfox and dog\n", "work"),
		note("My Folder/d.md", "\n\n  \nfirst real line\nfox\n"),
		note("Workshop/e.md", "fox\n", "workshop"),
	}
	tests := []struct {
		name  string
		query string
		want  []hit
	}{
		{
			name:  "all terms required",
			query: "fox dog",
			want: []hit{
				{"Work/c.md", 1, "fox and dog", "# Gamma", [][2]int{{0, 3}, {8, 11}}},
				{"a.md", 1, "the quick brown fox", "jumps over", [][2]int{{16, 19}}},
				{"a.md", 3, "the lazy dog", "jumps over", [][2]int{{9, 12}}},
			},
		},
		{
			name:  "case-insensitive with multiple matches in a line",
			query: "QUICK fox",
			want: []hit{
				{"a.md", 1, "the quick brown fox", "jumps over", [][2]int{{4, 9}, {16, 19}}},
				{"b.md", 1, "quick thinking", "no fox here? FOX!", [][2]int{{0, 5}}},
				{"b.md", 2, "no fox here? FOX!", "quick thinking", [][2]int{{3, 6}, {13, 16}}},
			},
		},
		{
			name:  "phrase",
			query: `"brown fox"`,
			want: []hit{
				{"a.md", 1, "the quick brown fox", "jumps over", [][2]int{{10, 19}}},
			},
		},
		{
			name:  "phrase not matching across words",
			query: `"fox brown"`,
			want:  nil,
		},
		{
			name:  "overlapping matches merged",
			query: `"quick brown" brown`,
			want: []hit{
				{"a.md", 1, "the quick brown fox", "jumps over", [][2]int{{4, 15}}},
			},
		},
		{
			name:  "tag filter nested",
			query: "fox #work",
			want: []hit{
				{"Work/c.md", 1, "fox and dog", "# Gamma", [][2]int{{0, 3}}},
				{"b.md", 2, "no fox here? FOX!", "quick thinking", [][2]int{{3, 6}, {13, 16}}},
			},
		},
		{
			name:  "tag filter case-insensitive",
			query: "fox #work/CLIENT",
			want: []hit{
				{"b.md", 2, "no fox here? FOX!", "quick thinking", [][2]int{{3, 6}, {13, 16}}},
			},
		},
		{
			name:  "tag only gives first non-empty line",
			query: "#animals",
			want: []hit{
				{"a.md", 0, "# Alpha", "the quick brown fox", nil},
			},
		},
		{
			name:  "in filter is a folder prefix",
			query: "fox in:work",
			want: []hit{
				{"Work/c.md", 1, "fox and dog", "# Gamma", [][2]int{{0, 3}}},
			},
		},
		{
			name:  "in only, quoted folder",
			query: `in:"my folder"`,
			want: []hit{
				{"My Folder/d.md", 3, "first real line", "fox", nil},
			},
		},
		{
			name:  "empty query",
			query: "",
			want:  nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FullText(context.Background(), ParseQuery(tt.query), ns, 0)
			if gp := project(got); !reflect.DeepEqual(gp, tt.want) {
				t.Errorf("FullText(%q) =\n%v\nwant\n%v", tt.query, gp, tt.want)
			}
			for _, h := range got {
				if h.Title != "T "+h.Path {
					t.Errorf("hit %q title = %q", h.Path, h.Title)
				}
			}
		})
	}
}

func TestFullTextUnicodeByteRanges(t *testing.T) {
	ns := []*index.Note{
		note("u.md", "Ünïcode ÜBER straße über\nKelvin: 5K is k\n"),
	}
	tests := []struct {
		query string
		want  []hit
	}{
		{
			query: "über",
			want: []hit{
				// "Ünïcode " is 10 bytes; "ÜBER" is 5; " straße " is 9.
				{"u.md", 0, "Ünïcode ÜBER straße über", "Kelvin: 5K is k", [][2]int{{10, 15}, {24, 29}}},
			},
		},
		{
			query: "straße",
			want: []hit{
				{"u.md", 0, "Ünïcode ÜBER straße über", "Kelvin: 5K is k", [][2]int{{16, 23}}},
			},
		},
		{
			// The Kelvin sign (3 bytes) folds to "k" (1 byte).
			query: "5k",
			want: []hit{
				{"u.md", 1, "Kelvin: 5K is k", "Ünïcode ÜBER straße über", [][2]int{{8, 12}}},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			got := project(FullText(context.Background(), ParseQuery(tt.query), ns, 0))
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got\n%v\nwant\n%v", got, tt.want)
			}
			for _, h := range got {
				for _, m := range h.Matches {
					if !strings.EqualFold(h.Text[m[0]:m[1]], tt.query) {
						t.Errorf("range %v = %q", m, h.Text[m[0]:m[1]])
					}
				}
			}
		})
	}
}

func TestFullTextContext(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    []hit
	}{
		{"single line", "fox", []hit{{"n.md", 0, "fox", "", [][2]int{{0, 3}}}}},
		{"last line uses previous", "a\nfox\n", []hit{{"n.md", 1, "fox", "a", [][2]int{{0, 3}}}}},
		{"crlf", "fox\r\nb\r\n", []hit{{"n.md", 0, "fox", "b", [][2]int{{0, 3}}}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := project(FullText(context.Background(), ParseQuery("fox"), []*index.Note{note("n.md", tt.content)}, 0))
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFullTextLimit(t *testing.T) {
	var ns []*index.Note
	for i := range 10 {
		ns = append(ns, note(fmt.Sprintf("n%d.md", i), "fox\nfox\n"))
	}
	got := FullText(context.Background(), ParseQuery("fox"), ns, 5)
	if len(got) != 5 {
		t.Fatalf("len = %d, want 5", len(got))
	}
	if got[4].Path != "n2.md" || got[4].Line != 0 {
		t.Errorf("last hit = %+v", got[4])
	}
}

func TestFullTextSortsByPath(t *testing.T) {
	ns := []*index.Note{note("b.md", "fox"), note("a.md", "fox")}
	got := FullText(context.Background(), ParseQuery("fox"), ns, 0)
	if len(got) != 2 || got[0].Path != "a.md" || got[1].Path != "b.md" {
		t.Errorf("got %+v", got)
	}
	if ns[0].Path != "b.md" {
		t.Error("input slice reordered")
	}
}

func TestFullTextCancelled(t *testing.T) {
	var ns []*index.Note
	for i := range 100 {
		ns = append(ns, note(fmt.Sprintf("n%03d.md", i), "fox\n"))
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got := FullText(ctx, ParseQuery("fox"), ns, 0); got != nil {
		t.Errorf("cancelled search returned %d hits", len(got))
	}
}

func TestIndexFold(t *testing.T) {
	tests := []struct {
		s, needle  string
		start, end int
	}{
		{"hello", "LL", 2, 4},
		{"hello", "", -1, -1},
		{"hello", "xyz", -1, -1},
		{"hel", "hello", -1, -1},
		{"aÜb", "ü", 1, 3},
		{"xKy", "K", 1, 4},
		{"abc\xffdef", "def", 4, 7},
	}
	for _, tt := range tests {
		s, e := indexFold(tt.s, tt.needle)
		if s != tt.start || e != tt.end {
			t.Errorf("indexFold(%q, %q) = %d,%d want %d,%d", tt.s, tt.needle, s, e, tt.start, tt.end)
		}
	}
}
