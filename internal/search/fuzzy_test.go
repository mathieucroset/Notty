package search

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/mathieucroset/notty/internal/index"
)

func notes(pairs ...string) []*index.Note {
	var out []*index.Note
	for i := 0; i+1 < len(pairs); i += 2 {
		out = append(out, &index.Note{Path: pairs[i], Title: pairs[i+1]})
	}
	return out
}

func resultPaths(rs []FuzzyResult) []string {
	var out []string
	for _, r := range rs {
		out = append(out, r.Path)
	}
	return out
}

func TestFuzzyEmptyQuery(t *testing.T) {
	ns := notes("a.md", "A", "b.md", "B", "c.md", "C")
	tests := []struct {
		name    string
		query   string
		recents []string
		want    []string
	}{
		{"recents order", "", []string{"c.md", "a.md"}, []string{"c.md", "a.md"}},
		{"missing skipped", "", []string{"gone.md", "b.md", "also-gone.md"}, []string{"b.md"}},
		{"duplicates once", "", []string{"a.md", "a.md", "b.md"}, []string{"a.md", "b.md"}},
		{"whitespace is empty", "   ", []string{"b.md"}, []string{"b.md"}},
		{"no recents", "", nil, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Fuzzy(tt.query, ns, tt.recents)
			if gp := resultPaths(got); !reflect.DeepEqual(gp, tt.want) {
				t.Errorf("paths = %v, want %v", gp, tt.want)
			}
			for _, r := range got {
				if r.Title == "" {
					t.Errorf("result %q has no title", r.Path)
				}
			}
		})
	}
}

func TestFuzzyEmptyQueryMax20(t *testing.T) {
	var ns []*index.Note
	var recents []string
	for i := range 30 {
		p := fmt.Sprintf("n%02d.md", i)
		ns = append(ns, &index.Note{Path: p, Title: p})
		recents = append(recents, p)
	}
	got := Fuzzy("", ns, recents)
	if len(got) != 20 || got[0].Path != "n00.md" || got[19].Path != "n19.md" {
		t.Errorf("got %d results: %v", len(got), resultPaths(got))
	}
}

func TestFuzzyRanking(t *testing.T) {
	ns := notes(
		"zeta.md", "Zeta",
		"alpha.md", "Alpha",
		"beta.md", "Beta",
		"Work/meeting notes.md", "Weekly meeting",
		"misc.md", "Something else",
	)
	tests := []struct {
		name    string
		query   string
		recents []string
		want    []string
	}{
		{
			name:  "no match",
			query: "qqq",
			want:  nil,
		},
		{
			name:  "ties broken by path",
			query: "ta", // matches zeta and beta with equal scores
			want:  []string{"beta.md", "zeta.md"},
		},
		{
			name:    "ties broken by recency",
			query:   "ta",
			recents: []string{"other.md", "zeta.md"},
			want:    []string{"zeta.md", "beta.md"},
		},
		{
			name:    "more recent first",
			query:   "ta",
			recents: []string{"beta.md", "zeta.md"},
			want:    []string{"beta.md", "zeta.md"},
		},
		{
			name:    "score beats recency",
			query:   "alp",
			recents: []string{"misc.md"},
			want:    []string{"alpha.md"},
		},
		{
			name:  "title match",
			query: "weekly",
			want:  []string{"Work/meeting notes.md"},
		},
		{
			name:  "extension not matched",
			query: "md",
			want:  nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Fuzzy(tt.query, ns, tt.recents)
			if gp := resultPaths(got); !reflect.DeepEqual(gp, tt.want) {
				t.Errorf("paths = %v, want %v (results %+v)", gp, tt.want, got)
			}
		})
	}
}

func TestFuzzyScoreIsBestOfPathAndTitle(t *testing.T) {
	// "wm" matches the title "Weekly meeting" at word starts (strong) and
	// the path only weakly.
	ns := notes("Work/xxxx/weird-meeting.md", "Weekly Meeting")
	got := Fuzzy("wm", ns, nil)
	if len(got) != 1 {
		t.Fatalf("got %+v", got)
	}
	r := got[0]
	if len(r.PathMatches) == 0 || len(r.TitleMatches) == 0 {
		t.Errorf("want both match sets, got %+v", r)
	}
	if r.Score < 0 {
		t.Errorf("Score = %d", r.Score)
	}
}

func TestFuzzyMatchIndexes(t *testing.T) {
	tests := []struct {
		name               string
		path, title, query string
		wantPath, wantTtl  []int
	}{
		{
			name: "ascii", path: "Work/plan.md", title: "Q4 Plan", query: "plan",
			wantPath: []int{5, 6, 7, 8}, wantTtl: []int{3, 4, 5, 6},
		},
		{
			name: "rune indexes on unicode", path: "café/über.md", title: "Über uns", query: "üb",
			wantPath: []int{5, 6}, wantTtl: []int{0, 1},
		},
		{
			name: "only title matches", path: "x.md", title: "Hello", query: "hlo",
			wantPath: nil, wantTtl: []int{0, 2, 4},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Fuzzy(tt.query, notes(tt.path, tt.title), nil)
			if len(got) != 1 {
				t.Fatalf("got %+v", got)
			}
			if !reflect.DeepEqual(got[0].PathMatches, tt.wantPath) {
				t.Errorf("PathMatches = %v, want %v", got[0].PathMatches, tt.wantPath)
			}
			if !reflect.DeepEqual(got[0].TitleMatches, tt.wantTtl) {
				t.Errorf("TitleMatches = %v, want %v", got[0].TitleMatches, tt.wantTtl)
			}
		})
	}
}

func TestFuzzyTop50(t *testing.T) {
	var ns []*index.Note
	for i := range 80 {
		p := fmt.Sprintf("note%02d.md", i)
		ns = append(ns, &index.Note{Path: p, Title: p})
	}
	got := Fuzzy("note", ns, nil)
	if len(got) != 50 {
		t.Fatalf("len = %d, want 50", len(got))
	}
	if got[0].Path != "note00.md" || got[49].Path != "note49.md" {
		t.Errorf("first/last = %q/%q", got[0].Path, got[49].Path)
	}
}
