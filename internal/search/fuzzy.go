// Package search implements Notty's fuzzy finder and full-text search over
// the note index (spec §8).
package search

import (
	"sort"
	"strings"

	"github.com/sahilm/fuzzy"

	"github.com/mathieucroset/notty/internal/index"
)

const (
	// maxRecents is how many recent notes an empty fuzzy query shows.
	maxRecents = 20
	// maxFuzzyResults caps the fuzzy finder's result list.
	maxFuzzyResults = 50
)

// FuzzyResult is one fuzzy finder entry. PathMatches and TitleMatches are
// the rune indexes of matched characters in Path and Title, for
// highlighting; either may be nil when only the other field matched.
type FuzzyResult struct {
	Path, Title  string
	Score        int
	PathMatches  []int
	TitleMatches []int
}

// Fuzzy ranks notes against query, matching both the path (without its
// ".md" extension) and the title; a note's score is the better of the two.
// Ties are broken by recency (earlier in recents first, recent notes before
// the rest), then by path. At most 50 results are returned.
//
// An empty (or blank) query returns the recents that exist in notes, in
// recents order, at most 20.
func Fuzzy(query string, notes []*index.Note, recents []string) []FuzzyResult {
	query = strings.TrimSpace(query)
	byPath := make(map[string]*index.Note, len(notes))
	for _, n := range notes {
		byPath[n.Path] = n
	}
	rank := make(map[string]int, len(recents))
	for i, p := range recents {
		if _, ok := rank[p]; !ok {
			rank[p] = i
		}
	}

	if query == "" {
		var out []FuzzyResult
		for i, p := range recents {
			n, ok := byPath[p]
			if !ok || rank[p] != i { // missing, or a duplicate
				continue
			}
			out = append(out, FuzzyResult{Path: n.Path, Title: n.Title})
			if len(out) == maxRecents {
				break
			}
		}
		return out
	}

	pathStrs := make([]string, len(notes))
	titleStrs := make([]string, len(notes))
	for i, n := range notes {
		pathStrs[i] = stripNoteExt(n.Path)
		titleStrs[i] = n.Title
	}
	results := map[int]*FuzzyResult{} // by note index
	get := func(i int) *FuzzyResult {
		r, ok := results[i]
		if !ok {
			r = &FuzzyResult{Path: notes[i].Path, Title: notes[i].Title}
			results[i] = r
		}
		return r
	}
	for _, m := range fuzzy.FindNoSort(query, pathStrs) {
		r := get(m.Index)
		r.Score = m.Score
		r.PathMatches = runeIndexes(m.Str, m.MatchedIndexes)
	}
	for _, m := range fuzzy.FindNoSort(query, titleStrs) {
		_, seen := results[m.Index]
		r := get(m.Index)
		if !seen || m.Score > r.Score {
			r.Score = m.Score
		}
		r.TitleMatches = runeIndexes(m.Str, m.MatchedIndexes)
	}

	out := make([]FuzzyResult, 0, len(results))
	for _, r := range results {
		out = append(out, *r)
	}
	recency := func(p string) int {
		if i, ok := rank[p]; ok {
			return i
		}
		return len(recents)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Score != b.Score {
			return a.Score > b.Score
		}
		if ra, rb := recency(a.Path), recency(b.Path); ra != rb {
			return ra < rb
		}
		return a.Path < b.Path
	})
	if len(out) > maxFuzzyResults {
		out = out[:maxFuzzyResults]
	}
	return out
}

// stripNoteExt removes a trailing ".md" (any case) from p.
func stripNoteExt(p string) string {
	if n := len(p) - 3; n >= 0 && strings.EqualFold(p[n:], ".md") {
		return p[:n]
	}
	return p
}

// runeIndexes converts ascending byte offsets in s to rune indexes.
func runeIndexes(s string, byteIdx []int) []int {
	if len(byteIdx) == 0 {
		return nil
	}
	out := make([]int, 0, len(byteIdx))
	k, r := 0, 0
	for b := range s {
		for k < len(byteIdx) && byteIdx[k] == b {
			out = append(out, r)
			k++
		}
		if k == len(byteIdx) {
			break
		}
		r++
	}
	return out
}
